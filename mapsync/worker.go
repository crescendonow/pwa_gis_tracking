package mapsync

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Source streams Mongo metadata and documents. The callback form bounds memory
// for initial and reconciliation runs.
type Source interface {
	Collections(context.Context) ([]Collection, error)
	ForEach(context.Context, Collection, Cursor, func(SourceFeature) error) error
}

// Store is the mirror boundary. Implementations must make Upsert idempotent.
type Store interface {
	States(context.Context) (map[string]SyncState, error)
	Upsert(context.Context, []MirrorFeature) error
	RemoveAbsent(context.Context, Collection, time.Time) error
	SaveState(context.Context, Collection, SyncState) error
	UpsertDMAColors(context.Context, []DMAColor) error
	EnrichDMA(context.Context) error
	RefreshSummary(context.Context) error
}

// OrderCollections queues collections that have never completed a full sync
// first, so long-running historical collections cannot starve them, then
// orders the remainder by oldest last_full_sync_at. Ties (including two
// never-synced collections) break on alias so the order is deterministic.
func OrderCollections(collections []Collection, states map[string]SyncState) []Collection {
	ordered := make([]Collection, len(collections))
	copy(ordered, collections)
	sort.SliceStable(ordered, func(i, j int) bool {
		left, right := states[ordered[i].Alias].LastFullSyncAt, states[ordered[j].Alias].LastFullSyncAt
		leftNever, rightNever := left == nil, right == nil
		if leftNever != rightNever {
			return leftNever
		}
		if !leftNever && !left.Equal(*right) {
			return left.Before(*right)
		}
		return ordered[i].Alias < ordered[j].Alias
	})
	return ordered
}

// Warmer warms only loopback Martin tile URLs after a successful sync.
type Warmer interface {
	Warm(context.Context, []WarmTarget) error
}

// Worker is safe to call repeatedly from an NSSM long-running process. One
// cycle is active at a time to prevent cache stampedes and competing watermarks.
type Worker struct {
	Source            Source
	Store             Store
	Warmer            Warmer
	BatchSize         int
	MaxConcurrent     int
	CycleTimeout      time.Duration
	CollectionTimeout time.Duration
	// FinalizeTimeout bounds EnrichDMA/RefreshSummary/Warm, which run on a
	// context detached from the cycle's own deadline (see finalizeTimeout).
	FinalizeTimeout time.Duration
	// LoadDMAColors reads current colours from the DMA source database, which
	// lives on a different server than the mirror. Optional: nil skips the
	// colour sync (the operator is expected to log why at startup).
	LoadDMAColors func(context.Context) ([]DMAColor, error)
	WarmTargets   func(context.Context) ([]WarmTarget, error)
	Report        func(CycleResult, error)
	// Progress, when set, is called once per collection as it finishes so a
	// long backfill can log periodic progress without waiting for the cycle
	// (which may run for hours) to return.
	Progress func(CollectionResult)
	Now      func() time.Time
	cycleMu  sync.Mutex
}

// CycleResult permits service logs to report partial failures without hiding
// successful collections.
type CycleResult struct {
	Collections int                `json:"collections"`
	Succeeded   int                `json:"succeeded"`
	Failed      int                `json:"failed"`
	Skipped     int                `json:"skipped"`
	Upserted    int                `json:"upserted"`
	Results     []CollectionResult `json:"results"`
}

type CollectionResult struct {
	Alias    string `json:"alias"`
	Skipped  int    `json:"skipped"`
	Upserted int    `json:"upserted"`
	Error    string `json:"error,omitempty"`
}

// finalizeTimeout defaults FinalizeTimeout to 10 minutes, matching the
// MAP_SYNC_FINALIZE_TIMEOUT default in cmd/map-sync.
func (worker *Worker) finalizeTimeout() time.Duration {
	if worker.FinalizeTimeout > 0 {
		return worker.FinalizeTimeout
	}
	return 10 * time.Minute
}

