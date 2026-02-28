package services

import (
	"context"
	"fmt"
	"log"
	"sort"
	"time"

	"pwa_gis_tracking/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

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
	mapping := FieldMapping[collection]
	if search != "" {
		searchRegex := primitive.Regex{Pattern: search, Options: "i"}
		var orConds []bson.M

		if len(mapping) > 0 {
			for mongoKey, pgKey := range mapping {
				if pgKey == "password" {
					continue
				}
				orConds = append(orConds, bson.M{
					"properties." + mongoKey: searchRegex,
				})
			}
		} else {
			// Fallback: search common property fields
			for _, f := range []string{"_id", "typeId", "sizeId", "pwaCode", "remark"} {
				orConds = append(orConds, bson.M{
					"properties." + f: searchRegex,
				})
			}
		}

		if len(orConds) > 0 {
			// If we already have a date filter, combine with $and
			if len(filter) > 0 {
				filter = bson.M{
					"$and": []bson.M{
						filter,
						{"$or": orConds},
					},
				}
			} else {
				filter["$or"] = orConds
			}
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
	opts := options.Find().
		SetSkip(skip).
		SetLimit(int64(pageSize)).
		SetSort(bson.D{
			{Key: "properties.recordDate", Value: -1}, // newest first
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

		// Apply field mapping (mongo key → postgres key)
		mapped := MapProperties(collection, rawProps)

		// Add the MongoDB document _id as reference (hidden column)
		if docID, ok := doc["_id"]; ok {
			if oid, ok := docID.(primitive.ObjectID); ok {
				mapped["_doc_id"] = oid.Hex()
			} else {
				mapped["_doc_id"] = fmt.Sprintf("%v", docID)
			}
		}

		data = append(data, mapped)
	}

	if data == nil {
		data = []map[string]interface{}{}
	}

	// Build ordered column info
	columns := buildColumnInfo(collection)

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