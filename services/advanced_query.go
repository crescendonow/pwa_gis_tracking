package services

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"time"

	"pwa_gis_tracking/config"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// ════════════════════════════════════════════════════════════
// Request / Response types
// ════════════════════════════════════════════════════════════

// AdvancedQueryRequest is the JSON body for POST /api/features/advanced-query.
type AdvancedQueryRequest struct {
	PwaCode    string                 `json:"pwaCode"`
	Collection string                 `json:"collection"`
	Conditions map[string]interface{} `json:"conditions"` // ConditionGroup as raw JSON
	StartDate  string                 `json:"startDate"`
	EndDate    string                 `json:"endDate"`
	Page       int                    `json:"page"`
	PageSize   int                    `json:"pageSize"`
	SortBy     string                 `json:"sortBy"`
	SortOrder  string                 `json:"sortOrder"`
	Limit      int                    `json:"limit"`
}

// ════════════════════════════════════════════════════════════
// Validation & Security
// ════════════════════════════════════════════════════════════

// AllowedOperators restricts which operators can be used in queries.
var AllowedOperators = map[string]bool{
	"=": true, "!=": true,
	">": true, "<": true, ">=": true, "<=": true,
	"contains": true, "not_contains": true,
	"in": true, "between": true,
	"is_empty": true, "is_not_empty": true,
}

// validateField checks that a field exists in FieldMapping for the collection.
// Accepts either MongoDB key (e.g. "sizeId") or display key (e.g. "pipe_size").
// Returns the full MongoDB path (e.g. "properties.sizeId").
func validateField(collection, field string) (string, error) {
	mapping := FieldMapping[collection]

	// Collections without mapping (e.g. pwa_waterworks) — allow any field
	if len(mapping) == 0 {
		// Still sanitize: only allow alphanumeric + underscore
		if !regexp.MustCompile(`^[a-zA-Z0-9_]+$`).MatchString(field) {
			return "", fmt.Errorf("invalid field name: %s", field)
		}
		return "properties." + field, nil
	}

	// Check if it's a MongoDB key
	if _, exists := mapping[field]; exists {
		return "properties." + field, nil
	}

	// Check if it's a display (pg) key → reverse lookup
	for mongoKey, pgKey := range mapping {
		if pgKey == field && pgKey != "password" {
			return "properties." + mongoKey, nil
		}
	}

	return "", fmt.Errorf("invalid field: %s", field)
}

// ════════════════════════════════════════════════════════════
// Condition Tree → MongoDB BSON Translation
// ════════════════════════════════════════════════════════════

// TranslateConditions converts a condition group tree (parsed from JSON)
// into a MongoDB BSON filter. Supports recursive nesting up to maxDepth.
func TranslateConditions(collection string, group map[string]interface{}, depth int) (bson.M, error) {
	const maxDepth = 5
	const maxRulesPerGroup = 20

	if depth > maxDepth {
		return nil, fmt.Errorf("nested conditions too deep (max %d levels)", maxDepth)
	}

	if group == nil {
		return bson.M{}, nil
	}

	// Extract logic (AND/OR)
	logic := "AND"
	if l, ok := group["logic"].(string); ok {
		logic = strings.ToUpper(l)
	}
	if logic != "AND" && logic != "OR" {
		logic = "AND"
	}

	// Extract rules array
	rawRules, ok := group["rules"].([]interface{})
	if !ok || len(rawRules) == 0 {
		return bson.M{}, nil
	}

	if len(rawRules) > maxRulesPerGroup {
		return nil, fmt.Errorf("too many rules in group (max %d)", maxRulesPerGroup)
	}

	var conditions []bson.M

	for _, rawRule := range rawRules {
		ruleMap, ok := rawRule.(map[string]interface{})
		if !ok {
			continue
		}

		// Determine if this is a nested group or a leaf rule
		if _, hasLogic := ruleMap["logic"]; hasLogic {
			// Nested group
			nested, err := TranslateConditions(collection, ruleMap, depth+1)
			if err != nil {
				return nil, err
			}
			if len(nested) > 0 {
				conditions = append(conditions, nested)
			}
		} else {
			// Leaf rule
			filter, err := translateRule(collection, ruleMap)
			if err != nil {
				return nil, err
			}
			if filter != nil {
				conditions = append(conditions, filter)
			}
		}
	}

	if len(conditions) == 0 {
		return bson.M{}, nil
	}
	if len(conditions) == 1 {
		return conditions[0], nil
	}

	mongoOp := "$and"
	if logic == "OR" {
		mongoOp = "$or"
	}
	return bson.M{mongoOp: conditions}, nil
}

