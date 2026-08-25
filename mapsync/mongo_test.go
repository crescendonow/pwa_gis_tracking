package mapsync

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestMongoCreatedTimeDoesNotFallBackToUpdatedTime(t *testing.T) {
	created := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	updated := created.Add(12 * time.Hour)
	properties := bson.M{"_createdAt": primitive.NewDateTimeFromTime(created), "_updatedAt": primitive.NewDateTimeFromTime(updated)}

	got := mongoTime(mongoProperty(properties, "_createdAt"))
	if got == nil || !got.Equal(created) {
		t.Fatalf("created time = %v, want %v", got, created)
	}
	if got.Equal(updated) {
		t.Fatal("created time must not use _updatedAt")
	}
}

func TestIncrementalMongoFilterStaysOnTheIndexedField(t *testing.T) {
	since := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	filter := incrementalMongoFilter(&since)
	if len(filter) != 1 {
		t.Fatalf("incremental filter %v must match on properties._updatedAt alone, or MongoDB collection-scans every cycle", filter)
	}
	condition, ok := filter["properties._updatedAt"].(bson.M)
	if !ok {
		t.Fatalf("incremental filter %v does not use the properties._updatedAt_1 index", filter)
	}
	want := since.Add(-IncrementalReplayOverlap)
	if got, ok := condition["$gte"].(time.Time); !ok || !got.Equal(want) {
		t.Fatalf("incremental filter replays from %v, want %v", condition["$gte"], want)
	}
}

func TestMongoFindArgsResumesFullScansAndSortsForResume(t *testing.T) {
	collection := Collection{Alias: "b5511011_pipe", PwaCode: "5511011", Layer: "pipe"}
	resumeFrom := primitive.NewObjectIDFromTimestamp(time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC))

	filter, findOptions := mongoFindArgs(collection, Cursor{AfterID: resumeFrom.Hex()})
	condition, ok := filter["_id"].(bson.M)
	if !ok || condition["$gt"] != resumeFrom {
		t.Fatalf("full scan filter = %v, want _id greater than the saved cursor", filter)
	}
	if !strings.Contains(fmt.Sprint(findOptions.Sort), "_id") {
		t.Fatalf("full scan must sort by _id so a resumed scan is deterministic, got %v", findOptions.Sort)
	}

	filter, _ = mongoFindArgs(collection, Cursor{AfterID: "not-an-object-id"})
	if len(filter) != 0 {
		t.Fatalf("an unusable cursor must restart the full scan, got %v", filter)
	}
}

func TestMongoObjectIDProvidesTimestampFallback(t *testing.T) {
	want := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	got := mongoObjectIDTime(primitive.NewObjectIDFromTimestamp(want))
	if got == nil || !got.Equal(want) {
		t.Fatalf("object id timestamp = %v, want %v", got, want)
	}
}

func TestSupportedMirrorLayersRejectUnrelatedCollections(t *testing.T) {
	if !IsSupportedLayer("meter") || IsSupportedLayer("internal_audit") {
		t.Fatal("supported mirror layer allow-list is not enforced")
	}
}

func TestCollectionsSkipsRegionRollups(t *testing.T) {
	collections := []Collection{
		{Alias: "b5511000_pipe", PwaCode: "5511000", Layer: "pipe"},
		{Alias: "b5511011_pipe", PwaCode: "5511011", Layer: "pipe"},
	}
	filtered := FilterCollections(collections, false)
	if len(filtered) != 1 || filtered[0].PwaCode != "5511011" {
		t.Fatalf("filtered = %#v, want only the branch collection", filtered)
	}
	included := FilterCollections(collections, true)
	if len(included) != 2 {
		t.Fatalf("included = %#v, want both when rollups are included", included)
	}
}
