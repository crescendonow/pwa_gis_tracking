package services

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"pwa_gis_tracking/config"
	"pwa_gis_tracking/models"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// LayerConfig holds configuration for each GIS layer.
type LayerConfig struct {
	CountFields []string // Fields used for counting [globalId, specificId]
	DateField   string   // Date field for filtering (recordDate, _createdAt, _updatedAt)
}

// LayerConfigs defines the configuration for all supported GIS layers.
var LayerConfigs = map[string]LayerConfig{
	"pipe":           {CountFields: []string{"globalId", "PIPE_ID"}, DateField: "recordDate"},
	"valve":          {CountFields: []string{"globalId", "_id"}, DateField: "recordDate"},
	"firehydrant":    {CountFields: []string{"globalId", "_id"}, DateField: "recordDate"},
	"meter":          {CountFields: []string{"globalId", "custCode"}, DateField: "recordDate"},
	"bldg":           {CountFields: []string{"globalId", "BLDG_ID"}, DateField: "recordDate"},
	"leakpoint":      {CountFields: []string{"globalId", "LEAK_ID"}, DateField: "recordDate"},
	"pwa_waterworks": {CountFields: []string{"globalId", "_id"}, DateField: "_createdAt"},
	"struct":         {CountFields: []string{"globalId", "_id"}, DateField: "_createdAt"},
	"pipe_serv":      {CountFields: []string{"globalId", "_id"}, DateField: "_createdAt"},
}

// GetAllLayerNames returns all supported layer names in display order.
func GetAllLayerNames() []string {
	return []string{"pipe", "valve", "firehydrant", "meter", "bldg", "leakpoint", "pwa_waterworks", "struct", "pipe_serv"}
}

// FindCollectionID looks up the MongoDB collection ObjectID from the "collections"
// metadata collection using alias format: b{pwaCode}_{featureType}
func FindCollectionID(pwaCode string, featureType string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Prepend "b" prefix if not already present
	code := pwaCode
	if !strings.HasPrefix(code, "b") {
		code = "b" + code
	}

	alias := fmt.Sprintf("%s_%s", code, featureType)
	metaCollection := config.GetMongoCollection("collections")

	log.Printf("FindCollectionID: alias=%s, db=%s", alias, config.MongoDB.Name())

	var result struct {
		ID primitive.ObjectID `bson:"_id"`
	}
	err := metaCollection.FindOne(ctx, bson.M{"alias": alias}).Decode(&result)
	if err != nil {
		return "", fmt.Errorf("collection not found: %s", alias)
	}
	return result.ID.Hex(), nil
}

// CountFeatures counts the number of documents in a feature collection.
// Supports optional date range filtering via startDate/endDate (format: YYYY-MM-DD).
func CountFeatures(pwaCode string, layerName string, startDate, endDate string) (int64, error) {
	collectionID, err := FindCollectionID(pwaCode, layerName)
	if err != nil {
		log.Printf("FindCollectionID failed: %s_%s -> %v", pwaCode, layerName, err)
		return 0, nil // No collection found = 0 count
	}
	log.Printf("Found collection: %s_%s -> features_%s", pwaCode, layerName, collectionID)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	featuresCol := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))
	filter := bson.M{}

	// Build date filter if date range is provided
	if startDate != "" || endDate != "" {
		layerCfg := LayerConfigs[layerName]
		dateField := "properties." + layerCfg.DateField
		dateFilter := buildDateFilter(dateField, startDate, endDate)
		if dateFilter != nil {
			filter = dateFilter
		}
	}

	count, err := featuresCol.CountDocuments(ctx, filter)
	if err != nil {
		return 0, fmt.Errorf("count failed for %s_%s: %v", pwaCode, layerName, err)
	}
	return count, nil
}

