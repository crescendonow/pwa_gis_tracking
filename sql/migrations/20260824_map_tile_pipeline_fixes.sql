-- Follow-up fixes for the map tile pipeline described in
-- note/21_plan_for_edit_mappage_vectortile.md. Rerunnable. Never alters or
-- drops anything outside schema pwa_tracking_map.

-- S3: resumable full-scan checkpoint. NULL means no full scan in progress.
-- full_started_at pins when the current full pass began. A pass that resumes
-- across cycles writes rows with several synced_at values, so RemoveAbsent
-- must compare against this rather than the current segment's clock, or it
-- would delete everything the earlier segments wrote.
ALTER TABLE pwa_tracking_map.sync_state
    ADD COLUMN IF NOT EXISTS cursor_id text;
ALTER TABLE pwa_tracking_map.sync_state
    ADD COLUMN IF NOT EXISTS full_started_at timestamptz;

-- S1: region-level rollup collections (b{region}000_*) are stale snapshots
-- that duplicate branch-level collections. Remove any already-mirrored rows
-- and their sync state so OrderCollections (S2) does not treat them as
-- pending work.
DELETE FROM pwa_tracking_map.features
 WHERE source_collection ~ '^b[0-9]+000_';
DELETE FROM pwa_tracking_map.sync_state
 WHERE source_collection ~ '^b[0-9]+000_';

-- S7: pwa_dma.dma_boundary lives on a different server than the mirror
-- (10.250.230.81), so enrich_dma_colors() has always failed with
-- "relation does not exist" and every DMA has rendered with the same
-- fallback colour. pwa-gis-map-sync now copies colours into this local
-- table (mapsync.DMAColorSource / PostgresStore.UpsertDMAColors) so the
-- tile function never has to reach across servers.
CREATE TABLE IF NOT EXISTS pwa_tracking_map.dma_color (
    pwa_code         text NOT NULL,
    dma_id           text NOT NULL,
    sld_color_fill   text,
    sld_color_stroke text,
    updated_at       timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (pwa_code, dma_id)
);

CREATE OR REPLACE FUNCTION pwa_tracking_map.enrich_dma_colors()
RETURNS void
LANGUAGE sql
AS $$
    UPDATE pwa_tracking_map.features AS feature
       SET properties = feature.properties
           || jsonb_build_object(
                'sld_color_fill', pwa_tracking_map.css_color_or_default(dma.sld_color_fill, '#2f80ed'),
                'sld_color_stroke', pwa_tracking_map.css_color_or_default(dma.sld_color_stroke, '#1f4f8a')
              )
      FROM pwa_tracking_map.dma_color AS dma
     WHERE feature.layer = 'dma_boundary'
       AND feature.pwa_code = dma.pwa_code
       AND COALESCE(feature.properties ->> 'dma_id', feature.properties ->> 'DMA_ID', feature.properties ->> 'dmaId') = dma.dma_id;
$$;

-- S10: partial GiST index per layer plus an RBAC-filter composite index.
-- Building these after backfill (or with CREATE INDEX CONCURRENTLY run
-- separately) avoids locking the table while ~29.5M rows are loading.
CREATE INDEX IF NOT EXISTS features_geom_pwa_waterworks_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'pwa_waterworks';
CREATE INDEX IF NOT EXISTS features_geom_dma_boundary_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'dma_boundary';
CREATE INDEX IF NOT EXISTS features_geom_pipe_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'pipe';
CREATE INDEX IF NOT EXISTS features_geom_pipe_serv_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'pipe_serv';
CREATE INDEX IF NOT EXISTS features_geom_bldg_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'bldg';
CREATE INDEX IF NOT EXISTS features_geom_struct_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'struct';
CREATE INDEX IF NOT EXISTS features_geom_valve_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'valve';
CREATE INDEX IF NOT EXISTS features_geom_firehydrant_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'firehydrant';
CREATE INDEX IF NOT EXISTS features_geom_leakpoint_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'leakpoint';
CREATE INDEX IF NOT EXISTS features_geom_step_test_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'step_test';
CREATE INDEX IF NOT EXISTS features_geom_meter_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'meter';
CREATE INDEX IF NOT EXISTS features_geom_flow_meter_gix
    ON pwa_tracking_map.features USING gist (geom) WHERE layer = 'flow_meter';

CREATE INDEX IF NOT EXISTS features_layer_pwa_date_idx
    ON pwa_tracking_map.features (layer, pwa_code, record_date);
