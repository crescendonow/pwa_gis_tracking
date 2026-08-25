package mapsync

import (
	"context"
	"fmt"
	"log"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

// MongoSource adapts the existing collections/features_{id} layout without
// sharing the web application's global Mongo client.
type MongoSource struct {
	Client   *mongo.Client
	Database string
	// IncludeRollups keeps region-level (b{region}000_*) collections in the
	// mirror. They are stale duplicates of branch collections, so this
	// defaults to false and exists only as an operator escape hatch.
	IncludeRollups bool
}

func (source MongoSource) Collections(ctx context.Context) ([]Collection, error) {
	cursor, err := source.Client.Database(source.Database).Collection("collections").Find(ctx, bson.M{"alias": primitive.Regex{Pattern: `^b[^_]+_.+$`, Options: "i"}})
	if err != nil {
		return nil, err
	}
	defer cursor.Close(ctx)
	collections := make([]Collection, 0)
	for cursor.Next(ctx) {
		var document bson.M
		if err := cursor.Decode(&document); err != nil {
			return nil, err
		}
		collection, err := ParseCollectionAlias(fmt.Sprint(document["alias"]), mongoID(document["_id"]))
		if err != nil {
			continue
		}
		collections = append(collections, collection)
	}
	if err := cursor.Err(); err != nil {
		return nil, err
	}
	return FilterCollections(collections, source.IncludeRollups), nil
}

// FilterCollections applies the supported-layer allow-list and, unless
// includeRollups is set, drops region-level rollup collections. It is a pure
// function so the queue-shaping rules can be tested without a live Mongo.
func FilterCollections(collections []Collection, includeRollups bool) []Collection {
	filtered := make([]Collection, 0, len(collections))
	for _, collection := range collections {
		if !IsSupportedLayer(collection.Layer) {
			continue
		}
		if !includeRollups && IsRollupPwaCode(collection.PwaCode) {
			continue
		}
		filtered = append(filtered, collection)
	}
	return filtered
}

func (source MongoSource) ForEach(ctx context.Context, collection Collection, position Cursor, visit func(SourceFeature) error) error {
	filter, findOptions := mongoFindArgs(collection, position)
	cursor, err := source.Client.Database(source.Database).Collection("features_"+collection.ID).Find(ctx, filter, findOptions)
	if err != nil {
		return err
	}
	defer cursor.Close(ctx)
	for cursor.Next(ctx) {
		var document bson.M
		if err := cursor.Decode(&document); err != nil {
			return err
		}
		properties, _ := normaliseMongoValue(document["properties"]).(map[string]any)
		createdAt := mongoTime(document["_createdAt"], document["createdAt"], mongoProperty(document["properties"], "_createdAt"), mongoProperty(document["properties"], "createdAt"))
		updatedAt := mongoTime(document["_updatedAt"], document["updatedAt"], mongoProperty(document["properties"], "_updatedAt"), mongoProperty(document["properties"], "updatedAt"))
		if updatedAt == nil {
			updatedAt = createdAt
		}
		if updatedAt == nil {
			updatedAt = mongoObjectIDTime(document["_id"])
		}
		feature := SourceFeature{
			ID: mongoID(document["_id"]), Geometry: normaliseMongoValue(document["geometry"]), Properties: properties,
			CreatedAt: createdAt,
			UpdatedAt: updatedAt,
		}
		if err := visit(feature); err != nil {
			return err
		}
	}
	return cursor.Err()
}

// mongoFindArgs picks the filter and sort/batch options for one ForEach call.
// Incremental reads use the properties._updatedAt_1 index that exists on
// every collection. Full scans use the _id_ index so no in-memory sort is
// needed and a mid-scan timeout can resume with $gt on the last seen _id.
func mongoFindArgs(collection Collection, position Cursor) (bson.M, *options.FindOptions) {
	if position.Since != nil {
		findOptions := options.Find().SetSort(bson.D{{Key: "properties._updatedAt", Value: 1}, {Key: "_id", Value: 1}}).SetBatchSize(1000)
		return incrementalMongoFilter(position.Since), findOptions
	}
	findOptions := options.Find().SetSort(bson.D{{Key: "_id", Value: 1}}).SetBatchSize(1000)
	filter := bson.M{}
	if afterID := strings.TrimSpace(position.AfterID); afterID != "" {
		objectID, err := primitive.ObjectIDFromHex(afterID)
		if err != nil {
			log.Printf("map_sync_cursor_invalid collection=%s cursor=%q error=%v: restarting full scan from the beginning", collection.Alias, afterID, err)
		} else {
			filter = bson.M{"_id": bson.M{"$gt": objectID}}
		}
	}
	return filter, findOptions
}

// incrementalMongoFilter matches on properties._updatedAt alone. Every feature
// collection carries a properties._updatedAt_1 index and stores its timestamps
// there; the top-level _updatedAt/updatedAt fields do not exist. An $or across
// those unindexed fields made MongoDB fall back to a collection scan of all
// 44M documents on every 15-minute cycle, and combined with the sort in
// mongoFindArgs it would also force a 32MB in-memory sort. Documents with no
// properties._updatedAt are picked up by the daily full reconciliation.
func incrementalMongoFilter(since *time.Time) bson.M {
	if since == nil {
		return bson.M{}
	}
	return bson.M{"properties._updatedAt": bson.M{"$gte": since.UTC().Add(-IncrementalReplayOverlap)}}
}

func mongoObjectIDTime(value any) *time.Time {
	objectID, ok := value.(primitive.ObjectID)
	if !ok {
		return nil
	}
	parsed := objectID.Timestamp().UTC()
	return &parsed
}

func mongoID(value any) string {
	switch typed := value.(type) {
	case primitive.ObjectID:
		return typed.Hex()
	case string:
		return typed
	default:
		return fmt.Sprint(value)
	}
}

func mongoTime(values ...any) *time.Time {
	for _, value := range values {
		if properties, ok := value.(bson.M); ok {
			if parsed := mongoTime(properties["_updatedAt"], properties["updatedAt"], properties["_createdAt"]); parsed != nil {
				return parsed
			}
			continue
		}
		switch typed := value.(type) {
		case primitive.DateTime:
			parsed := typed.Time().UTC()
			return &parsed
		case time.Time:
			parsed := typed.UTC()
			return &parsed
		case string:
			if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
				parsed = parsed.UTC()
				return &parsed
			}
		}
	}
	return nil
}

func mongoProperty(value any, key string) any {
	switch properties := value.(type) {
	case bson.M:
		return properties[key]
	case map[string]any:
		return properties[key]
	default:
		return nil
	}
}

func normaliseMongoValue(value any) any {
	switch typed := value.(type) {
	case bson.M:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			result[key] = normaliseMongoValue(child)
		}
		return result
	case bson.D:
		result := make(map[string]any, len(typed))
		for _, child := range typed {
			result[child.Key] = normaliseMongoValue(child.Value)
		}
		return result
	case bson.A:
		result := make([]any, len(typed))
		for i, child := range typed {
			result[i] = normaliseMongoValue(child)
		}
		return result
	case primitive.DateTime:
		return typed.Time().UTC().Format(time.RFC3339Nano)
	case primitive.ObjectID:
		return typed.Hex()
	case primitive.Decimal128:
		return typed.String()
	default:
		return value
	}
}

// CollectionName is useful to operations tooling and prevents independent
// callers from duplicating the source collection naming convention.
func (collection Collection) CollectionName() string {
	return "features_" + strings.TrimSpace(collection.ID)
}
