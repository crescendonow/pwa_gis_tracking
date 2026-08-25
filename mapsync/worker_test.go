package mapsync

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testSource struct {
	collection Collection
	feature    SourceFeature
	seenCursor Cursor
}

func (source *testSource) Collections(context.Context) ([]Collection, error) {
	return []Collection{source.collection}, nil
}
func (source *testSource) ForEach(_ context.Context, _ Collection, cursor Cursor, visit func(SourceFeature) error) error {
	source.seenCursor = cursor
	return visit(source.feature)
}

type testStore struct {
	states         map[string]SyncState // optional pre-seeded multi-collection state
	state          SyncState            // legacy single-collection state, used when states is nil
	saved          SyncState
	savedHistory   []SyncState
	statesCalls    int
	saveStateCalls int
	upserts        int
	reconciled     bool
	reconciledFrom time.Time
	enriched       bool
	summarized     bool
	dmaColors      []DMAColor

	onUpsertDMAColors func()
	onEnrichDMA       func()
}

func (store *testStore) States(context.Context) (map[string]SyncState, error) {
	store.statesCalls++
	if store.states != nil {
		return store.states, nil
	}
	return map[string]SyncState{"b10_pipe": store.state}, nil
}
func (store *testStore) Upsert(_ context.Context, features []MirrorFeature) error {
	store.upserts += len(features)
	return nil
}
func (store *testStore) RemoveAbsent(_ context.Context, _ Collection, before time.Time) error {
	store.reconciled = true
	store.reconciledFrom = before
	return nil
}
func (store *testStore) SaveState(_ context.Context, _ Collection, state SyncState) error {
	store.saveStateCalls++
	store.savedHistory = append(store.savedHistory, state)
	store.saved = state
	store.state = state
	return nil
}
func (store *testStore) UpsertDMAColors(_ context.Context, colors []DMAColor) error {
	store.dmaColors = colors
	if store.onUpsertDMAColors != nil {
		store.onUpsertDMAColors()
	}
	return nil
}
func (store *testStore) EnrichDMA(ctx context.Context) error {
	store.enriched = true
	if store.onEnrichDMA != nil {
		store.onEnrichDMA()
	}
	return ctx.Err()
}
func (store *testStore) RefreshSummary(ctx context.Context) error {
	store.summarized = true
	return ctx.Err()
}

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
	if source.seenCursor.Since != nil {
		t.Fatal("first cycle must be a full reconciliation")
	}
	store.reconciled = false
	if _, err := worker.RunCycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if source.seenCursor.Since == nil || store.reconciled {
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

func TestOrderCollectionsPutsNeverSyncedFirst(t *testing.T) {
	synced := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	collections := []Collection{{Alias: "b1_pipe"}, {Alias: "b2_pipe"}}
	states := map[string]SyncState{"b1_pipe": {LastFullSyncAt: &synced}}
	ordered := OrderCollections(collections, states)
	if ordered[0].Alias != "b2_pipe" {
		t.Fatalf("ordered = %#v, want the never-synced collection first", ordered)
	}
}

func TestOrderCollectionsIsDeterministic(t *testing.T) {
	older := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	newer := time.Date(2026, 8, 20, 0, 0, 0, 0, time.UTC)
	collections := []Collection{{Alias: "b2_pipe"}, {Alias: "b1_pipe"}, {Alias: "b3_pipe"}}
	states := map[string]SyncState{
		"b2_pipe": {LastFullSyncAt: &newer},
		"b1_pipe": {LastFullSyncAt: &older},
		"b3_pipe": {LastFullSyncAt: &older},
	}
	first := OrderCollections(collections, states)
	second := OrderCollections(collections, states)
	if len(first) != 3 || first[0].Alias != "b1_pipe" || first[1].Alias != "b3_pipe" || first[2].Alias != "b2_pipe" {
		t.Fatalf("order = %#v, want oldest-first with alias tie-break", first)
	}
	for i := range first {
		if first[i].Alias != second[i].Alias {
			t.Fatalf("OrderCollections is not deterministic: %#v vs %#v", first, second)
		}
	}
}

type multiCollectionSource struct {
	collections []Collection
	feature     SourceFeature
}

func (source *multiCollectionSource) Collections(context.Context) ([]Collection, error) {
	return source.collections, nil
}
func (source *multiCollectionSource) ForEach(_ context.Context, _ Collection, _ Cursor, visit func(SourceFeature) error) error {
	return visit(source.feature)
}

func TestRunCycleReadsStatesOnce(t *testing.T) {
	collections := []Collection{
		{Alias: "b1_pipe", PwaCode: "1", Layer: "pipe"},
		{Alias: "b2_pipe", PwaCode: "2", Layer: "pipe"},
	}
	source := &multiCollectionSource{collections: collections, feature: SourceFeature{ID: "f1", Geometry: map[string]any{"type": "Point", "coordinates": []float64{100, 13}}}}
	store := &testStore{states: map[string]SyncState{}}
	worker := Worker{Source: source, Store: store}
	if _, err := worker.RunCycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if store.statesCalls != 1 {
		t.Fatalf("States called %d times, want exactly 1", store.statesCalls)
	}
}

type multiFeatureSource struct {
	collection Collection
	features   []SourceFeature
	seenCursor Cursor
	failAfter  int // index at which ForEach returns an error; -1 disables
}

func (source *multiFeatureSource) Collections(context.Context) ([]Collection, error) {
	return []Collection{source.collection}, nil
}
func (source *multiFeatureSource) ForEach(_ context.Context, _ Collection, cursor Cursor, visit func(SourceFeature) error) error {
	source.seenCursor = cursor
	for i, feature := range source.features {
		if source.failAfter >= 0 && i == source.failAfter {
			return errors.New("source failed")
		}
		if err := visit(feature); err != nil {
			return err
		}
	}
	return nil
}

func TestSyncCollectionCheckpointsAfterEachBatch(t *testing.T) {
	now := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	geometry := map[string]any{"type": "Point", "coordinates": []float64{100, 13}}
	source := &multiFeatureSource{collection: collection, failAfter: -1, features: []SourceFeature{
		{ID: "f1", Geometry: geometry}, {ID: "f2", Geometry: geometry}, {ID: "f3", Geometry: geometry},
	}}
	store := &testStore{}
	worker := &Worker{Source: source, Store: store, BatchSize: 1, Now: func() time.Time { return now }}
	skipped, upserted, err := worker.syncCollection(context.Background(), collection, SyncState{}, true)
	if err != nil || skipped != 0 || upserted != 3 {
		t.Fatalf("syncCollection() = %d, %d, %v", skipped, upserted, err)
	}
	if store.saveStateCalls < 3 {
		t.Fatalf("SaveState called %d times, want at least one checkpoint per batch", store.saveStateCalls)
	}
	for _, saved := range store.savedHistory[:len(store.savedHistory)-1] {
		if saved.Status != "running" || saved.CursorID == "" {
			t.Fatalf("interior checkpoint = %#v, want running status with a cursor", saved)
		}
	}
	final := store.savedHistory[len(store.savedHistory)-1]
	if final.Status != "ok" || final.CursorID != "" {
		t.Fatalf("final state = %#v, want ok status and cleared cursor", final)
	}
}

func TestSyncCollectionResumesFromCursor(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &multiFeatureSource{collection: collection, failAfter: -1, features: []SourceFeature{
		{ID: "f2", Geometry: map[string]any{"type": "Point", "coordinates": []float64{100, 13}}},
	}}
	store := &testStore{}
	worker := &Worker{Source: source, Store: store}
	state := SyncState{CursorID: "checkpoint123"}
	if _, _, err := worker.syncCollection(context.Background(), collection, state, false); err != nil {
		t.Fatal(err)
	}
	if source.seenCursor.AfterID != "checkpoint123" || source.seenCursor.Since != nil {
		t.Fatalf("cursor = %#v, want resume from the saved checkpoint id", source.seenCursor)
	}
}

func TestResumedFullScanKeepsRowsWrittenByEarlierSegments(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	geometry := map[string]any{"type": "Point", "coordinates": []float64{100, 13}}
	source := &multiFeatureSource{collection: collection, failAfter: -1, features: []SourceFeature{{ID: "f2", Geometry: geometry}}}
	store := &testStore{}
	// The earlier segment of this full pass ran a day ago and wrote its rows
	// with that segment's synced_at. Reconciling against the resumed
	// segment's clock instead of the pass start would delete every one of them.
	passStartedAt := time.Date(2026, 8, 23, 9, 0, 0, 0, time.UTC)
	resumedAt := passStartedAt.Add(24 * time.Hour)
	worker := &Worker{Source: source, Store: store, Now: func() time.Time { return resumedAt }}
	state := SyncState{CursorID: "f1", FullStartedAt: &passStartedAt}
	if _, _, err := worker.syncCollection(context.Background(), collection, state, true); err != nil {
		t.Fatal(err)
	}
	if !store.reconciled {
		t.Fatal("a completed full pass must reconcile deletions")
	}
	if !store.reconciledFrom.Equal(passStartedAt) {
		t.Fatalf("RemoveAbsent(before=%v), want the pass start %v", store.reconciledFrom, passStartedAt)
	}
	if store.saved.FullStartedAt != nil {
		t.Fatalf("completed pass left full_started_at = %v, want it cleared", store.saved.FullStartedAt)
	}
}

func TestSyncCollectionDoesNotRemoveAbsentOnPartialFullScan(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	geometry := map[string]any{"type": "Point", "coordinates": []float64{100, 13}}
	source := &multiFeatureSource{collection: collection, failAfter: 1, features: []SourceFeature{
		{ID: "f1", Geometry: geometry}, {ID: "f2", Geometry: geometry}, {ID: "f3", Geometry: geometry},
	}}
	store := &testStore{}
	worker := &Worker{Source: source, Store: store, BatchSize: 1}
	if _, _, err := worker.syncCollection(context.Background(), collection, SyncState{}, true); err == nil {
		t.Fatal("expected the interrupted full scan to fail")
	}
	if store.reconciled {
		t.Fatal("RemoveAbsent must not run on a partial full scan")
	}
	if store.saved.Status != "failed" {
		t.Fatalf("saved status = %q, want failed", store.saved.Status)
	}
	if store.saved.CursorID != "f1" {
		t.Fatalf("saved cursor = %q, want the last successful checkpoint f1", store.saved.CursorID)
	}
}

type deadlineCheckSource struct {
	collection    Collection
	sawDeadlineOK bool
}

func (source *deadlineCheckSource) Collections(context.Context) ([]Collection, error) {
	return []Collection{source.collection}, nil
}
func (source *deadlineCheckSource) ForEach(ctx context.Context, _ Collection, _ Cursor, visit func(SourceFeature) error) error {
	_, ok := ctx.Deadline()
	source.sawDeadlineOK = ok
	return nil
}

func TestRunCycleHonoursUnlimitedTimeouts(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &deadlineCheckSource{collection: collection}
	store := &testStore{}
	worker := Worker{Source: source, Store: store, CycleTimeout: -1, CollectionTimeout: -1}
	if _, err := worker.RunCycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if source.sawDeadlineOK {
		t.Fatal("negative timeouts must be treated as unlimited and impose no context deadline")
	}
}

func TestRunCycleAppliesDefaultTimeoutsWhenUnset(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &deadlineCheckSource{collection: collection}
	store := &testStore{}
	worker := Worker{Source: source, Store: store}
	if _, err := worker.RunCycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if !source.sawDeadlineOK {
		t.Fatal("default (zero-value) timeouts must still impose a context deadline")
	}
}

// blockingUntilDoneSource waits for its collection-scoped context to expire
// before yielding a feature, deterministically reproducing the moment the
// original bug (R6) fired: the cycle's own timeout had already elapsed by
// the time finalize ran.
type blockingUntilDoneSource struct {
	collection Collection
}

func (source *blockingUntilDoneSource) Collections(context.Context) ([]Collection, error) {
	return []Collection{source.collection}, nil
}
func (source *blockingUntilDoneSource) ForEach(ctx context.Context, _ Collection, _ Cursor, visit func(SourceFeature) error) error {
	<-ctx.Done()
	return visit(SourceFeature{ID: "f1", Geometry: map[string]any{"type": "Point", "coordinates": []float64{100, 13}}})
}

func TestRunCycleFinalizesAfterCycleDeadline(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &blockingUntilDoneSource{collection: collection}
	store := &testStore{}
	worker := &Worker{Source: source, Store: store, CycleTimeout: 30 * time.Millisecond, CollectionTimeout: 30 * time.Millisecond}
	result, err := worker.RunCycle(context.Background(), false)
	if err != nil {
		t.Fatalf("RunCycle() error = %v, result = %#v", err, result)
	}
	if !store.enriched || !store.summarized {
		t.Fatal("finalize must still run after the cycle deadline has already elapsed")
	}
}

func TestRunCycleUpsertsDMAColorsBeforeEnrichDMA(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &testSource{collection: collection, feature: SourceFeature{ID: "f1", Geometry: map[string]any{"type": "Point", "coordinates": []float64{100, 13}}}}
	store := &testStore{}
	var order []string
	store.onUpsertDMAColors = func() { order = append(order, "dma_colors") }
	store.onEnrichDMA = func() { order = append(order, "enrich") }
	colors := []DMAColor{{PwaCode: "10", DMAID: "1", Fill: "#111111", Stroke: "#222222"}}
	worker := Worker{Source: source, Store: store, LoadDMAColors: func(context.Context) ([]DMAColor, error) { return colors, nil }}
	if _, err := worker.RunCycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if len(order) != 2 || order[0] != "dma_colors" || order[1] != "enrich" {
		t.Fatalf("finalize order = %v, want dma_colors before enrich", order)
	}
	if len(store.dmaColors) != 1 || store.dmaColors[0].PwaCode != "10" {
		t.Fatalf("store.dmaColors = %#v, want the loaded colors passed through", store.dmaColors)
	}
}

func TestRunCycleSkipsDMAColorsWhenLoaderIsNil(t *testing.T) {
	collection, _ := ParseCollectionAlias("b10_pipe", "collection-id")
	source := &testSource{collection: collection, feature: SourceFeature{ID: "f1", Geometry: map[string]any{"type": "Point", "coordinates": []float64{100, 13}}}}
	store := &testStore{}
	worker := Worker{Source: source, Store: store}
	if _, err := worker.RunCycle(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	if store.dmaColors != nil {
		t.Fatalf("dmaColors = %#v, want no call when LoadDMAColors is nil", store.dmaColors)
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