// translateRule converts a single rule {field, operator, value, value2} to BSON.
func translateRule(collection string, rule map[string]interface{}) (bson.M, error) {
	field, _ := rule["field"].(string)
	operator, _ := rule["operator"].(string)
	value := rule["value"]
	value2 := rule["value2"]

	if field == "" || operator == "" {
		return nil, nil // Skip incomplete rules
	}

	// Validate operator
	if !AllowedOperators[operator] {
		return nil, fmt.Errorf("invalid operator: %s", operator)
	}

	// Validate field against FieldMapping whitelist
	mongoField, err := validateField(collection, field)
	if err != nil {
		return nil, err
	}

	switch operator {
	case "=":
		return aqBuildEquality(mongoField, value), nil

	case "!=":
		strVal := fmt.Sprintf("%v", value)
		var numVal int64
		if _, err := fmt.Sscanf(strVal, "%d", &numVal); err == nil && fmt.Sprintf("%d", numVal) == strVal {
			// Exclude both string and numeric forms
			return bson.M{"$and": []bson.M{
				{mongoField: bson.M{"$ne": strVal}},
				{mongoField: bson.M{"$ne": numVal}},
				{mongoField: bson.M{"$ne": int32(numVal)}},
			}}, nil
		}
		return bson.M{mongoField: bson.M{"$ne": strVal}}, nil

	case ">":
		return bson.M{mongoField: bson.M{"$gt": aqCoerceNumeric(value)}}, nil
	case "<":
		return bson.M{mongoField: bson.M{"$lt": aqCoerceNumeric(value)}}, nil
	case ">=":
		return bson.M{mongoField: bson.M{"$gte": aqCoerceNumeric(value)}}, nil
	case "<=":
		return bson.M{mongoField: bson.M{"$lte": aqCoerceNumeric(value)}}, nil

	case "contains":
		pattern := regexp.QuoteMeta(fmt.Sprintf("%v", value))
		return bson.M{mongoField: primitive.Regex{Pattern: pattern, Options: "i"}}, nil

	case "not_contains":
		pattern := regexp.QuoteMeta(fmt.Sprintf("%v", value))
		return bson.M{mongoField: bson.M{
			"$not": primitive.Regex{Pattern: pattern, Options: "i"},
		}}, nil

	case "in":
		values := aqParseInValues(value)
		if len(values) == 0 {
			return nil, nil
		}
		return bson.M{mongoField: bson.M{"$in": values}}, nil

	case "between":
		return bson.M{mongoField: bson.M{
			"$gte": aqCoerceNumeric(value),
			"$lte": aqCoerceNumeric(value2),
		}}, nil

	case "is_empty":
		return bson.M{"$or": []bson.M{
			{mongoField: bson.M{"$exists": false}},
			{mongoField: nil},
			{mongoField: ""},
		}}, nil

	case "is_not_empty":
		return bson.M{"$and": []bson.M{
			{mongoField: bson.M{"$exists": true}},
			{mongoField: bson.M{"$ne": nil}},
			{mongoField: bson.M{"$ne": ""}},
		}}, nil
	}

	return nil, fmt.Errorf("unhandled operator: %s", operator)
}

// ════════════════════════════════════════════════════════════
// Value helpers (prefixed aq to avoid collision with features_paginated.go)
// ════════════════════════════════════════════════════════════

