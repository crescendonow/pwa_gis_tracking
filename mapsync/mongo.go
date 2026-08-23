package mapsync

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
)

// MongoSource adapts the existing collections/features_{id} layout without
// sharing the web application's global Mongo client.
type MongoSource struct {
	Client   *mongo.Client
	Database string
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
		if err != nil || !IsSupportedLayer(collection.Layer) {
			continue
		}
		collections = append(collections, collection)
	}
	return collections, cursor.Err()
}

func (source MongoSource) ForEach(ctx context.Context, collection Collection, since *time.Time, visit func(SourceFeature) error) error {
	filter := incrementalMongoFilter(since)
	cursor, err := source.Client.Database(source.Database).Collection("features_"+collection.ID).Find(ctx, filter)
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

func incrementalMongoFilter(since *time.Time) bson.M {
	if since == nil {
		return bson.M{}
	}
	replayFrom := since.UTC().Add(-IncrementalReplayOverlap)
	newer := bson.M{"$gte": replayFrom}
	return bson.M{"$or": []bson.M{
		{"_updatedAt": newer}, {"updatedAt": newer},
		{"properties._updatedAt": newer}, {"properties.updatedAt": newer},
		{"_createdAt": newer}, {"createdAt": newer},
		{"properties._createdAt": newer}, {"properties.createdAt": newer},
		{"_id": bson.M{"$type": "objectId", "$gte": primitive.NewObjectIDFromTimestamp(replayFrom)}},
	}}
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
