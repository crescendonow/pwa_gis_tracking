package mapsync

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

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

func (store PostgresStore) State(ctx context.Context, collection Collection) (SyncState, error) {
	var watermark, full, success sql.NullTime
	var state SyncState
	err := store.DB.QueryRowContext(ctx, `SELECT watermark, last_full_sync_at, last_success_at, status, COALESCE(last_error, '') FROM pwa_tracking_map.sync_state WHERE source_collection = $1`, collection.Alias).Scan(&watermark, &full, &success, &state.Status, &state.LastError)
	if err == sql.ErrNoRows {
		return SyncState{}, nil
	}
	if err != nil {
		return SyncState{}, err
	}
	state.Watermark = optionalTime(watermark)
	state.LastFullSyncAt = optionalTime(full)
	state.LastSuccessAt = optionalTime(success)
	return state, nil
}

func (store PostgresStore) Upsert(ctx context.Context, features []MirrorFeature) error {
	transaction, err := store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer transaction.Rollback()
	statement, err := transaction.PrepareContext(ctx, `INSERT INTO pwa_tracking_map.features (source_collection, layer, pwa_code, source_id, geom, properties, source_created_at, source_updated_at, record_date, synced_at)
VALUES ($1, $2, $3, $4, ST_SetSRID(ST_GeomFromGeoJSON($5), 4326), $6::jsonb, $7, $8, $9, $10)
ON CONFLICT (source_collection, source_id) DO UPDATE SET layer = EXCLUDED.layer, pwa_code = EXCLUDED.pwa_code, geom = EXCLUDED.geom, properties = EXCLUDED.properties, source_created_at = EXCLUDED.source_created_at, source_updated_at = EXCLUDED.source_updated_at, record_date = EXCLUDED.record_date, synced_at = EXCLUDED.synced_at`)
	if err != nil {
		return err
	}
	defer statement.Close()
	for _, feature := range features {
		properties, err := json.Marshal(feature.Properties)
		if err != nil {
			return fmt.Errorf("marshal %s properties: %w", feature.SourceID, err)
		}
		if _, err := statement.ExecContext(ctx, feature.SourceCollection, feature.Layer, feature.PwaCode, feature.SourceID, string(feature.GeometryJSON), properties, feature.SourceCreatedAt, feature.SourceUpdatedAt, propertyDate(feature.Properties), feature.SyncedAt); err != nil {
			return err
		}
	}
	return transaction.Commit()
}

func (store PostgresStore) RemoveAbsent(ctx context.Context, collection Collection, before time.Time) error {
	_, err := store.DB.ExecContext(ctx, `DELETE FROM pwa_tracking_map.features WHERE source_collection = $1 AND synced_at < $2`, collection.Alias, before)
	return err
}

func (store PostgresStore) SaveState(ctx context.Context, collection Collection, state SyncState) error {
	_, err := store.DB.ExecContext(ctx, `INSERT INTO pwa_tracking_map.sync_state (source_collection, watermark, last_full_sync_at, last_success_at, status, last_error, updated_at) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), now()) ON CONFLICT (source_collection) DO UPDATE SET watermark = EXCLUDED.watermark, last_full_sync_at = EXCLUDED.last_full_sync_at, last_success_at = EXCLUDED.last_success_at, status = EXCLUDED.status, last_error = EXCLUDED.last_error, updated_at = EXCLUDED.updated_at`, collection.Alias, state.Watermark, state.LastFullSyncAt, state.LastSuccessAt, state.Status, state.LastError)
	return err
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