// aqBuildEquality handles dual-type matching: string "160" vs int 160.
func aqBuildEquality(field string, value interface{}) bson.M {
	strVal := fmt.Sprintf("%v", value)
	var numVal int64
	if _, err := fmt.Sscanf(strVal, "%d", &numVal); err == nil && fmt.Sprintf("%d", numVal) == strVal {
		return bson.M{"$or": []bson.M{
			{field: strVal},
			{field: numVal},
			{field: int32(numVal)},
		}}
	}
	return bson.M{field: strVal}
}

// aqCoerceNumeric tries to interpret value as int64, then float64, else string.
func aqCoerceNumeric(v interface{}) interface{} {
	if v == nil {
		return 0
	}
	// If already numeric from JSON parsing
	switch n := v.(type) {
	case float64:
		if n == float64(int64(n)) {
			return int64(n)
		}
		return n
	case int64:
		return n
	case int32:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		if f, err := n.Float64(); err == nil {
			return f
		}
	}

	strVal := fmt.Sprintf("%v", v)
	var intVal int64
	if _, err := fmt.Sscanf(strVal, "%d", &intVal); err == nil && fmt.Sprintf("%d", intVal) == strVal {
		return intVal
	}
	var floatVal float64
	if _, err := fmt.Sscanf(strVal, "%f", &floatVal); err == nil {
		return floatVal
	}
	return strVal
}

// aqParseInValues splits comma-separated or array values for $in.
func aqParseInValues(v interface{}) []interface{} {
	switch val := v.(type) {
	case []interface{}:
		result := make([]interface{}, 0, len(val))
		for _, item := range val {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" {
				result = append(result, aqCoerceForIn(s))
			}
		}
		return result
	case string:
		parts := strings.Split(val, ",")
		result := make([]interface{}, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, aqCoerceForIn(p))
			}
		}
		return result
	default:
		s := fmt.Sprintf("%v", v)
		parts := strings.Split(s, ",")
		result := make([]interface{}, 0, len(parts))
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				result = append(result, aqCoerceForIn(p))
			}
		}
		return result
	}
}

// aqCoerceForIn returns both string and numeric forms for $in matching.
func aqCoerceForIn(s string) interface{} {
	// For $in we need to include both string and numeric
	// MongoDB $in matches any value in the array
	var numVal int64
	if _, err := fmt.Sscanf(s, "%d", &numVal); err == nil && fmt.Sprintf("%d", numVal) == s {
		// Return as-is — we'll expand in the caller
		return numVal
	}
	return s
}

// expandInValues expands numeric values in $in to include both types.
func expandInValues(values []interface{}) []interface{} {
	result := make([]interface{}, 0, len(values)*2)
	for _, v := range values {
		result = append(result, v)
		switch n := v.(type) {
		case int64:
			result = append(result, fmt.Sprintf("%d", n))
			result = append(result, int32(n))
		case string:
			var numVal int64
			if _, err := fmt.Sscanf(n, "%d", &numVal); err == nil && fmt.Sprintf("%d", numVal) == n {
				result = append(result, numVal)
				result = append(result, int32(numVal))
			}
		}
	}
	return result
}

// ════════════════════════════════════════════════════════════
// Query Execution
// ════════════════════════════════════════════════════════════

