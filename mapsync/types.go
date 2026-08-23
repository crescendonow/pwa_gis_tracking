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
}
