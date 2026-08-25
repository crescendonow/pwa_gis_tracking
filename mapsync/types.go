// Package mapsync contains the source-to-mirror pipeline used by the standalone
// pwa-gis-map-sync service. It deliberately has no HTTP or web-handler dependency.
package mapsync

import "time"

var supportedMirrorLayers = map[string]struct{}{
	"pwa_waterworks": {}, "dma_boundary": {}, "pipe": {}, "pipe_serv": {},
	"bldg": {}, "struct": {}, "valve": {}, "firehydrant": {},
	"leakpoint": {}, "step_test": {}, "meter": {}, "flow_meter": {},
}

// IsSupportedLayer keeps the sync service from mirroring unrelated Mongo
// collections into the tile database.
func IsSupportedLayer(layer string) bool {
	_, ok := supportedMirrorLayers[layer]
	return ok
}

// Collection identifies one Mongo feature collection and its map layer.
type Collection struct {
	Alias   string
	ID      string
	PwaCode string
	Layer   string
}

// SourceFeature is the small source contract needed by the transformer.
// Document is normally a bson.M, kept as any here so tests and adapters stay simple.
type SourceFeature struct {
	ID         string
	Geometry   any
	Properties map[string]any
	CreatedAt  *time.Time
	UpdatedAt  *time.Time
}

// MirrorFeature is the PostGIS-mirror representation of a source feature.
type MirrorFeature struct {
	SourceCollection string
	Layer            string
	PwaCode          string
	SourceID         string
	GeometryJSON     []byte
	Properties       map[string]any
	SourceCreatedAt  *time.Time
	SourceUpdatedAt  *time.Time
	SyncedAt         time.Time
}

// SyncState records a collection's incremental watermark and last result.
type SyncState struct {
	Watermark      *time.Time
	LastFullSyncAt *time.Time
	LastSuccessAt  *time.Time
	Status         string
	LastError      string
	// CursorID resumes an interrupted full scan from the last checkpointed
	// document instead of restarting the collection from the beginning.
	CursorID string
	// FullStartedAt pins when the current full pass began. A full pass that
	// resumes across cycles writes rows with several different synced_at
	// values, so RemoveAbsent must compare against this instead of the
	// current segment's clock or it would delete every row the earlier
	// segments wrote. Nil when no full pass is in progress.
	FullStartedAt *time.Time
}

// Cursor tells a Source whether to read incrementally (Since set) or as a
// full scan, optionally resuming a full scan already in progress (AfterID).
type Cursor struct {
	Since   *time.Time // nil = full scan
	AfterID string     // full-scan resume point (Mongo ObjectID hex)
}

// DMAColor is one DMA boundary's browser-safe fill/stroke colour, sourced
// from the authoritative PostGIS database and mirrored into pwa_tracking_map
// so the tile function never has to reach across servers.
type DMAColor struct {
	PwaCode string
	DMAID   string
	Fill    string
	Stroke  string
}
