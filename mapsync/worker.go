package mapsync

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Source streams Mongo metadata and documents. The callback form bounds memory
// for initial and reconciliation runs.
type Source interface {
	Collections(context.Context) ([]Collection, error)
	ForEach(context.Context, Collection, *time.Time, func(SourceFeature) error) error
}

// Store is the mirror boundary. Implementations must make Upsert idempotent.
type Store interface {
	State(context.Context, Collection) (SyncState, error)
	Upsert(context.Context, []MirrorFeature) error
	RemoveAbsent(context.Context, Collection, time.Time) error
	SaveState(context.Context, Collection, SyncState) error
	EnrichDMA(context.Context) error
	RefreshSummary(context.Context) error
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
	WarmTargets       func(context.Context) ([]WarmTarget, error)
	Report            func(CycleResult, error)
	Now               func() time.Time
	cycleMu           sync.Mutex
}

// CycleResult permits service logs to report partial failures without hiding
// successful collections.
type CycleResult struct {
	Collections int                `json:"collections"`
	Succeeded   int                `json:"succeeded"`
	Failed      int                `json:"failed"`
	Skipped     int                `json:"skipped"`
	Results     []CollectionResult `json:"results"`
}

type CollectionResult struct {
	Alias   string `json:"alias"`
	Skipped int    `json:"skipped"`
	Error   string `json:"error,omitempty"`
}

// RunCycle synchronises every collection independently. A failure in one
// collection records its state and never prevents another collection's commit.
func (worker *Worker) RunCycle(ctx context.Context, forceReconciliation bool) (CycleResult, error) {
	worker.cycleMu.Lock()
	defer worker.cycleMu.Unlock()
	cycleTimeout := worker.CycleTimeout
	if cycleTimeout <= 0 {
		cycleTimeout = 14 * time.Minute
	}
	ctx, cancelCycle := context.WithTimeout(ctx, cycleTimeout)
	defer cancelCycle()
	if worker.Source == nil || worker.Store == nil {
		return CycleResult{}, errors.New("mapsync source and store are required")
	}
	collections, err := worker.Source.Collections(ctx)
	if err != nil {
		return CycleResult{}, fmt.Errorf("list source collections: %w", err)
	}
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
			collectionTimeout := worker.CollectionTimeout
			if collectionTimeout <= 0 {
				collectionTimeout = 10 * time.Minute
			}
			collectionCtx, cancelCollection := context.WithTimeout(ctx, collectionTimeout)
			defer cancelCollection()
			skipped, syncErr := worker.syncCollection(collectionCtx, collection, forceReconciliation)
			resultMu.Lock()
			result.Skipped += skipped
			collectionResult := CollectionResult{Alias: collection.Alias, Skipped: skipped}
			if syncErr == nil {
				result.Succeeded++
			} else {
				failures = append(failures, syncErr)
				result.Failed++
				collectionResult.Error = syncErr.Error()
			}
			result.Results = append(result.Results, collectionResult)
			resultMu.Unlock()
		}()
	}
	group.Wait()
	if result.Succeeded > 0 {
		if err := worker.Store.EnrichDMA(ctx); err != nil {
			failures = append(failures, fmt.Errorf("enrich dma: %w", err))
		}
		if err := worker.Store.RefreshSummary(ctx); err != nil {
			failures = append(failures, fmt.Errorf("refresh map summary: %w", err))
		}
		if worker.Warmer != nil {
			if worker.WarmTargets == nil {
				failures = append(failures, errors.New("warm tiles: PWA zone bounds provider is required"))
			} else if targets, err := worker.WarmTargets(ctx); err != nil {
				failures = append(failures, fmt.Errorf("build PWA zone warm targets: %w", err))
			} else if err := worker.Warmer.Warm(ctx, targets); err != nil {
				failures = append(failures, fmt.Errorf("warm tiles: %w", err))
			}
		}
	}
	return result, errors.Join(failures...)
}

func (worker *Worker) syncCollection(ctx context.Context, collection Collection, forceReconciliation bool) (int, error) {
	now := time.Now().UTC()
	if worker.Now != nil {
		now = worker.Now().UTC()
	}
	state, err := worker.Store.State(ctx, collection)
	if err != nil {
		return 0, fmt.Errorf("%s: read state: %w", collection.Alias, err)
	}
	full := forceReconciliation || NeedsReconciliation(now, state.LastFullSyncAt)
	watermark := state.Watermark
	if full {
		watermark = nil
	}
	batchSize := worker.BatchSize
	if batchSize < 1 {
		batchSize = 500
	}
	batch := make([]MirrorFeature, 0, batchSize)
	maxWatermark := watermark
	skipped := 0
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		if err := worker.Store.Upsert(ctx, batch); err != nil {
			return err
		}
		batch = batch[:0]
		return nil
	}
	err = worker.Source.ForEach(ctx, collection, watermark, func(source SourceFeature) error {
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
		err = worker.Store.RemoveAbsent(ctx, collection, now)
	}
	if err != nil {
		state.Status = "failed"
		state.LastError = err.Error()
		_ = worker.Store.SaveState(ctx, collection, state)
		return skipped, fmt.Errorf("%s: %w", collection.Alias, err)
	}
	state.Watermark = maxWatermark
	state.LastSuccessAt = &now
	if full {
		state.LastFullSyncAt = &now
	}
	state.Status = "ok"
	state.LastError = ""
	if err := worker.Store.SaveState(ctx, collection, state); err != nil {
		return skipped, fmt.Errorf("%s: save state: %w", collection.Alias, err)
	}
	return skipped, nil
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
