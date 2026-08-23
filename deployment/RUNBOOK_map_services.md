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

## 2. Apply the PostGIS migration

Use an account that may create the `pwa_tracking_map` schema and read
`pwa_dma.dma_boundary`. The migration is rerunnable.

```powershell
psql "$env:DATABASE_URL" -v ON_ERROR_STOP=1 -f .\sql\migrations\20260823_create_pwa_tracking_map.sql
```

Confirm that `pwa_tracking_map.pwa_gis_map_tile(integer,integer,integer,json)`
exists before starting Martin.

## 3. Install the two NSSM services

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
- `pwa-gis-map-sync` — immediate sync, then every 15 minutes; full
  reconciliation every 24 hours

The sync service refreshes summary metadata and warms nationwide z5-z7 plus
z8-z10 tiles derived from the current PWA office locations grouped by zone.
Deeper zooms are on demand. Each cycle and collection has a timeout and emits a
structured `map_sync_cycle` result into the NSSM log.

## 4. Configure the Go web service

Set these variables on the existing web service and restart it:

```text
MARTIN_URL=http://127.0.0.1:5031
MAP_DEM_TILEJSON_URL=https://demotiles.maplibre.org/terrain-tiles/tiles.json
MAP_SATELLITE_PROVIDER=esri-osm
```

`MAP_DEM_TILEJSON_URL` may later be changed to the internal DTM TileJSON without
changing frontend code. Google satellite is intentionally disabled until an
official server-side Google Map Tiles session proxy and API key are configured.
After that proxy exists, set `MAP_SATELLITE_PROVIDER=google` and
`MAP_GOOGLE_TILE_PROXY_TEMPLATE` to its same-origin `{z}/{x}/{y}` URL. Never put
the Google key or session token in this browser-facing template. The current
fallback is Esri World Imagery with OSM labels.

## 5. Validate and reload nginx

```powershell
C:\nginx\nginx.exe -t
C:\nginx\nginx.exe -s reload
Invoke-WebRequest http://127.0.0.1:5031/catalog -UseBasicParsing
```

Then sign in and smoke-test:

- `/pwa_gis_tracking/map`
- a DMA tile at z5-z9 and its database-driven fill/stroke colours
- pipes at z10+, meters at z14+, terrain and OSM 3D buildings
- branch, region and all-scope users
- sidebar assistant and cache summary status

## Rollback

Stop `pwa-gis-map-sync` and `pwa-gis-map-martin`, then roll back the web
route/nav/nginx changes. Keep `pwa_tracking_map` for diagnosis and recovery;
drop it only after explicit approval because that destroys the mirror and sync
state. MongoDB is unaffected.
