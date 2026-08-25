# Map services deployment runbook

This runbook deploys the read-only map pipeline on the same Windows server as
`pwa_gis_tracking`. MongoDB remains the source of truth.

## Topology

```text
MongoDB -> pwa-gis-map-sync (15 minutes / daily reconcile)
        -> pgweb_gis2.pwa_tracking_map
        -> pwa-gis-map-martin (127.0.0.1:5031)
        -> authenticated Go tile proxy -> MapLibre
```

Martin is never exposed directly to browsers. The nginx `_martin` location is
loopback-only and is intended only for server diagnostics.

## 1. Prepare secrets and binaries

1. Copy `deployment/map-services.env.example` to
   `deployment/map-services.env` and fill it on the server. Do not commit it.
2. Reuse the server's verified Martin v1.3.1 binary (the same release used by
   `potential-martin`) or place it in a server tools directory.
3. Build the sync worker from the repository root:

```powershell
New-Item -ItemType Directory -Path .\bin -Force | Out-Null
go build -o .\bin\pwa-gis-map-sync.exe .\cmd\map-sync
```

## 2. Apply the PostGIS migrations

Use an account that may create the `pwa_tracking_map` schema. Both migrations
are rerunnable and touch only schema `pwa_tracking_map`.

```powershell
psql "$env:DATABASE_URL" -v ON_ERROR_STOP=1 -f .\sql\migrations\20260823_create_pwa_tracking_map.sql
psql "$env:DATABASE_URL" -v ON_ERROR_STOP=1 -f .\sql\migrations\20260824_map_tile_pipeline_fixes.sql
```

Confirm that `pwa_tracking_map.pwa_gis_map_tile(integer,integer,integer,json)`
exists before starting Martin.

The second migration (see `note/21_plan_for_edit_mappage_vectortile.md`)
fixes the root causes found when only branches `5511`/`5512` were rendering:
it adds the `sync_state.cursor_id` resume checkpoint, removes region-level
rollup collections (`b{region}000_*`, a stale 3x-duplicated snapshot), adds
`pwa_tracking_map.dma_color` and rewrites `enrich_dma_colors()` to join it
instead of `pwa_dma.dma_boundary` (which lives on a different server and was
silently failing every cycle), and adds partial GiST + RBAC-filter indexes.
Building indexes on a table with ~29.5M rows locks it; run this migration
right after the one-time backfill (step 3) completes, or split out the
`CREATE INDEX` statements to run with `CONCURRENTLY` separately.

## 3. One-time backfill

Before installing `pwa-gis-map-sync` as a long-running service, run it once
in backfill mode so the initial ~29.5M-record load is not cut short by the
service's normal 14-minute cycle timeout. Stop any existing service first so
two syncs never run concurrently.

```powershell
nssm stop pwa-gis-map-sync   # skip if this is a first install
$env:MAP_SYNC_MODE = "backfill"
.\bin\pwa-gis-map-sync.exe
Remove-Item Env:\MAP_SYNC_MODE
```

Backfill mode (`-mode backfill` or `MAP_SYNC_MODE=backfill`) sets the cycle
and collection timeouts to unlimited, runs exactly one full cycle to
completion, logs `map_sync_progress` every 60 seconds
(`collections_done=.. features_upserted=.. current=..`), then exits with code
0 on success or 1 on failure — expect this to take on the order of hours the
first time. Re-run it if it exits non-zero; interrupted collections resume
from their last checkpoint (`sync_state.cursor_id`) instead of restarting.

## 4. Install the two NSSM services

Run PowerShell as Administrator. `-StartServices` is optional so the operator
can inspect all settings before first start.

```powershell
.\deployment\install_map_services.ps1 `
  -NssmPath C:\nssm-2.24\nssm-2.24\win64\nssm.exe `
  -MartinExe C:\tools\martin\martin.exe `
  -MapSyncExe .\bin\pwa-gis-map-sync.exe `
  -EnvironmentFile .\deployment\map-services.env `
  -StartServices
```

Service names are fixed:

- `pwa-gis-map-martin` — loopback `127.0.0.1:5031`
- `pwa-gis-map-sync` — immediate sync (`MAP_SYNC_MODE=service`), then every
  15 minutes; full reconciliation every 24 hours

The sync service refreshes summary metadata, mirrors DMA colours from
`MAP_DMA_SOURCE_DATABASE_URL` when set (skipped with a logged warning
otherwise), and warms nationwide z5-z7 plus z8-z10 tiles derived from the
current PWA office locations grouped by zone. Deeper zooms are on demand.
Each cycle and collection has a timeout (`MAP_SYNC_CYCLE_TIMEOUT`,
`MAP_SYNC_COLLECTION_TIMEOUT`); `EnrichDMA`/`RefreshSummary`/cache warming run
on a separate `MAP_SYNC_FINALIZE_TIMEOUT` budget so a cycle that used its
full timeout still finalizes instead of failing outright. Each cycle emits a
structured `map_sync_cycle` result into the NSSM log.

## 5. Configure the Go web service

Set these variables on the existing web service and restart it:

```text
MARTIN_URL=http://127.0.0.1:5031
MAP_DEM_TILEJSON_URL=https://demotiles.maplibre.org/terrain-tiles/tiles.json
MAP_SATELLITE_PROVIDER=esri-osm
MAP_DATABASE_URL=postgres://USER:PASSWORD@10.250.230.81:5432/pgweb_gis2
```

`MAP_DEM_TILEJSON_URL` may later be changed to the internal DTM TileJSON without
changing frontend code. Google satellite is intentionally disabled until an
official server-side Google Map Tiles session proxy and API key are configured.
After that proxy exists, set `MAP_SATELLITE_PROVIDER=google` and
`MAP_GOOGLE_TILE_PROXY_TEMPLATE` to its same-origin `{z}/{x}/{y}` URL. Never put
the Google key or session token in this browser-facing template. The current
fallback is Esri World Imagery with OSM labels.

`MAP_DATABASE_URL` points `/api/map/summary` at the mirror database. It is
optional and opened lazily: if it is unset, unreachable, or fails to ping,
the web service still starts and `LoadMapSummary` falls back to `PgDB` (the
same host the web service already uses for everything else) rather than
failing to boot.

## 6. Validate and reload nginx

```powershell
C:\nginx\nginx.exe -t
C:\nginx\nginx.exe -s reload
Invoke-WebRequest http://127.0.0.1:5031/catalog -UseBasicParsing
```

Then sign in and smoke-test:

- `/pwa_gis_tracking/map`
- all 10 zone prefixes render, not just `5511`/`5512` (previously the only
  two whose region ended up first in Mongo's natural collection order)
- a DMA tile at z5-z9 and its database-driven fill/stroke colours (not the
  same fallback `#2f80ed` on every polygon)
- an out-of-data tile/zoom/pwa_code combination returns HTTP 204, not 502
- `/api/map/summary` returns 200 with non-empty per-layer counts
- pipes at z10+, meters at z14+, terrain and OSM 3D buildings
- branch, region and all-scope users
- changing the zone or branch dropdown reloads the map immediately, without
  needing to click "ใช้ตัวกรอง" first
- sidebar assistant and cache summary status

## Rollback

Stop `pwa-gis-map-sync` and `pwa-gis-map-martin`, then roll back the web
route/nav/nginx changes. Keep `pwa_tracking_map` for diagnosis and recovery;
drop it only after explicit approval because that destroys the mirror and sync
state. MongoDB is unaffected.