// buildDateFilter creates a MongoDB filter for date range queries.
// Supports both Date objects and ISO string values in MongoDB.
func buildDateFilter(dateField string, startDate, endDate string) bson.M {
	conditions := bson.M{}

	if startDate != "" {
		t, err := time.Parse("2006-01-02", startDate)
		if err == nil {
			conditions["$gte"] = t
		}
	}

	if endDate != "" {
		t, err := time.Parse("2006-01-02", endDate)
		if err == nil {
			// Set to end of day
			t = t.Add(23*time.Hour + 59*time.Minute + 59*time.Second)
			conditions["$lte"] = t
		}
	}

	if len(conditions) == 0 {
		return nil
	}

	// Support both Date object and string format; fallback to _createdAt
	return bson.M{
		"$or": []bson.M{
			{dateField: conditions},
			{"properties._createdAt": conditions},
		},
	}
}

// CountAllLayersForBranch counts features across all layers for a single branch (concurrent).
func CountAllLayersForBranch(pwaCode string, startDate, endDate string) (map[string]int64, error) {
	layers := GetAllLayerNames()
	result := make(map[string]int64)

	var wg sync.WaitGroup
	var mu sync.Mutex
	errChan := make(chan error, len(layers))

	for _, layer := range layers {
		wg.Add(1)
		go func(l string) {
			defer wg.Done()
			count, err := CountFeatures(pwaCode, l, startDate, endDate)
			if err != nil {
				errChan <- err
				return
			}
			mu.Lock()
			result[l] = count
			mu.Unlock()
		}(layer)
	}

	wg.Wait()
	close(errChan)

	return result, nil
}

// GetBranchCountsSummary returns feature counts for all branches (concurrent).
func GetBranchCountsSummary(offices []models.PwaOffice, startDate, endDate string) ([]models.FeatureCountByBranch, error) {
	var results []models.FeatureCountByBranch
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Limit concurrency to avoid overwhelming MongoDB
	sem := make(chan struct{}, 10)

	for _, office := range offices {
		wg.Add(1)
		sem <- struct{}{}
		go func(o models.PwaOffice) {
			defer wg.Done()
			defer func() { <-sem }()

			layers, err := CountAllLayersForBranch(o.PwaCode, startDate, endDate)
			if err != nil {
				log.Printf("Error counting for %s: %v", o.PwaCode, err)
				return
			}

			var total int64
			for _, c := range layers {
				total += c
			}

			mu.Lock()
			results = append(results, models.FeatureCountByBranch{
				PwaCode:    o.PwaCode,
				BranchName: o.Name,
				Zone:       o.Zone,
				Layers:     layers,
				Total:      total,
			})
			mu.Unlock()
		}(office)
	}

	wg.Wait()
	return results, nil
}

// GetCollectionsForPwaCode retrieves all MongoDB collections matching a pwaCode prefix.
func GetCollectionsForPwaCode(pwaCode string) ([]map[string]interface{}, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Prepend "b" prefix if not already present
	code := pwaCode
	if !strings.HasPrefix(code, "b") {
		code = "b" + code
	}

	metaCollection := config.GetMongoCollection("collections")
	pattern := fmt.Sprintf("^%s_", regexp.QuoteMeta(code))
	filter := bson.M{"alias": primitive.Regex{Pattern: pattern, Options: "i"}}

	cursor, err := metaCollection.Find(ctx, filter)
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)

	var results []map[string]interface{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}
		results = append(results, map[string]interface{}{
			"id":    doc["_id"].(primitive.ObjectID).Hex(),
			"alias": doc["alias"],
		})
	}
	return results, nil
}

