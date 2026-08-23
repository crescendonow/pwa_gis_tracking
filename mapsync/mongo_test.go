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

func TestIncrementalMongoFilterReplaysCreationAndObjectIDTimestamps(t *testing.T) {
	since := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	filter := fmt.Sprint(incrementalMongoFilter(&since))
	for _, required := range []string{"_updatedAt", "properties.updatedAt", "_createdAt", "properties.createdAt", "ObjectID"} {
		if !strings.Contains(filter, required) {
			t.Fatalf("incremental filter %q is missing %q", filter, required)
		}
	}
	want := since.Add(-IncrementalReplayOverlap)
	if !strings.Contains(filter, want.Format("2006-01-02")) {
		t.Fatalf("incremental filter does not replay from %v: %s", want, filter)
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