// withOptionalTimeout applies duration as a context deadline, falls back to
// defaultDuration when duration is the zero value, and imposes no deadline at
// all when duration is negative. Negative timeouts are how backfill mode asks
// for an unlimited cycle/collection budget.
func withOptionalTimeout(ctx context.Context, duration, defaultDuration time.Duration) (context.Context, context.CancelFunc) {
	switch {
	case duration < 0:
		return context.WithCancel(ctx)
	case duration == 0:
		return context.WithTimeout(ctx, defaultDuration)
	default:
		return context.WithTimeout(ctx, duration)
	}
}

// RunCycle synchronises every collection independently. A failure in one
// collection records its state and never prevents another collection's commit.
func (worker *Worker) RunCycle(ctx context.Context, forceReconciliation bool) (CycleResult, error) {
	worker.cycleMu.Lock()
	defer worker.cycleMu.Unlock()
	ctx, cancelCycle := withOptionalTimeout(ctx, worker.CycleTimeout, 14*time.Minute)
	defer cancelCycle()
	if worker.Source == nil || worker.Store == nil {
		return CycleResult{}, errors.New("mapsync source and store are required")
	}
	collections, err := worker.Source.Collections(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("list source collections: %w", err)
	}
	states, err := worker.Store.States(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("read sync state: %w", err)
	}
	collections = OrderCollections(collections, states)
	result := CycleResult{Collections: len(collections)}
	maxConcurrent := worker.MaxConcurrent
	if maxConcurrent < 1 {
		maxConcurrent = 4
	}
	sem := make(chan struct{}, maxConcurrent)
	var group sync.WaitGroup
	var resultMu sync.Mutex
	var failures []error
	for _, collection := range collections {
		collection := collection
		group.Add(1)
		go func() {
			defer group.Done()
			select {
			case sem <- struct{}{}:
			case <-ctx.Done():
				resultMu.Lock()
				failure := fmt.Errorf("%s: wait for worker: %w", collection.Alias, ctx.Err())
				failures = append(failures, failure)
				result.Failed++
				result.Results = append(result.Results, CollectionResult{Alias: collection.Alias, Error: failure.Error()})
				resultMu.Unlock()
				return
			}
			defer func() { <-sem }()
			collectionCtx, cancelCollection := withOptionalTimeout(ctx, worker.CollectionTimeout, 10*time.Minute)
			defer cancelCollection()
			skipped, upserted, syncErr := worker.syncCollection(collectionCtx, collection, states[collection.Alias], forceReconciliation)
			resultMu.Lock()
			result.Skipped += skipped
			result.Upserted += upserted
			collectionResult := CollectionResult{Alias: collection.Alias, Skipped: skipped, Upserted: upserted}
			if syncErr == nil {
				result.Succeeded++
			} else {
				failures = append(failures, syncErr)
				result.Failed++
				collectionResult.Error = syncErr.Error()
			}
			result.Results = append(result.Results, collectionResult)
			resultMu.Unlock()
			if worker.Progress != nil {
				worker.Progress(collectionResult)
			}
		}()
	}
	group.Wait()
	if result.Succeeded > 0 || result.Upserted > 0 {
		// Detach from the cycle's own deadline: by the time a full 44M-record
		// cycle reaches this point, ctx may have already expired, which used
		// to fail EnrichDMA/RefreshSummary/Warm on every single cycle (R6).
		finalizeCtx, cancelFinalize := context.WithTimeout(context.WithoutCancel(ctx), worker.finalizeTimeout())
		defer cancelFinalize()
		if worker.LoadDMAColors != nil {
			if colors, err := worker.LoadDMAColors(finalizeCtx); err != nil {
				failures = append(failures, fmt.Errorf("load dma colors: %w", err))
			} else if err := worker.Store.UpsertDMAColors(finalizeCtx, colors); err != nil {
				failures = append(failures, fmt.Errorf("upsert dma colors: %w", err))
			}
		}
		if err := worker.Store.EnrichDMA(finalizeCtx); err != nil {
			failures = append(failures, fmt.Errorf("enrich dma: %w", err))
		}
		if err := worker.Store.RefreshSummary(finalizeCtx); err != nil {
			failures = append(failures, fmt.Errorf("refresh map summary: %w", err))
		}
		if worker.Warmer != nil {
			if worker.WarmTargets == nil {
				failures = append(failures, errors.New("warm tiles: PWA zone bounds provider is required"))
			} else if targets, err := worker.WarmTargets(finalizeCtx); err != nil {
				failures = append(failures, fmt.Errorf("build PWA zone warm targets: %w", err))
			} else if err := worker.Warmer.Warm(finalizeCtx, targets); err != nil {
				failures = append(failures, fmt.Errorf("warm tiles: %w", err))
			}
		}
	}
	return result, errors.Join(failures...)
}

