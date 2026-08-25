package mapsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"
)

// upsertChunkSize keeps each multi-row statement under Postgres's 65,535
// parameter limit (10 params/row => 10,000 params at 1,000 rows).
const upsertChunkSize = 1000

// upsertColumns is a single source of truth for buildUpsertStatement and the
// ON CONFLICT clause so they cannot drift out of sync.
var upsertColumns = []string{"source_collection", "layer", "pwa_code", "source_id", "geom", "properties", "source_created_at", "source_updated_at", "record_date", "synced_at"}

// PostgresStore persists the map mirror in pwa_tracking_map.
type PostgresStore struct{ DB *sql.DB }

// LoadZoneBounds derives warm coverage from the current authoritative office
// locations. Padding covers each office's service area without hard-coded
// regional rectangles.
func LoadZoneBounds(ctx context.Context, database *sql.DB) ([]Bounds, error) {
	rows, err := database.QueryContext(ctx, `
		SELECT zone::text,
		       MIN(ST_X(wkb_geometry::geometry)) - 0.6,
		       MIN(ST_Y(wkb_geometry::geometry)) - 0.6,
		       MAX(ST_X(wkb_geometry::geometry)) + 0.6,
		       MAX(ST_Y(wkb_geometry::geometry)) + 0.6
		  FROM pwa_office.pwa_office234
		 WHERE wkb_geometry IS NOT NULL
		 GROUP BY zone
		 ORDER BY zone::int`)
	if err != nil {
		return nil, fmt.Errorf("load PWA zone bounds: %w", err)
	}
	defer rows.Close()
	bounds := make([]Bounds, 0, 12)
	for rows.Next() {
		var zone string
		var value Bounds
		if err := rows.Scan(&zone, &value.West, &value.South, &value.East, &value.North); err != nil {
			return nil, err
		}
		bounds = append(bounds, value)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(bounds) == 0 {
		return nil, errors.New("no PWA zone bounds are available")
	}
	return bounds, nil
}

// States reads every collection's sync watermark in a single round-trip so a
// cycle over thousands of collections does not pay one query per collection.
func (store PostgresStore) States(ctx context.Context) (map[string]SyncState, error) {
	rows, err := store.DB.QueryContext(ctx, `SELECT source_collection, watermark, last_full_sync_at, last_success_at, status, COALESCE(last_error, ''), COALESCE(cursor_id, ''), full_started_at FROM pwa_tracking_map.sync_state`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	states := make(map[string]SyncState)
	for rows.Next() {
		var alias string
		var watermark, full, success, fullStarted sql.NullTime
		var state SyncState
		if err := rows.Scan(&alias, &watermark, &full, &success, &state.Status, &state.LastError, &state.CursorID, &fullStarted); err != nil {
			return nil, err
		}
		state.Watermark = optionalTime(watermark)
		state.LastFullSyncAt = optionalTime(full)
		state.LastSuccessAt = optionalTime(success)
		state.FullStartedAt = optionalTime(fullStarted)
		states[alias] = state
	}
	return states, rows.Err()
}

// buildUpsertStatement builds a multi-row INSERT ... ON CONFLICT for the
// given row count so a batch of features costs one round-trip instead of one
// per row. Each row consumes 10 placeholders in upsertColumns order.
func buildUpsertStatement(rows int) string {
	var values strings.Builder
	for i := 0; i < rows; i++ {
		if i > 0 {
			values.WriteString(", ")
		}
		base := i * len(upsertColumns)
		values.WriteByte('(')
		for column := 0; column < len(upsertColumns); column++ {
			if column > 0 {
				values.WriteByte(',')
			}
			placeholder := "$" + strconv.Itoa(base+column+1)
			if upsertColumns[column] == "geom" {
				values.WriteString("ST_SetSRID(ST_GeomFromGeoJSON(" + placeholder + "),4326)")
			} else if upsertColumns[column] == "properties" {
				values.WriteString(placeholder + "::jsonb")
			} else {
				values.WriteString(placeholder)
			}
		}
		values.WriteByte(')')
	}
	return "INSERT INTO pwa_tracking_map.features (" + strings.Join(upsertColumns, ", ") + ") VALUES " + values.String() +
		" ON CONFLICT (source_collection, source_id) DO UPDATE SET layer = EXCLUDED.layer, pwa_code = EXCLUDED.pwa_code, geom = EXCLUDED.geom, properties = EXCLUDED.properties, source_created_at = EXCLUDED.source_created_at, source_updated_at = EXCLUDED.source_updated_at, record_date = EXCLUDED.record_date, synced_at = EXCLUDED.synced_at"
}

func upsertArgs(features []MirrorFeature) ([]any, error) {
	args := make([]any, 0, len(features)*len(upsertColumns))
	for _, feature := range features {
		properties, err := json.Marshal(feature.Properties)
		if err != nil {
			return nil, fmt.Errorf("marshal %s properties: %w", feature.SourceID, err)
		}
		args = append(args, feature.SourceCollection, feature.Layer, feature.PwaCode, feature.SourceID, string(feature.GeometryJSON), properties, feature.SourceCreatedAt, feature.SourceUpdatedAt, propertyDate(feature.Properties), feature.SyncedAt)
	}
	return args, nil
}

func (store PostgresStore) Upsert(ctx context.Context, features []MirrorFeature) error {
	if len(features) == 0 {
		return nil
	}
	transaction, err := store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for start := 0; start < len(features); start += upsertChunkSize {
		end := start + upsertChunkSize
		if end > len(features) {
			end = len(features)
		}
		if err := upsertChunk(ctx, transaction, features[start:end]); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

// upsertChunk sends one multi-row statement. If it fails (most commonly a
// single row whose geometry ST_GeomFromGeoJSON cannot parse), the whole
// statement is rejected even though only one row is at fault, so it retries
// row-by-row and skips just the bad rows.
//
// Every attempt runs inside a savepoint. A failed statement puts the whole
// Postgres transaction into the aborted state, where every later command
// fails with 25P02 until it is rolled back, so without savepoints the retry
// would skip all rows and then lose the batch at commit time.
func upsertChunk(ctx context.Context, transaction *sql.Tx, features []MirrorFeature) error {
	args, err := upsertArgs(features)
	if err != nil {
		return err
	}
	chunkErr, err := execInSavepoint(ctx, transaction, "map_sync_chunk", buildUpsertStatement(len(features)), args)
	if err != nil {
		return err
	}
	if chunkErr == nil {
		return nil
	}
	if len(features) == 1 {
		return fmt.Errorf("upsert %s/%s: %w", features[0].SourceCollection, features[0].SourceID, chunkErr)
	}
	skipped := 0
	for _, feature := range features {
		rowArgs, argsErr := upsertArgs([]MirrorFeature{feature})
		rowErr := argsErr
		if argsErr == nil {
			rowErr, err = execInSavepoint(ctx, transaction, "map_sync_row", buildUpsertStatement(1), rowArgs)
			if err != nil {
				return err
			}
		}
		if rowErr != nil {
			skipped++
			log.Printf("map_sync_upsert_skip collection=%s source_id=%s error=%v", feature.SourceCollection, feature.SourceID, rowErr)
		}
	}
	if skipped > 0 {
		log.Printf("map_sync_upsert_chunk_recovered skipped=%d total=%d first_error=%v", skipped, len(features), chunkErr)
	}
	return nil
}

// execInSavepoint runs statement inside a named savepoint. It separates the
// statement's own failure (returned as statementErr, transaction still usable)
// from a savepoint command failure (returned as err, transaction unusable).
func execInSavepoint(ctx context.Context, transaction *sql.Tx, name, statement string, args []any) (statementErr error, err error) {
	if _, err := transaction.ExecContext(ctx, "SAVEPOINT "+name); err != nil {
		return nil, err
	}
	if _, statementErr := transaction.ExecContext(ctx, statement, args...); statementErr != nil {
		if _, err := transaction.ExecContext(ctx, "ROLLBACK TO SAVEPOINT "+name); err != nil {
			return statementErr, err
		}
		return statementErr, nil
	}
	if _, err := transaction.ExecContext(ctx, "RELEASE SAVEPOINT "+name); err != nil {
		return nil, err
	}
	return nil, nil
}

func (store PostgresStore) RemoveAbsent(ctx context.Context, collection Collection, before time.Time) error {
	_, err := store.DB.ExecContext(ctx, `DELETE FROM pwa_tracking_map.features WHERE source_collection = $1 AND synced_at < $2`, collection.Alias, before)
	return err
}

func (store PostgresStore) SaveState(ctx context.Context, collection Collection, state SyncState) error {
	_, err := store.DB.ExecContext(ctx, `INSERT INTO pwa_tracking_map.sync_state (source_collection, watermark, last_full_sync_at, last_success_at, status, last_error, cursor_id, full_started_at, updated_at) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8, now()) ON CONFLICT (source_collection) DO UPDATE SET watermark = EXCLUDED.watermark, last_full_sync_at = EXCLUDED.last_full_sync_at, last_success_at = EXCLUDED.last_success_at, status = EXCLUDED.status, last_error = EXCLUDED.last_error, cursor_id = EXCLUDED.cursor_id, full_started_at = EXCLUDED.full_started_at, updated_at = EXCLUDED.updated_at`, collection.Alias, state.Watermark, state.LastFullSyncAt, state.LastSuccessAt, state.Status, state.LastError, state.CursorID, state.FullStartedAt)
	return err
}

// dmaColorColumns mirrors upsertColumns for pwa_tracking_map.dma_color.
var dmaColorColumns = []string{"pwa_code", "dma_id", "sld_color_fill", "sld_color_stroke"}

// buildDMAColorUpsertStatement builds a multi-row INSERT ... ON CONFLICT for
// pwa_tracking_map.dma_color, mirroring buildUpsertStatement's shape so a
// batch of colours also costs one round-trip.
func buildDMAColorUpsertStatement(rows int) string {
	var values strings.Builder
	for i := 0; i < rows; i++ {
		if i > 0 {
			values.WriteString(", ")
		}
		base := i * len(dmaColorColumns)
		values.WriteByte('(')
		for column := 0; column < len(dmaColorColumns); column++ {
			if column > 0 {
				values.WriteByte(',')
			}
			values.WriteString("$" + strconv.Itoa(base+column+1))
		}
		values.WriteString(",now())")
	}
	return "INSERT INTO pwa_tracking_map.dma_color (" + strings.Join(dmaColorColumns, ", ") + ", updated_at) VALUES " + values.String() +
		" ON CONFLICT (pwa_code, dma_id) DO UPDATE SET sld_color_fill = EXCLUDED.sld_color_fill, sld_color_stroke = EXCLUDED.sld_color_stroke, updated_at = EXCLUDED.updated_at"
}

// UpsertDMAColors mirrors DMA colours read from the source database (a
// different server) into the mirror so enrich_dma_colors() never has to
// reach across servers itself.
func (store PostgresStore) UpsertDMAColors(ctx context.Context, colors []DMAColor) error {
	if len(colors) == 0 {
		return nil
	}
	transaction, err := store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	for start := 0; start < len(colors); start += upsertChunkSize {
		end := start + upsertChunkSize
		if end > len(colors) {
			end = len(colors)
		}
		chunk := colors[start:end]
		args := make([]any, 0, len(chunk)*len(dmaColorColumns))
		for _, color := range chunk {
			args = append(args, color.PwaCode, color.DMAID, color.Fill, color.Stroke)
		}
		if _, err := transaction.ExecContext(ctx, buildDMAColorUpsertStatement(len(chunk)), args...); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (store PostgresStore) EnrichDMA(ctx context.Context) error {
	_, err := store.DB.ExecContext(ctx, `SELECT pwa_tracking_map.enrich_dma_colors()`)
	return err
}
func (store PostgresStore) RefreshSummary(ctx context.Context) error {
	_, err := store.DB.ExecContext(ctx, `SELECT pwa_tracking_map.refresh_map_summary()`)
	return err
}

func optionalTime(value sql.NullTime) *time.Time {
	if !value.Valid {
		return nil
	}
	result := value.Time.UTC()
	return &result
}

func propertyDate(properties map[string]any) any {
	for _, key := range []string{"recordDate", "record_date", "date"} {
		if value, exists := properties[key]; exists {
			switch typed := value.(type) {
			case string:
				if parsed, err := time.Parse("2006-01-02", typed); err == nil {
					return parsed
				}
				if parsed, err := time.Parse(time.RFC3339, typed); err == nil {
					return parsed
				}
			case time.Time:
				return typed
			}
		}
	}
	return nil
}