// ExportFeaturesAsGeoJSON exports features as a GeoJSON FeatureCollection byte array.
// Supports optional date range filtering. Returns all properties per feature.
func ExportFeaturesAsGeoJSON(pwaCode, layerName, startDate, endDate string) ([]byte, error) {
	collectionID, err := FindCollectionID(pwaCode, layerName)
	if err != nil {
		return nil, fmt.Errorf("collection not found: %s_%s", pwaCode, layerName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	featuresCol := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))

	filter := bson.M{}
	if startDate != "" || endDate != "" {
		layerCfg := LayerConfigs[layerName]
		dateField := "properties." + layerCfg.DateField
		dateFilter := buildDateFilter(dateField, startDate, endDate)
		if dateFilter != nil {
			filter = dateFilter
		}
	}

	projection := options.Find().SetProjection(bson.M{
		"geometry":   1,
		"properties": 1,
		"_id":        0,
	})

	cursor, err := featuresCol.Find(ctx, filter, projection)
	if err != nil {
		return nil, fmt.Errorf("query failed: %v", err)
	}
	defer cursor.Close(ctx)

	type Feature struct {
		Type       string                 `json:"type"`
		Geometry   interface{}            `json:"geometry"`
		Properties map[string]interface{} `json:"properties"`
	}

	features := []Feature{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		geom := doc["geometry"]
		if geom == nil {
			continue
		}

		// Clean properties: convert BSON types, skip nested objects/arrays
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

	fc := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
		"metadata": map[string]interface{}{
			"pwaCode":    pwaCode,
			"collection": layerName,
			"count":      len(features),
		},
	}

	return json.Marshal(fc)
}

// cleanBsonForJSON recursively converts BSON types to JSON-serializable Go types.
func cleanBsonForJSON(v interface{}) interface{} {
	switch val := v.(type) {
	case bson.M:
		result := make(map[string]interface{})
		for k, v2 := range val {
			result[k] = cleanBsonForJSON(v2)
		}
		return result
	case bson.A:
		result := make([]interface{}, len(val))
		for i, v2 := range val {
			result[i] = cleanBsonForJSON(v2)
		}
		return result
	case primitive.DateTime:
		return val.Time().Format(time.RFC3339)
	case primitive.ObjectID:
		return val.Hex()
	default:
		return v
	}
}

// SumPipeLength aggregates the total pipe length (in meters) for a branch.
// Reads properties.PIPE_LONG from the pipe collection and sums it.
// Handles both numeric and string values using $toDouble with $ifNull fallback.
// Also tries alternative field names: PIPE_LONG, pipe_long, pipeLength, PIPE_LEN.
func SumPipeLength(pwaCode string, startDate, endDate string) (float64, error) {
	collectionID, err := FindCollectionID(pwaCode, "pipe")
	if err != nil {
		return 0, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	featuresCol := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))
	filter := bson.M{}
	if startDate != "" || endDate != "" {
		dateFilter := buildDateFilter("properties.recordDate", startDate, endDate)
		if dateFilter != nil {
			filter = dateFilter
		}
	}

	// Try each possible field name for pipe length
	fieldNames := []string{"length", "PIPE_LONG", "pipe_long", "pipeLength", "PIPE_LEN", "pipe_len"}
	for _, fieldName := range fieldNames {
		total := sumFieldAsDouble(ctx, featuresCol, filter, "properties."+fieldName)
		if total > 0 {
			return total, nil
		}
	}

	return 0, nil
}

// sumFieldAsDouble aggregates $sum on a field, converting string values to double.
func sumFieldAsDouble(ctx context.Context, col *mongo.Collection, filter bson.M, field string) float64 {
	pipeline := []bson.M{
		{"$match": filter},
		{"$group": bson.M{
			"_id": nil,
			"total": bson.M{
				"$sum": bson.M{
					"$convert": bson.M{
						"input":   "$" + field,
						"to":      "double",
						"onError": 0,
						"onNull":  0,
					},
				},
			},
		}},
	}

	cursor, err := col.Aggregate(ctx, pipeline)
	if err != nil {
		return 0
	}
	defer cursor.Close(ctx)

	var result struct {
		Total float64 `bson:"total"`
	}
	if cursor.Next(ctx) {
		if err := cursor.Decode(&result); err != nil {
			return 0
		}
	}
	return result.Total
}

