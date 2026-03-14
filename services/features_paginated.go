package services

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"time"

	"pwa_gis_tracking/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// parseNumericFilter parses operator+value strings like ">=2568", "<=100", ">50", "<10".
// Returns the MongoDB operator, the numeric value, and whether parsing succeeded.
func parseNumericFilter(value string) (string, int64, bool) {
	value = strings.TrimSpace(value)
	var op string
	var numStr string

	if strings.HasPrefix(value, ">=") {
		op = "$gte"
		numStr = strings.TrimSpace(value[2:])
	} else if strings.HasPrefix(value, "<=") {
		op = "$lte"
		numStr = strings.TrimSpace(value[2:])
	} else if strings.HasPrefix(value, ">") {
		op = "$gt"
		numStr = strings.TrimSpace(value[1:])
	} else if strings.HasPrefix(value, "<") {
		op = "$lt"
		numStr = strings.TrimSpace(value[1:])
	} else {
		numStr = value
	}

	var numVal int64
	if _, err := fmt.Sscanf(numStr, "%d", &numVal); err != nil {
		return "", 0, false
	}
	return op, numVal, true
}

// PaginatedResult holds the response for paginated feature queries.
type PaginatedResult struct {
	Data       []map[string]interface{} `json:"data"`
	Columns    []ColumnInfo             `json:"columns"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"page_size"`
	Total      int64                    `json:"total"`
	TotalPages int                      `json:"total_pages"`
}

// ColumnInfo describes one column in the mapped output.
type ColumnInfo struct {
	Key      string `json:"key"`       // Postgres-style column name (display key)
	MongoKey string `json:"mongo_key"` // Original MongoDB field name
}