// ExecuteAdvancedQuery runs a structured query and returns paginated results.
func ExecuteAdvancedQuery(req *AdvancedQueryRequest) (*PaginatedResult, error) {
	// Validate collection
	validLayers := GetAllLayerNames()
	valid := false
	for _, l := range validLayers {
		if l == req.Collection {
			valid = true
			break
		}
	}
	if !valid {
		return nil, fmt.Errorf("invalid collection: %s", req.Collection)
	}

	// Find MongoDB collection ID
	collectionID, err := FindCollectionID(req.PwaCode, req.Collection)
	if err != nil {
		return nil, fmt.Errorf("collection not found for %s/%s: %w", req.PwaCode, req.Collection, err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	coll := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))

	// ── Build filter from conditions tree ──
	filter := bson.M{}
	if req.Conditions != nil && len(req.Conditions) > 0 {
		condFilter, err := TranslateConditions(req.Collection, req.Conditions, 0)
		if err != nil {
			return nil, fmt.Errorf("query error: %w", err)
		}
		if len(condFilter) > 0 {
			filter = condFilter
		}
	}

	// ── Add date filter ──
	if req.StartDate != "" || req.EndDate != "" {
		layerCfg := LayerConfigs[req.Collection]
		dateField := "properties." + layerCfg.DateField
		dateFilter := buildDateFilter(dateField, req.StartDate, req.EndDate)
		if dateFilter != nil {
			if len(filter) > 0 {
				filter = bson.M{"$and": []bson.M{filter, dateFilter}}
			} else {
				filter = dateFilter
			}
		}
	}

	// ── Count ──
	total, err := coll.CountDocuments(ctx, filter)
	if err != nil {
		return nil, fmt.Errorf("count error: %w", err)
	}

	// Apply limit cap
	limit := req.Limit
	if limit <= 0 || limit > 10000 {
		limit = 5000
	}
	if total > int64(limit) {
		total = int64(limit)
	}

	// ── Pagination ──
	page := req.Page
	if page < 1 {
		page = 1
	}
	pageSize := req.PageSize
	if pageSize < 1 || pageSize > 200 {
		pageSize = 50
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

	skip := int64((page - 1) * pageSize)

	// ── Sort ──
	sortKey := "properties.recordDate"
	sortDir := -1
	if req.SortBy != "" {
		if validated, err := validateField(req.Collection, req.SortBy); err == nil {
			sortKey = validated
		}
		if req.SortOrder == "asc" {
			sortDir = 1
		}
	}

	// ── Query ──
	opts := options.Find().
		SetSkip(skip).
		SetLimit(int64(pageSize)).
		SetSort(bson.D{{Key: sortKey, Value: sortDir}}).
		SetProjection(bson.M{
			"properties": 1,
			"_id":        1,
		})

	cursor, err := coll.Find(ctx, filter, opts)
	if err != nil {
		return nil, fmt.Errorf("query error: %w", err)
	}
	defer cursor.Close(ctx)

	// ── Process results (same pattern as ListFeaturesPaginated) ──
	mapping := FieldMapping[req.Collection]
	var data []map[string]interface{}

	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			log.Printf("[AdvancedQuery] decode error: %v", err)
			continue
		}

		props, ok := doc["properties"].(bson.M)
		if !ok {
			continue
		}

		rawProps := make(map[string]interface{}, len(props))
		for k, v := range props {
			switch val := v.(type) {
			case primitive.DateTime:
				rawProps[k] = val.Time().Format(time.RFC3339)
			case primitive.ObjectID:
				rawProps[k] = val.Hex()
			case bson.M, bson.A:
				continue
			default:
				rawProps[k] = v
			}
		}

		var row map[string]interface{}
		if len(mapping) > 0 {
			row = MapProperties(req.Collection, rawProps)
		} else {
			row = rawProps
		}

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

	columns := buildColumnInfo(req.Collection)

	log.Printf("[AdvancedQuery] %s/%s page=%d total=%d rows=%d",
		req.PwaCode, req.Collection, page, total, len(data))

	return &PaginatedResult{
		Data:       data,
		Columns:    columns,
		Page:       page,
		PageSize:   pageSize,
		Total:      total,
		TotalPages: totalPages,
	}, nil
}

// ════════════════════════════════════════════════════════════
// Export: Advanced Query → GeoJSON (with geometry)
// ════════════════════════════════════════════════════════════