// ExportFeaturesForMap returns lightweight GeoJSON for map rendering.
// Only includes geometry + _id + typeId (for pipe coloring); properties loaded on-demand.
func ExportFeaturesForMap(pwaCode, layerName, startDate, endDate string) ([]byte, error) {
	collectionID, err := FindCollectionID(pwaCode, layerName)
	if err != nil {
		return nil, fmt.Errorf("collection not found: %s_%s", pwaCode, layerName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	featuresCol := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))

	filter := bson.M{}
	if startDate != "" || endDate != "" {
		layerCfg := LayerConfigs[layerName]
		dateField := "properties." + layerCfg.DateField
		dateFilter := buildDateFilter(dateField, startDate, endDate)
		if dateFilter != nil {
			filter = dateFilter
		}
	}

	// Lightweight projection: geometry + _id + typeId for pipe coloring
	projection := options.Find().SetProjection(bson.M{
		"geometry":          1,
		"_id":               1,
		"properties.typeId": 1,
	})

	cursor, err := featuresCol.Find(ctx, filter, projection)
	if err != nil {
		return nil, fmt.Errorf("query failed: %v", err)
	}
	defer cursor.Close(ctx)

	type Feature struct {
		Type       string                 `json:"type"`
		ID         string                 `json:"id,omitempty"`
		Geometry   interface{}            `json:"geometry"`
		Properties map[string]interface{} `json:"properties"`
	}

	features := []Feature{}
	for cursor.Next(ctx) {
		var doc bson.M
		if err := cursor.Decode(&doc); err != nil {
			continue
		}

		geom := doc["geometry"]
		if geom == nil {
			continue
		}

		// Extract _id as string
		var featureID string
		if oid, ok := doc["_id"].(primitive.ObjectID); ok {
			featureID = oid.Hex()
		}

		// Extract minimal properties (typeId for pipe color matching)
		props := make(map[string]interface{})
		props["_fid"] = featureID
		if p, ok := doc["properties"].(bson.M); ok {
			if typeId, exists := p["typeId"]; exists {
				props["typeId"] = fmt.Sprintf("%v", typeId)
			}
		}

		features = append(features, Feature{
			Type:       "Feature",
			ID:         featureID,
			Geometry:   cleanBsonForJSON(geom),
			Properties: props,
		})
	}

	fc := map[string]interface{}{
		"type":     "FeatureCollection",
		"features": features,
	}

	return json.Marshal(fc)
}

// GetFeatureProperties returns full properties for a single feature by ObjectID.
// Used for lazy-loading on map click.
func GetFeatureProperties(pwaCode, layerName, featureID string) (map[string]interface{}, error) {
	collectionID, err := FindCollectionID(pwaCode, layerName)
	if err != nil {
		return nil, fmt.Errorf("collection not found: %s_%s", pwaCode, layerName)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	featuresCol := config.GetMongoCollection(fmt.Sprintf("features_%s", collectionID))

	oid, err := primitive.ObjectIDFromHex(featureID)
	if err != nil {
		return nil, fmt.Errorf("invalid feature ID: %s", featureID)
	}

	var doc bson.M
	err = featuresCol.FindOne(ctx, bson.M{"_id": oid}, options.FindOne().SetProjection(bson.M{
		"properties": 1,
		"_id":        0,
	})).Decode(&doc)
	if err != nil {
		return nil, fmt.Errorf("feature not found: %s", featureID)
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

	return props, nil
}

// GetYearsFromRecordDate returns a list of years that have recorded data.
// Uses a static range since aggregating across all collections is too expensive.
func GetYearsFromRecordDate() ([]int, error) {
	currentYear := time.Now().Year()
	years := []int{}
	for y := 2017; y <= currentYear; y++ {
		years = append(years, y)
	}
	return years, nil
}

// GetLayerDisplayName returns the Thai display name for a given layer key.
func GetLayerDisplayName(layer string) string {
	names := map[string]string{
		"pipe":           "ท่อประปา",
		"valve":          "ประตูน้ำ",
		"firehydrant":    "หัวดับเพลิง",
		"meter":          "มาตรวัดน้ำ",
		"bldg":           "อาคาร",
		"leakpoint":      "จุดแตกรั่ว",
		"pwa_waterworks": "ตำแหน่งสำนักงาน",
		"struct":         "รั้วบ้าน",
		"pipe_serv":      "ท่อบริการ",
	}
	if n, ok := names[layer]; ok {
		return n
	}
	return strings.Title(layer)
}
