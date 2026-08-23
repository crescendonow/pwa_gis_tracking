-- Read-optimised, disposable mirror for the map tile pipeline. MongoDB remains
-- the source of truth. This migration is intentionally rerunnable.
CREATE EXTENSION IF NOT EXISTS postgis;

CREATE SCHEMA IF NOT EXISTS pwa_tracking_map;

CREATE TABLE IF NOT EXISTS pwa_tracking_map.features (
    id                bigserial PRIMARY KEY,
    source_collection text NOT NULL,
    layer             text NOT NULL,
    pwa_code          text NOT NULL,
    source_id         text NOT NULL,
    geom              geometry(Geometry, 4326) NOT NULL,
    properties        jsonb NOT NULL DEFAULT '{}'::jsonb,
    source_created_at timestamptz,
    source_updated_at timestamptz,
    record_date       date,
    synced_at         timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT features_source_key UNIQUE (source_collection, source_id)
);
ALTER TABLE pwa_tracking_map.features
    ADD COLUMN IF NOT EXISTS record_date date;

CREATE INDEX IF NOT EXISTS features_geom_gix
    ON pwa_tracking_map.features USING gist (geom);
CREATE INDEX IF NOT EXISTS features_layer_pwa_idx
    ON pwa_tracking_map.features (layer, pwa_code);
CREATE INDEX IF NOT EXISTS features_source_updated_idx
    ON pwa_tracking_map.features (source_updated_at);
CREATE INDEX IF NOT EXISTS features_source_id_idx
    ON pwa_tracking_map.features (source_id);
CREATE INDEX IF NOT EXISTS features_record_date_idx
    ON pwa_tracking_map.features (record_date);