// ExportAdvancedQueryAsGeoJSON executes the query including geometry
// and returns a GeoJSON FeatureCollection as bytes.
func ExportAdvancedQueryAsGeoJSON(req *AdvancedQueryRequest) ([]byte, error) {
	collectionID, err := FindCollectionID(req.PwaCode, req.Collection)
	if err != nil {
		return nil, fmt.Errorf("collection not found: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	coll := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))

	// Build filter
	filter := bson.M{}
	if req.Conditions != nil && len(req.Conditions) > 0 {
		condFilter, err := TranslateConditions(req.Collection, req.Conditions, 0)
		if err != nil {
			return nil, fmt.Errorf("query error: %w", err)
		}
		if len(condFilter) > 0 {
			filter = condFilter
		}
	}

	if req.StartDate != "" || req.EndDate != "" {
		layerCfg := LayerConfigs[req.Collection]
		dateField := "properties." + layerCfg.DateField
		dateFilter := buildDateFilter(dateField, req.StartDate, req.EndDate)
		if dateFilter != nil {
			if len(filter) > 0 {
				filter = bson.M{"$and": []bson.M{filter, dateFilter}}
			} else {
				filter = dateFilter
			}
		}
	}

	// Limit
	limit := req.Limit
	if limit <= 0 || limit > 10000 {
		limit = 5000
	}

	findOpts := options.Find().
		SetLimit(int64(limit)).
		SetProjection(bson.M{
			"geometry":   1,
			"properties": 1,
			"_id":        0,
		})

	cursor, err := coll.Find(ctx, filter, findOpts)
	if err != nil {
		return nil, fmt.Errorf("export query error: %w", err)
	}
	defer cursor.Close(ctx)

	type Feature struct {
		Type       string                 `json:"type"`
		Geometry   interface{}            `json:"geometry"`
		Properties map[string]interface{} `json:"properties"`
	}

	var features []Feature
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		geom := doc["geometry"]
		if geom == nil {
			continue
		}

		props := make(map[string]interface{})
		if p, ok := doc["properties"].(bson.M); ok {
			for k, v := range p {
				switch val := v.(type) {
				case primitive.DateTime:
					props[k] = val.Time().Format(time.RFC3339)
				case bson.M, bson.A:
					continue
				default:
					props[k] = v
				}
			}
		}

		features = append(features, Feature{
			Type:       "Feature",
			Geometry:   cleanBsonForJSON(geom),
			Properties: props,
		})
	}

	if features == nil {
		features = []Feature{}
	}

	fc := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
		"metadata": map[string]interface{}{
			"pwaCode":    req.PwaCode,
			"collection": req.Collection,
			"count":      len(features),
		},
	}

	return json.Marshal(fc)
}

// ════════════════════════════════════════════════════════════
// CSV Export
// ════════════════════════════════════════════════════════════

// ConvertGeoJSONToCSV converts a GeoJSON FeatureCollection to CSV bytes.
func ConvertGeoJSONToCSV(geojsonData []byte) ([]byte, error) {
	var fc struct {
		Features []struct {
			Properties map[string]interface{} `json:"properties"`
		} `json:"features"`
	}
	if err := json.Unmarshal(geojsonData, &fc); err != nil {
		return nil, fmt.Errorf("parse GeoJSON error: %w", err)
	}

	if len(fc.Features) == 0 {
		return []byte(""), nil
	}

	// Collect all unique headers from all features
	headerSet := map[string]bool{}
	for _, f := range fc.Features {
		for k := range f.Properties {
			if k != "_createdBy" {
				headerSet[k] = true
			}
		}
	}

	// Sort headers for deterministic output
	headers := make([]string, 0, len(headerSet))
	for h := range headerSet {
		headers = append(headers, h)
	}
	// Sort alphabetically
	for i := 0; i < len(headers); i++ {
		for j := i + 1; j < len(headers); j++ {
			if headers[i] > headers[j] {
				headers[i], headers[j] = headers[j], headers[i]
			}
		}
	}

	var buf strings.Builder
	w := csv.NewWriter(&buf)

	// Write header
	w.Write(headers)

	// Write rows
	for _, f := range fc.Features {
		row := make([]string, len(headers))
		for i, h := range headers {
			if v, ok := f.Properties[h]; ok && v != nil {
				row[i] = fmt.Sprintf("%v", v)
			}
		}
		w.Write(row)
	}

	w.Flush()
	if err := w.Error(); err != nil {
		return nil, fmt.Errorf("CSV write error: %w", err)
	}

	return []byte(buf.String()), nil
}