// ListFeaturesPaginated fetches features from MongoDB with pagination,
// maps properties through FieldMapping, and supports case-insensitive search.
func ListFeaturesPaginated(
	pwaCode, collection, startDate, endDate, search string,
	page, pageSize int,
	raw bool,
	filters map[string]string,
	sortBy string, sortOrder int,
) (*PaginatedResult, error) {

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Resolve the actual MongoDB collection ObjectID hex string
	collectionID, err := FindCollectionID(pwaCode, collection)
	if err != nil {
		return nil, fmt.Errorf("collection not found for %s/%s: %w", pwaCode, collection, err)
	}

	// ★ Collection name = "features_" + collectionID (same as mongo_service.go)
	coll := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))

	// ────────────────────────────────────────────
	// Build query filter
	// ────────────────────────────────────────────
	filter := bson.M{}

	// ★ Use buildDateFilter (same as CountFeatures / ExportFeaturesAsGeoJSON)
	if startDate != "" || endDate != "" {
		layerCfg := LayerConfigs[collection]
		dateField := "properties." + layerCfg.DateField
		dateFilter := buildDateFilter(dateField, startDate, endDate)
		if dateFilter != nil {
			filter = dateFilter
		}
	}

	// Case-insensitive search across all mapped fields
	// Supports multi-term search: "hdpe 100" → each term must match at least one field
	mapping := FieldMapping[collection]
	if search != "" {
		searchTerms := strings.Fields(search)
		var termConds []bson.M

		for _, term := range searchTerms {
			termRegex := primitive.Regex{Pattern: term, Options: "i"}
			var orConds []bson.M

			if len(mapping) > 0 {
				for mongoKey, pgKey := range mapping {
					if pgKey == "password" {
						continue
					}
					orConds = append(orConds, bson.M{
						"properties." + mongoKey: termRegex,
					})
				}
			} else {
				for _, f := range []string{"_id", "typeId", "sizeId", "pwaCode", "remark"} {
					orConds = append(orConds, bson.M{
						"properties." + f: termRegex,
					})
				}
			}

			if len(orConds) > 0 {
				termConds = append(termConds, bson.M{"$or": orConds})
			}
		}

		if len(termConds) > 0 {
			if len(filter) > 0 {
				existing := []bson.M{filter}
				existing = append(existing, termConds...)
				filter = bson.M{"$and": existing}
			} else if len(termConds) == 1 {
				filter = termConds[0]
			} else {
				filter = bson.M{"$and": termConds}
			}
		}
	}

	// Column-specific filters (exact match or regex on specific MongoDB fields)
	if len(filters) > 0 {
		var andConds []bson.M
		if existing, ok := filter["$and"]; ok {
			andConds = existing.([]bson.M)
		} else if len(filter) > 0 {
			andConds = []bson.M{filter}
		}

		// ID columns that may be stored as int or string in MongoDB
		numericIDFields := map[string]bool{
			"PIPE_ID": true, "VALVE_ID": true, "FIRE_ID": true,
			"LEAK_ID": true, "STRUCT_ID": true, "BLDG_ID": true,
		}

		// Integer-cast columns: support comparison operators (>=, <=, >, <)
		intCastFields := map[string]bool{
			"yearInstall":       true,
			"roundOpen":         true,
			"pressure":          true,
			"averageWaterUsage": true,
			"presentWaterUsage": true,
		}

		for field, value := range filters {
			propField := "properties." + field
			// Date fields: use range matching (start of day to end of day)
			if field == "recordDate" || field == "leakDatetime" || field == "beginCustDate" {
				dateFilter := buildDateFilter(propField, value, value)
				if dateFilter != nil {
					andConds = append(andConds, dateFilter)
				}
			} else if numericIDFields[field] {
				// ID columns: match both string and numeric types
				var numVal int64
				if _, err := fmt.Sscanf(value, "%d", &numVal); err == nil {
					andConds = append(andConds, bson.M{
						"$or": []bson.M{
							{propField: value},
							{propField: numVal},
							{propField: int32(numVal)},
						},
					})
				} else {
					andConds = append(andConds, bson.M{propField: value})
				}
			} else if intCastFields[field] {
				// Integer-cast columns: support comparison operators and dual-type match
				op, numVal, ok := parseNumericFilter(value)
				if ok {
					if op != "" {
						// Comparison operator: $gte, $lte, $gt, $lt
						andConds = append(andConds, bson.M{
							propField: bson.M{op: numVal},
						})
					} else {
						// No operator: dual-type match (string AND int)
						andConds = append(andConds, bson.M{
							"$or": []bson.M{
								{propField: fmt.Sprintf("%d", numVal)},
								{propField: numVal},
								{propField: int32(numVal)},
							},
						})
					}
				} else {
					// Not a valid number, fall back to exact string match
					andConds = append(andConds, bson.M{propField: value})
				}
			} else {
				// Try dual-type match (string + int) for values that look numeric
				var numVal int64
				if _, err := fmt.Sscanf(value, "%d", &numVal); err == nil && fmt.Sprintf("%d", numVal) == value {
					andConds = append(andConds, bson.M{
						"$or": []bson.M{
							{propField: value},
							{propField: numVal},
							{propField: int32(numVal)},
						},
					})
				} else {
					andConds = append(andConds, bson.M{propField: value})
				}
			}
		}

		if len(andConds) > 0 {
			filter = bson.M{"$and": andConds}
		}
	}

	// ────────────────────────────────────────────
	// Count total matching documents
	// ────────────────────────────────────────────
	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("count error: %w", err)
	}

	totalPages := int(total) / pageSize
	if int(total)%pageSize > 0 {
		totalPages++
	}
	if totalPages == 0 {
		totalPages = 1
	}
	if page > totalPages {
		page = totalPages
	}

	// ────────────────────────────────────────────
	// Paginated query — properties only (no geometry)
	// ────────────────────────────────────────────
	skip := int64((page - 1) * pageSize)
	// Build sort — use sortBy if provided, default to recordDate desc
	sortKey := "properties.recordDate"
	sortDir := -1 // descending (newest first)
	if sortBy != "" {
		sortKey = "properties." + sortBy
		sortDir = sortOrder
	}

	opts := options.Find().
		SetSkip(skip).
		SetLimit(int64(pageSize)).
		SetSort(bson.D{
			{Key: sortKey, Value: sortDir},
		}).
		SetProjection(bson.M{
			"properties": 1,
			"_id":        1,
		})

	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer cursor.Close(ctx)

	// ────────────────────────────────────────────
	// Process results with field mapping
	// ────────────────────────────────────────────
	var data []map[string]interface{}

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			log.Printf("[FeaturesList] decode error: %v", err)
			continue
		}

		props, ok := doc["properties"].(bson.M)
		if !ok {
			continue
		}

		// Convert bson.M → map[string]interface{} with BSON type cleanup
		// ★ Same type handling as ExportFeaturesAsGeoJSON in mongo_service.go
		rawProps := make(map[string]interface{}, len(props))
		for k, v := range props {
			switch val := v.(type) {
			case primitive.DateTime:
				rawProps[k] = val.Time().Format(time.RFC3339)
			case primitive.ObjectID:
				rawProps[k] = val.Hex()
			case bson.M, bson.A:
				// Skip nested objects/arrays (same as mongo_service.go)
				continue
			default:
				rawProps[k] = v
			}
		}

		// Apply field mapping (mongo key → postgres key) unless raw mode
		var row map[string]interface{}
		if raw {
			row = rawProps
		} else {
			row = MapProperties(collection, rawProps)
		}

		// Add the MongoDB document _id as reference (hidden column)
		if docID, ok := doc["_id"]; ok {
			if oid, ok := docID.(primitive.ObjectID); ok {
				row["_doc_id"] = oid.Hex()
			} else {
				row["_doc_id"] = fmt.Sprintf("%v", docID)
			}
		}

		data = append(data, row)
	}

	if data == nil {
		data = []map[string]interface{}{}
	}

	// Build ordered column info
	var columns []ColumnInfo
	if raw && len(data) > 0 {
		// In raw mode, derive columns from the first data row (sorted alphabetically)
		seen := map[string]bool{}
		for _, row := range data {
			for k := range row {
				if k != "_doc_id" && k != "_createdBy" && !seen[k] {
					columns = append(columns, ColumnInfo{Key: k, MongoKey: k})
					seen[k] = true
				}
			}
		}
		sort.Slice(columns, func(i, j int) bool { return columns[i].Key < columns[j].Key })
	} else {
		columns = buildColumnInfo(collection)
	}

	log.Printf("[FeaturesList] %s/%s page=%d total=%d rows=%d search=%q",
		pwaCode, collection, page, total, len(data), search)

	return &PaginatedResult{
		Data:       data,
		Columns:    columns,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// ────────────────────────────────────────────────────────────
// Column ordering per collection (matches field_mapping.go)
// ────────────────────────────────────────────────────────────

var preferredOrder = map[string][]string{
	"pipe": {
		"pipe_id", "project_no", "contrac_date", "cap_date", "asset_code",
		"pipe_type", "grade", "pipe_size", "class", "pipe_func",
		"laying", "product", "depth", "pipe_long", "yearinstall",
		"locate", "pwa_code", "rec_date", "remark",
	},
	"valve": {
		"valve_id", "valve_type", "valve_size", "valve_status",
		"depth", "round_open", "yearinstall",
		"pwa_code", "rec_date", "remark",
	},
	"firehydrant": {
		"fire_id", "fire_size", "fire_status", "pressure",
		"pwa_code", "rec_date", "remark",
	},
	"meter": {
		"bldg_id", "pipe_id", "custcode", "custname", "meterno",
		"metersize", "bgncustdt", "mtrrdroute", "mtrseq", "addrno",
		"custstat", "pwa_code", "rec_date", "remark",
	},
	"bldg": {
		"bldg_id", "housecode", "use_status", "custcode", "custname",
		"usetype", "bl_type", "addrno", "building", "floor",
		"villageno", "village", "soi", "road",
		"subdistrict", "district", "province", "zipcode",
		"pwa_code", "rec_date",
	},
	"leakpoint": {
		"leak_id", "leak_no", "leakdate", "locate", "leakcause",
		"leakdepth", "repairby", "repaircost", "repairdate",
		"leakdetail", "leakchecker", "pipe_id", "pipe_type", "pipe_size",
		"leak_informer", "pwa_code", "rec_date", "remark",
	},
}

func buildColumnInfo(collection string) []ColumnInfo {
	mapping, exists := FieldMapping[collection]
	if !exists || len(mapping) == 0 {
		return []ColumnInfo{{Key: "_doc_id", MongoKey: "_id"}}
	}

	// Build reverse map: pgKey → mongoKey
	reverse := make(map[string]string, len(mapping))
	for mk, pk := range mapping {
		reverse[pk] = mk
	}

	var columns []ColumnInfo
	added := map[string]bool{}

	// 1) Use preferred order if defined
	if order, ok := preferredOrder[collection]; ok {
		for _, pgKey := range order {
			if mongoKey, ok := reverse[pgKey]; ok {
				columns = append(columns, ColumnInfo{Key: pgKey, MongoKey: mongoKey})
				added[pgKey] = true
			}
		}
	}

	// 2) Append remaining mapped fields (sorted for determinism)
	var remaining []string
	for _, pgKey := range mapping {
		if !added[pgKey] && pgKey != "password" {
			remaining = append(remaining, pgKey)
		}
	}
	sort.Strings(remaining)
	for _, pgKey := range remaining {
		if mk, ok := reverse[pgKey]; ok {
			columns = append(columns, ColumnInfo{Key: pgKey, MongoKey: mk})
		}
	}

	return columns
}

// SuggestFeatureValues returns autocomplete suggestions for a search query.
// It searches across all mapped fields and returns distinct matching values.
func SuggestFeatureValues(pwaCode, collection, query string, limit int) ([]map[string]string, error) {
	if query == "" || len(query) < 2 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	collectionID, err := FindCollectionID(pwaCode, collection)
	if err != nil {
		return nil, fmt.Errorf("collection not found: %w", err)
	}

	coll := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))
	mapping := FieldMapping[collection]
	if len(mapping) == 0 {
		return nil, nil
	}

	// Build $or regex filter across all mapped fields
	escapedQuery := regexp.QuoteMeta(query)
	searchRegex := primitive.Regex{Pattern: escapedQuery, Options: "i"}
	var orConds []bson.M
	var searchFields []string

	for mongoKey, pgKey := range mapping {
		if pgKey == "password" {
			continue
		}
		orConds = append(orConds, bson.M{
			"properties." + mongoKey: searchRegex,
		})
		searchFields = append(searchFields, mongoKey)
	}

	if len(orConds) == 0 {
		return nil, nil
	}

	// Query limited docs
	findOpts := options.Find().
		SetLimit(60).
		SetProjection(bson.M{"properties": 1, "_id": 0})

	cursor, err := coll.Find(ctx, bson.M{"$or": orConds}, findOpts)
	if err != nil {
		return nil, fmt.Errorf("suggest query error: %w", err)
	}
	defer cursor.Close(ctx)

	// Extract matching values from results
	type suggestion struct {
		Field string
		Value string
	}
	seen := map[string]bool{}
	var suggestions []suggestion

	lowerQuery := strings.ToLower(query)

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		props, ok := doc["properties"].(bson.M)
		if !ok {
			continue
		}

		for _, mongoKey := range searchFields {
			val, exists := props[mongoKey]
			if !exists || val == nil {
				continue
			}
			strVal := fmt.Sprintf("%v", val)
			if strVal == "" {
				continue
			}
			if !strings.Contains(strings.ToLower(strVal), lowerQuery) {
				continue
			}
			key := mongoKey + ":" + strVal
			if seen[key] {
				continue
			}
			seen[key] = true
			suggestions = append(suggestions, suggestion{Field: mongoKey, Value: strVal})
			if len(suggestions) >= limit {
				break
			}
		}
		if len(suggestions) >= limit {
			break
		}
	}

	// Convert to response format
	result := make([]map[string]string, len(suggestions))
	for i, s := range suggestions {
		result[i] = map[string]string{
			"field": s.Field,
			"value": s.Value,
		}
	}
	return result, nil
}

