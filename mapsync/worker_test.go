package mapsync

import (
	"context"
	"testing"
	"time"
)

type testSource struct {
	collection Collection
	feature    SourceFeature
	seenSince  *time.Time
}

func (source *testSource) Collections(context.Context) ([]Collection, error) {
	return []Collection{source.collection}, nil
}
func (source *testSource) ForEach(_ context.Context, _ Collection, since *time.Time, visit func(SourceFeature) error) error {
	source.seenSince = since
	return visit(source.feature)
}

type testStore struct {
	state      SyncState
	saved      SyncState
	upserts    int
	reconciled bool
	enriched   bool
	summarized bool
}

func (store *testStore) State(context.Context, Collection) (SyncState, error) {
	return store.state, nil
}
func (store *testStore) Upsert(_ context.Context, features []MirrorFeature) error {
	store.upserts += len(features)
	return nil
}
func (store *testStore) RemoveAbsent(context.Context, Collection, time.Time) error {
	store.reconciled = true
	return nil
}
func (store *testStore) SaveState(_ context.Context, _ Collection, state SyncState) error {
	store.saved = state
	store.state = state
	return nil
}
func (store *testStore) EnrichDMA(context.Context) error      { store.enriched = true; return nil }
func (store *testStore) RefreshSummary(context.Context) error { store.summarized = true; return nil }

func TestWorkerReconcilesThenAdvancesWatermark(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	updated := now.Add(-time.Minute)
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &testSource{collection: collection, feature: SourceFeature{ID: "f1", Geometry: map[string]any{"type": "Point", "coordinates": []float64{100, 13}}, UpdatedAt: &updated}}
	store := &testStore{}
	worker := Worker{Source: source, Store: store, BatchSize: 1, Now: func() time.Time { return now }}
	result, err := worker.RunCycle(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if result.Succeeded != 1 || !store.reconciled || !store.enriched || !store.summarized || store.saved.Watermark == nil {
		t.Fatalf("unexpected first cycle: %#v %#v", result, store)
	}
	if source.seenSince != nil {
		t.Fatal("first cycle must be a full reconciliation")
	}
	store.reconciled = false
	if _, err := worker.RunCycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if source.seenSince == nil || store.reconciled {
		t.Fatal("second cycle should be incremental without reconciliation")
	}
}

func TestWorkerAdvancesWatermarkFromCreatedAt(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	created := now.Add(-time.Minute)
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &testSource{collection: collection, feature: SourceFeature{ID: "f1", Geometry: map[string]any{"type": "Point", "coordinates": []float64{100, 13}}, CreatedAt: &created}}
	store := &testStore{}
	worker := Worker{Source: source, Store: store, Now: func() time.Time { return now }}
	if _, err := worker.RunCycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if store.saved.Watermark == nil || !store.saved.Watermark.Equal(created) {
		t.Fatalf("watermark = %v, want created time", store.saved.Watermark)
	}
}

func TestWorkerReportsEveryCycleResult(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &testSource{collection: collection, feature: SourceFeature{ID: "", Geometry: nil}}
	store := &testStore{}
	called := false
	worker := Worker{Source: source, Store: store, Report: func(result CycleResult, err error) {
		called = true
		if result.Collections != 1 || err != nil {
			t.Fatalf("report = %#v, %v", result, err)
		}
	}}
	worker.runAndReport(context.Background())
	if !called {
		t.Fatal("cycle reporter was not called")
	}
}