func (worker *Worker) syncCollection(ctx context.Context, collection Collection, state SyncState, forceReconciliation bool) (skipped int, upserted int, err error) {
	now := time.Now().UTC()
	if worker.Now != nil {
		now = worker.Now().UTC()
	}
	full := forceReconciliation || NeedsReconciliation(now, state.LastFullSyncAt)
	cursor := Cursor{}
	maxWatermark := state.Watermark
	fullStartedAt := state.FullStartedAt
	if full {
		cursor.AfterID = state.CursorID
		maxWatermark = nil
		if cursor.AfterID == "" || fullStartedAt == nil {
			fullStartedAt = &now
		}
	} else {
		cursor.Since = state.Watermark
	}
	batchSize := worker.BatchSize
	if batchSize < 1 {
		batchSize = 500
	}
	batch := make([]MirrorFeature, 0, batchSize)
	// visitedID tracks the last document read from the source, including
	// skipped ones, so a resumed scan never re-reads a broken document
	// forever. checkpointedCursorID only advances once its batch is durably
	// upserted and saved, so an error mid-batch never reports progress that
	// was not actually committed.
	visitedID := ""
	checkpointedCursorID := state.CursorID
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := worker.Store.Upsert(ctx, batch); err != nil {
			return err
		}
		upserted += len(batch)
		batch = batch[:0]
		if full {
			checkpointedCursorID = visitedID
			checkpoint := state
			checkpoint.CursorID = checkpointedCursorID
			checkpoint.FullStartedAt = fullStartedAt
			checkpoint.Status = "running"
			if err := worker.Store.SaveState(ctx, collection, checkpoint); err != nil {
				return err
			}
		}
		return nil
	}
	err = worker.Source.ForEach(ctx, collection, cursor, func(source SourceFeature) error {
		visitedID = source.ID
		feature, transformErr := TransformFeature(collection, source, now)
		if transformErr != nil {
			skipped++
			return nil
		}
		batch = append(batch, feature)
		candidate := source.UpdatedAt
		if candidate == nil {
			candidate = source.CreatedAt
		}
		if candidate != nil && (maxWatermark == nil || candidate.After(*maxWatermark)) {
			value := candidate.UTC()
			maxWatermark = &value
		}
		if len(batch) >= batchSize {
			return flush()
		}
		return nil
	})
	if err == nil {
		err = flush()
	}
	if err == nil && full {
		err = worker.Store.RemoveAbsent(ctx, collection, *fullStartedAt)
	}
	if err != nil {
		state.Status = "failed"
		state.LastError = err.Error()
		state.CursorID = checkpointedCursorID
		state.FullStartedAt = fullStartedAt
		_ = worker.Store.SaveState(ctx, collection, state)
		err = fmt.Errorf("%s: %w", collection.Alias, err)
		return skipped, upserted, err
	}
	state.Watermark = maxWatermark
	state.LastSuccessAt = &now
	if full {
		state.LastFullSyncAt = &now
		state.CursorID = ""
		state.FullStartedAt = nil
	}
	state.Status = "ok"
	state.LastError = ""
	if err := worker.Store.SaveState(ctx, collection, state); err != nil {
		return skipped, upserted, fmt.Errorf("%s: save state: %w", collection.Alias, err)
	}
	return skipped, upserted, nil
}

// Run is the NSSM service loop. The first cycle runs immediately; subsequent
// cycles are exactly the configured incremental cadence.
func (worker *Worker) Run(ctx context.Context) error {
	worker.runAndReport(ctx)
	ticker := time.NewTicker(IncrementalInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			worker.runAndReport(ctx)
		}
	}
}

func (worker *Worker) runAndReport(ctx context.Context) {
	result, err := worker.RunCycle(ctx, false)
	if worker.Report != nil {
		worker.Report(result, err)
	}
}