// FacetField defines which fields to aggregate for faceted filtering per collection.
var FacetFields = map[string][]string{
	"pipe":           {"typeId", "sizeId", "classId", "functionId", "gradeId", "layingId", "yearInstall"},
	"valve":          {"typeId", "sizeId", "statusId", "yearInstall"},
	"firehydrant":    {"sizeId", "statusId"},
	"meter":          {"custStat", "meterSizeCode", "meterSizeName"},
	"bldg":           {"buildingTypeId", "useStatusId", "useTypeId"},
	"leakpoint":      {"typeId", "pipeTypeId", "pipeSizeId", "cause"},
	"pwa_waterworks": {"depShortName"},
}

// FacetResult holds one facet: field name + value/count pairs.
type FacetResult struct {
	Field  string       `json:"field"`
	Values []FacetValue `json:"values"`
}

// FacetValue is a single value with its document count.
type FacetValue struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// GetFacetValues returns distinct values + counts for key columns, used for faceted filtering.
func GetFacetValues(pwaCode, collection string) ([]FacetResult, error) {
	fields, ok := FacetFields[collection]
	if !ok || len(fields) == 0 {
		return nil, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	collectionID, err := FindCollectionID(pwaCode, collection)
	if err != nil {
		return nil, fmt.Errorf("collection not found: %w", err)
	}

	coll := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))

	// Build a single aggregation with $facet to get all fields in one query
	facetStage := bson.M{}
	for _, f := range fields {
		propField := "properties." + f
		facetStage[f] = bson.A{
			bson.M{"$group": bson.M{
				"_id":   "$" + propField,
				"count": bson.M{"$sum": 1},
			}},
			bson.M{"$match": bson.M{"_id": bson.M{"$ne": nil}}},
			bson.M{"$sort": bson.M{"count": -1}},
			bson.M{"$limit": 30},
		}
	}

	pipeline := bson.A{
		bson.M{"$facet": facetStage},
	}

	cursor, err := coll.Aggregate(ctx, pipeline)
	if err != nil {
		return nil, fmt.Errorf("facet aggregation error: %w", err)
	}
	defer cursor.Close(ctx)

	// Parse the single-document result
	var results []FacetResult
	if cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			return nil, fmt.Errorf("facet decode error: %w", err)
		}

		for _, f := range fields {
			rawBuckets, exists := doc[f]
			if !exists {
				continue
			}
			buckets, ok := rawBuckets.(bson.A)
			if !ok {
				continue
			}

			var values []FacetValue
			for _, b := range buckets {
				bucket, ok := b.(bson.M)
				if !ok {
					continue
				}
				idVal := bucket["_id"]
				if idVal == nil {
					continue
				}
				strVal := fmt.Sprintf("%v", idVal)
				if strVal == "" {
					continue
				}
				cnt := 0
				switch c := bucket["count"].(type) {
				case int32:
					cnt = int(c)
				case int64:
					cnt = int(c)
				case float64:
					cnt = int(c)
				}
				values = append(values, FacetValue{Value: strVal, Count: cnt})
			}
			if len(values) > 0 {
				results = append(results, FacetResult{Field: f, Values: values})
			}
		}
	}

	return results, nil
}