CREATE TABLE IF NOT EXISTS pwa_tracking_map.sync_state (
    source_collection text PRIMARY KEY,
    watermark         timestamptz,
    last_full_sync_at timestamptz,
    last_success_at   timestamptz,
    status            text NOT NULL DEFAULT 'pending',
    last_error        text,
    updated_at        timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS pwa_tracking_map.map_summary (
    layer         text NOT NULL,
    pwa_code      text NOT NULL,
    feature_count bigint NOT NULL,
    updated_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (layer, pwa_code)
);

-- CSS colours come from the authoritative DMA table. Only simple CSS colour
-- literals are allowed through to the browser; all other values get a stable
-- application fallback.
CREATE OR REPLACE FUNCTION pwa_tracking_map.css_color_or_default(
    candidate text,
    fallback text
) RETURNS text
LANGUAGE sql
IMMUTABLE
AS $$
    SELECT CASE
        WHEN candidate ~* '^#[0-9a-f]{3}([0-9a-f]{3})?([0-9a-f]{2})?$'
          OR candidate ~* '^(rgb|rgba|hsl|hsla)\([0-9.,% ]+\)$'
          OR candidate ~* '^(black|white|red|green|blue|yellow|orange|purple|gray|grey|cyan|magenta|transparent)$'
        THEN candidate
        ELSE fallback
    END;
$$;

-- Run after each successful collection cycle. pwa_dma.dma_boundary is managed
-- outside this mirror and is intentionally never changed by this migration.
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
      FROM pwa_dma.dma_boundary AS dma
     WHERE feature.layer = 'dma_boundary'
       AND feature.pwa_code = dma.pwa_code::text
       AND COALESCE(feature.properties ->> 'dma_id', feature.properties ->> 'DMA_ID', feature.properties ->> 'dmaId') = dma.dma_id::text;
$$;

CREATE OR REPLACE FUNCTION pwa_tracking_map.refresh_map_summary()
RETURNS void
LANGUAGE plpgsql
AS $$
BEGIN
    INSERT INTO pwa_tracking_map.map_summary (layer, pwa_code, feature_count, updated_at)
    SELECT layer, pwa_code, count(*), now()
      FROM pwa_tracking_map.features
     GROUP BY layer, pwa_code
    ON CONFLICT (layer, pwa_code) DO UPDATE
       SET feature_count = EXCLUDED.feature_count,
           updated_at = EXCLUDED.updated_at;

    DELETE FROM pwa_tracking_map.map_summary AS summary
     WHERE NOT EXISTS (
        SELECT 1 FROM pwa_tracking_map.features AS feature
         WHERE feature.layer = summary.layer AND feature.pwa_code = summary.pwa_code
     );
END;
$$;

-- Martin function source. `query.pwa_codes` is supplied only by the server-side
-- tile proxy; Martin itself remains bound to loopback. Keep this as a compact,
-- explicit allow-list instead of serialising the full feature properties blob.
CREATE OR REPLACE FUNCTION pwa_tracking_map.pwa_gis_map_tile(
    z integer,
    x integer,
    y integer,
    query json DEFAULT '{}'::json
) RETURNS bytea
LANGUAGE sql
STABLE
PARALLEL SAFE
AS $$
    WITH bounds AS (
        SELECT ST_TileEnvelope(z, x, y) AS geom_3857,
               ST_Transform(ST_TileEnvelope(z, x, y), 4326) AS geom_4326
    ), requested_codes AS (
        SELECT trim(value) AS pwa_code
          FROM unnest(string_to_array(COALESCE(NULLIF(query ->> 'pwa_codes', ''), '*'), ',')) AS value
    ), tile_features AS (
        SELECT
            feature.id,
            feature.source_id AS "_fid",
            feature.pwa_code AS "_pwaCode",
            feature.layer AS "_layerName",
            feature.properties ->> 'sizeId' AS "sizeId",
            feature.properties ->> 'diameter' AS "diameter",
            feature.properties ->> 'functionId' AS "functionId",
            feature.properties ->> 'custStat' AS "custStat",
            feature.properties ->> 'meterNo' AS "meterNo",
            COALESCE(feature.properties ->> 'dma_id', feature.properties ->> 'DMA_ID', feature.properties ->> 'dmaId') AS "dma_id",
            COALESCE(
                feature.properties ->> 'name',
                feature.properties ->> 'pwaName',
                feature.properties ->> 'meterNo',
                feature.properties ->> 'pipeNo',
                feature.properties ->> 'bldgName',
                feature.source_id
            ) AS "label",
            feature.properties ->> 'sld_color_fill' AS "sld_color_fill",
            feature.properties ->> 'sld_color_stroke' AS "sld_color_stroke",
            ST_AsMVTGeom(ST_Transform(feature.geom, 3857), bounds.geom_3857, 4096, 64, true) AS geom
          FROM pwa_tracking_map.features AS feature
          CROSS JOIN bounds
         WHERE feature.geom && bounds.geom_4326
           AND ST_Intersects(feature.geom, bounds.geom_4326)
           AND (NULLIF(query ->> 'layer_name', '') IS NULL OR feature.layer = query ->> 'layer_name')
           AND (COALESCE(NULLIF(query ->> 'pwa_codes', ''), '*') = '*' OR feature.pwa_code IN (SELECT pwa_code FROM requested_codes))
           AND (NULLIF(query ->> 'start_date', '') IS NULL OR feature.record_date >= (query ->> 'start_date')::date)
           AND (NULLIF(query ->> 'end_date', '') IS NULL OR feature.record_date <= (query ->> 'end_date')::date)
    )
    SELECT COALESCE(ST_AsMVT(tile_features, 'pwa_gis', 4096, 'geom', 'id'), ''::bytea)
      FROM tile_features;
$$;

COMMENT ON FUNCTION pwa_tracking_map.pwa_gis_map_tile(integer, integer, integer, json) IS
'{"name":"PWA GIS map tiles","minzoom":5,"maxzoom":22,"description":"Authenticated PWA GIS vector tiles"}';
