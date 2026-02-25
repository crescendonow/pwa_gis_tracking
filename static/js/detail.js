/**
 * PWA GIS Online Tracking - Detail Page
 * Branch-level detail with layer counts, charts, MapLibre map (all layers + pipe colors), and export.
 */

var detailPieInstance = null;
var currentBranchData = null;
var allLayers = [];

// MapLibre map instance
var detailMap = null;
var mapPopup = null;
var mapLoadedLayers = []; // track active map layer IDs

// ==========================================
// Layer colors and icon paths
// ==========================================
var LAYER_MAP_CONFIG = {
    pipe:           { color: '#E67E22', icon: null,                                          type: 'line' },
    valve:          { color: '#9B59B6', icon: '/pwa_gis_tracking/static/icons/Valve.svg',     type: 'point' },
    firehydrant:    { color: '#E74C3C', icon: '/pwa_gis_tracking/static/icons/FireHydrant.svg', type: 'point' },
    meter:          { color: '#3498DB', icon: '/pwa_gis_tracking/static/icons/Meter.svg',     type: 'point' },
    bldg:           { color: '#2ECC71', icon: '/pwa_gis_tracking/static/icons/BLDG.svg',      type: 'polygon' },
    leakpoint:      { color: '#F39C12', icon: '/pwa_gis_tracking/static/icons/Leakpoint.svg', type: 'point' },
    pwa_waterworks: { color: '#1ABC9C', icon: '/pwa_gis_tracking/static/icons/PWASmall.svg',  type: 'point' },
    struct:         { color: '#34495E', icon: null,                                          type: 'polygon' },
    pipe_serv:      { color: '#D35400', icon: null,                                          type: 'line' }
};

// Pipe color by typeId (diameter in มม.) — from คำอธิบายสัญลักษณ์ท่อประปา legend
var PIPE_TYPE_COLORS = {
    '16': '#FFB6C1', '20': '#FFB6C1', '25': '#FFB6C1', '32': '#FFB6C1', '40': '#FFB6C1',
    '50': '#FF1493', '63': '#FF1493', '75': '#FF1493', '80': '#FF1493', '90': '#FF1493',
    '100': '#FFFF00', '110': '#FFFF00', '125': '#FFFF00', '140': '#FFFF00',
    '150': '#00C853', '160': '#00C853', '180': '#00C853',
    '200': '#0000FF', '225': '#0000FF',
    '250': '#FF0000', '280': '#FF0000',
    '300': '#CC0000', '315': '#CC0000',
    '350': '#9B59B6', '355': '#9B59B6',
    '400': '#00FFFF',
    '450': '#808080',
    '500': '#FF00FF', '560': '#FF00FF',
    '600': '#FFD700', '630': '#FFD700',
    '700': '#008080', '710': '#008080',
    '800': '#000080',
    '900': '#800080',
    '1000': '#00FF00',
    '1100': '#FF6347', '1200': '#FF6347', '1500': '#FF6347', '2000': '#FF6347'
};

// Color palette for charts
var LAYER_COLORS = [
    '#3498DB', '#E74C3C', '#2ECC71', '#F39C12', '#9B59B6',
    '#1ABC9C', '#E67E22', '#34495E', '#D35400'
];

// ==========================================
// Initialization
// ==========================================
document.addEventListener('DOMContentLoaded', async function() {
    ThaiDatePicker.init('#detailStartDate');
    ThaiDatePicker.init('#detailEndDate');
    await loadDetailZones();
    await loadDetailLayers();
});

// ==========================================
// API Helper
// ==========================================
async function apiGet(url) {
    var res = await fetch(url);
    if (!res.ok) throw new Error('API error: ' + res.status);
    return res.json();
}

// ==========================================
// Load Filter Options
// ==========================================
async function loadDetailZones() {
    try {
        var data = await apiGet('/pwa_gis_tracking/api/zones');
        var select = document.getElementById('detailZone');
        if (data.data) {
            data.data.forEach(function(z) {
                var opt = document.createElement('option');
                opt.value = z.zone;
                opt.textContent = 'เขต ' + z.zone + ' (' + z.branch_count + ' สาขา)';
                select.appendChild(opt);
            });
        }
    } catch (e) { console.error('Load zones error:', e); }
}

async function loadDetailLayers() {
    try {
        var data = await apiGet('/pwa_gis_tracking/api/layers');
        if (data.data) {
            allLayers = data.data;
            var select = document.getElementById('detailLayer');
            var exportSelect = document.getElementById('exportLayer');
            data.data.forEach(function(l) {
                var opt1 = document.createElement('option');
                opt1.value = l.name;
                opt1.textContent = l.display_name + ' (' + l.name + ')';
                select.appendChild(opt1);
                exportSelect.appendChild(opt1.cloneNode(true));
            });
        }
    } catch (e) { console.error('Load layers error:', e); }
}

async function onDetailZoneChange() {
    var zone = document.getElementById('detailZone').value;
    var branchSelect = document.getElementById('detailBranch');
    branchSelect.innerHTML = '<option value="">เลือกสาขา</option>';
    if (!zone) return;

    try {
        var data = await apiGet('/pwa_gis_tracking/api/offices?zone=' + zone);
        if (data.data) {
            data.data.forEach(function(o) {
                var opt = document.createElement('option');
                opt.value = o.pwa_code;
                opt.textContent = o.pwa_code + ' - ' + o.name;
                branchSelect.appendChild(opt);
            });
        }
    } catch (e) { console.error('Load branches error:', e); }
}

// ==========================================
// Branch Detail Loading
// ==========================================
async function loadBranchDetail() {
    var pwaCode = document.getElementById('detailBranch').value;
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;
    var selectedLayer = document.getElementById('detailLayer').value;

    if (!pwaCode) {
        showToast('กรุณาเลือกสาขา', 'error');
        return;
    }

    showLoading('กำลังนับจำนวนข้อมูล...');

    try {
        var url = '/pwa_gis_tracking/api/counts?pwaCode=' + pwaCode;
        if (startDate) url += '&startDate=' + startDate;
        if (endDate) url += '&endDate=' + endDate;

        var data = await apiGet(url);

        if (data.status === 'success') {
            currentBranchData = data;
            document.getElementById('resultsSection').style.display = 'block';
            document.getElementById('emptyState').style.display = 'none';
            renderDetailStats(data);
            renderDetailChart(data);
            renderDetailTable(data);

            // Load map: specific layer OR all layers
            if (selectedLayer) {
                await loadMapFeatures(pwaCode, selectedLayer, startDate, endDate);
            } else {
                // Load ALL layers that have data
                await loadAllLayersOnMap(pwaCode, data.layers || {}, startDate, endDate);
            }
        }
    } catch (e) {
        console.error('Load detail error:', e);
        showToast('ไม่สามารถโหลดข้อมูลได้: ' + e.message, 'error');
    } finally {
        hideLoading();
    }
}

// ==========================================
// MapLibre Map
// ==========================================

function ensureMap() {
    if (detailMap) return detailMap;
    if (!window.maplibregl) return null;

    detailMap = new maplibregl.Map({
        container: 'detailMap',
        style: {
            version: 8,
            sources: {
                osm: {
                    type: 'raster',
                    tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'],
                    tileSize: 256,
                    attribution: '© OpenStreetMap'
                }
            },
            layers: [{
                id: 'osm-tiles',
                type: 'raster',
                source: 'osm',
                minzoom: 0,
                maxzoom: 19
            }]
        },
        center: [100.5, 13.7],
        zoom: 6
    });

    detailMap.addControl(new maplibregl.NavigationControl(), 'top-right');
    detailMap.addControl(new maplibregl.ScaleControl({ maxWidth: 200 }), 'bottom-left');

    return detailMap;
}

/** Remove all feature layers and sources from the map. */
function clearMapFeatures() {
    if (!detailMap) return;
    mapLoadedLayers.forEach(function(id) {
        if (detailMap.getLayer(id)) detailMap.removeLayer(id);
    });
    mapLoadedLayers.forEach(function(id) {
        // Source ID = layer ID minus suffix
        var srcId = id.replace(/-point$|-line$|-fill$|-outline$/, '');
        if (detailMap.getSource(srcId)) {
            try { detailMap.removeSource(srcId); } catch(e) {}
        }
    });
    mapLoadedLayers = [];
    if (mapPopup) { mapPopup.remove(); mapPopup = null; }
}

/** Load ALL layers with data onto the map. */
async function loadAllLayersOnMap(pwaCode, layerCounts, startDate, endDate) {
    var container = document.getElementById('detailMapContainer');
    container.style.display = 'block';

    var map = ensureMap();
    if (!map) return;
    if (!map.loaded()) {
        await new Promise(function(resolve) { map.on('load', resolve); });
    }

    clearMapFeatures();

    document.getElementById('mapTitle').textContent = 'แผนที่: ทุกชั้นข้อมูล';

    var bounds = new maplibregl.LngLatBounds();
    var totalFeatures = 0;
    var legendHtml = '';

    // Get layers that have features, sorted by count (load smaller layers on top)
    var layersWithData = Object.entries(layerCounts)
        .filter(function(e) { return e[1] > 0; })
        .sort(function(a, b) { return b[1] - a[1]; }); // large first (drawn below)

    for (var i = 0; i < layersWithData.length; i++) {
        var layerName = layersWithData[i][0];
        var layerCount = layersWithData[i][1];
        showLoading('กำลังโหลด ' + getLayerDisplayName(layerName) + ' (' + (i+1) + '/' + layersWithData.length + ')...');

        try {
            var url = '/pwa_gis_tracking/api/features/map?pwaCode=' + pwaCode + '&collection=' + layerName;
            if (startDate) url += '&startDate=' + startDate;
            if (endDate) url += '&endDate=' + endDate;

            var res = await fetch(url);
            if (!res.ok) continue;
            var geojson = await res.json();
            if (!geojson.features || !geojson.features.length) continue;

            var sourceId = 'src-' + layerName;
            map.addSource(sourceId, { type: 'geojson', data: geojson });

            addLayerToMap(map, layerName, sourceId, pwaCode);

            // Extend bounds
            geojson.features.forEach(function(f) {
                if (f.geometry && f.geometry.coordinates) {
                    extendBounds(bounds, f.geometry.type, f.geometry.coordinates);
                }
            });
            totalFeatures += geojson.features.length;

            // Build legend
            var cfg = LAYER_MAP_CONFIG[layerName] || { color: '#888', type: 'point' };
            var displayName = getLayerDisplayName(layerName);
            if (cfg.type === 'line') {
                legendHtml += '<span style="display:inline-flex;align-items:center;gap:4px;margin-right:12px;font-size:11px;color:#94A3B8;">' +
                    '<span style="width:18px;height:3px;background:' + cfg.color + ';border-radius:2px;display:inline-block;"></span>' +
                    displayName + ' (' + formatNumber(geojson.features.length) + ')</span>';
            } else {
                legendHtml += '<span style="display:inline-flex;align-items:center;gap:4px;margin-right:12px;font-size:11px;color:#94A3B8;">' +
                    '<span style="width:10px;height:10px;background:' + cfg.color + ';border-radius:50%;border:1.5px solid rgba(255,255,255,0.5);display:inline-block;"></span>' +
                    displayName + ' (' + formatNumber(geojson.features.length) + ')</span>';
            }
        } catch (e) {
            console.error('Load layer ' + layerName + ' error:', e);
        }
    }

    document.getElementById('mapFeatureCount').textContent = formatNumber(totalFeatures) + ' features';
    document.getElementById('mapLegend').innerHTML = legendHtml;

    if (totalFeatures > 0) {
        map.fitBounds(bounds, { padding: 50, maxZoom: 16, duration: 800 });
    }

    // Store query context for property click
    map._pwaQuery = { pwaCode: pwaCode, collection: null };
}

/** Load a single layer onto the map. */
async function loadMapFeatures(pwaCode, layerName, startDate, endDate) {
    var container = document.getElementById('detailMapContainer');
    container.style.display = 'block';

    var map = ensureMap();
    if (!map) return;
    if (!map.loaded()) {
        await new Promise(function(resolve) { map.on('load', resolve); });
    }

    clearMapFeatures();

    var displayName = getLayerDisplayName(layerName);
    document.getElementById('mapTitle').textContent = 'แผนที่: ' + displayName;

    var url = '/pwa_gis_tracking/api/features/map?pwaCode=' + pwaCode + '&collection=' + layerName;
    if (startDate) url += '&startDate=' + startDate;
    if (endDate) url += '&endDate=' + endDate;

    try {
        var res = await fetch(url);
        if (!res.ok) throw new Error('HTTP ' + res.status);
        var geojson = await res.json();

        var count = geojson.features ? geojson.features.length : 0;
        document.getElementById('mapFeatureCount').textContent = formatNumber(count) + ' features';

        if (count === 0) {
            showToast('ไม่พบข้อมูลสำหรับชั้นข้อมูลนี้', 'info');
            return;
        }

        var sourceId = 'src-' + layerName;
        map.addSource(sourceId, { type: 'geojson', data: geojson });

        addLayerToMap(map, layerName, sourceId, pwaCode);

        // Fit bounds
        var bounds = new maplibregl.LngLatBounds();
        geojson.features.forEach(function(f) {
            if (f.geometry && f.geometry.coordinates) {
                extendBounds(bounds, f.geometry.type, f.geometry.coordinates);
            }
        });
        map.fitBounds(bounds, { padding: 50, maxZoom: 16, duration: 800 });

        // Legend for single layer
        var cfg = LAYER_MAP_CONFIG[layerName] || { color: '#888', type: 'point' };
        if (layerName === 'pipe') {
            document.getElementById('mapLegend').innerHTML = '<span style="font-size:11px;color:#94A3B8;">สีท่อแสดงตามขนาดเส้นผ่าศูนย์กลาง (typeId) — คลิกที่ท่อเพื่อดูรายละเอียด</span>';
        } else {
            document.getElementById('mapLegend').innerHTML = '';
        }

        map._pwaQuery = { pwaCode: pwaCode, collection: layerName };

    } catch (e) {
        console.error('Load map features error:', e);
        showToast('ไม่สามารถโหลดข้อมูลแผนที่ได้', 'error');
    }
}

/** Add a layer with appropriate styling to the map. */
function addLayerToMap(map, layerName, sourceId, pwaCode) {
    var cfg = LAYER_MAP_CONFIG[layerName] || { color: '#888', type: 'point' };

    if (layerName === 'pipe') {
        // Pipe: data-driven color by typeId
        var pipeLayerId = layerName + '-line';
        map.addLayer({
            id: pipeLayerId,
            type: 'line',
            source: sourceId,
            paint: {
                'line-color': buildPipeColorExpression(),
                'line-width': 3,
                'line-opacity': 0.85
            }
        });
        mapLoadedLayers.push(pipeLayerId);
        bindClickHandler(map, pipeLayerId, pwaCode, layerName);

    } else if (cfg.type === 'line') {
        var lineId = layerName + '-line';
        map.addLayer({
            id: lineId,
            type: 'line',
            source: sourceId,
            paint: {
                'line-color': cfg.color,
                'line-width': 2.5,
                'line-opacity': 0.8
            }
        });
        mapLoadedLayers.push(lineId);
        bindClickHandler(map, lineId, pwaCode, layerName);

    } else if (cfg.type === 'polygon') {
        var fillId = layerName + '-fill';
        var outlineId = layerName + '-outline';
        map.addLayer({
            id: fillId,
            type: 'fill',
            source: sourceId,
            filter: ['==', '$type', 'Polygon'],
            paint: { 'fill-color': cfg.color, 'fill-opacity': 0.2 }
        });
        map.addLayer({
            id: outlineId,
            type: 'line',
            source: sourceId,
            filter: ['==', '$type', 'Polygon'],
            paint: { 'line-color': cfg.color, 'line-width': 1.5 }
        });
        mapLoadedLayers.push(fillId, outlineId);
        bindClickHandler(map, fillId, pwaCode, layerName);

    } else {
        // Point type
        var pointId = layerName + '-point';
        // Try to load icon; fallback to circle
        if (cfg.icon) {
            loadIconAndAddLayer(map, layerName, sourceId, cfg, pwaCode);
        } else {
            map.addLayer({
                id: pointId,
                type: 'circle',
                source: sourceId,
                paint: {
                    'circle-radius': 5,
                    'circle-color': cfg.color,
                    'circle-stroke-color': '#fff',
                    'circle-stroke-width': 1.5,
                    'circle-opacity': 0.85
                }
            });
            mapLoadedLayers.push(pointId);
            bindClickHandler(map, pointId, pwaCode, layerName);
        }
    }
}

/** Load SVG icon and add as symbol layer. Falls back to circle on error. */
function loadIconAndAddLayer(map, layerName, sourceId, cfg, pwaCode) {
    var iconName = 'icon-' + layerName;
    var pointId = layerName + '-point';

    // Create an Image to load the SVG
    var img = new Image(24, 24);
    img.crossOrigin = 'anonymous';
    img.onload = function() {
        if (!map.hasImage(iconName)) {
            map.addImage(iconName, img);
        }
        map.addLayer({
            id: pointId,
            type: 'symbol',
            source: sourceId,
            layout: {
                'icon-image': iconName,
                'icon-size': 0.8,
                'icon-allow-overlap': true,
                'icon-ignore-placement': true
            }
        });
        mapLoadedLayers.push(pointId);
        bindClickHandler(map, pointId, pwaCode, layerName);
    };
    img.onerror = function() {
        // Fallback to circle
        map.addLayer({
            id: pointId,
            type: 'circle',
            source: sourceId,
            paint: {
                'circle-radius': 5,
                'circle-color': cfg.color,
                'circle-stroke-color': '#fff',
                'circle-stroke-width': 1.5,
                'circle-opacity': 0.85
            }
        });
        mapLoadedLayers.push(pointId);
        bindClickHandler(map, pointId, pwaCode, layerName);
    };
    img.src = cfg.icon;
}

/** Bind click handler + cursor for a map layer. */
function bindClickHandler(map, layerId, pwaCode, layerName) {
    map.on('click', layerId, function(e) {
        onMapFeatureClick(e, pwaCode, layerName);
    });
    map.on('mouseenter', layerId, function() {
        map.getCanvas().style.cursor = 'pointer';
    });
    map.on('mouseleave', layerId, function() {
        map.getCanvas().style.cursor = '';
    });
}

/** Build MapLibre data-driven color expression for pipe by typeId. */
function buildPipeColorExpression() {
    var matchExpr = ['match', ['to-string', ['get', 'typeId']]];
    var entries = Object.entries(PIPE_TYPE_COLORS);
    for (var i = 0; i < entries.length; i++) {
        matchExpr.push(entries[i][0], entries[i][1]);
    }
    matchExpr.push('#E67E22'); // fallback color
    return matchExpr;
}

/** Handle click on a map feature — show popup and lazy-load properties. */
function onMapFeatureClick(e, pwaCode, layerName) {
    if (!e.features || !e.features.length) return;

    var feature = e.features[0];
    var fid = feature.properties._fid;
    var lngLat = e.lngLat;

    // Determine the collection from the click context or from the stored query
    var collection = layerName || (detailMap._pwaQuery ? detailMap._pwaQuery.collection : null);
    var code = pwaCode || (detailMap._pwaQuery ? detailMap._pwaQuery.pwaCode : null);
    if (!collection || !code || !fid) return;

    if (mapPopup) mapPopup.remove();

    mapPopup = new maplibregl.Popup({ maxWidth: '380px', closeButton: true })
        .setLngLat(lngLat)
        .setHTML(
            '<div class="popup-header">' + getLayerDisplayName(collection) + '</div>' +
            '<div class="popup-loading">' +
            '<div class="spinner-sm"></div>' +
            '<div>กำลังโหลด properties...</div>' +
            '</div>'
        )
        .addTo(detailMap);

    var url = '/pwa_gis_tracking/api/features/properties?pwaCode=' + code +
        '&collection=' + collection +
        '&featureId=' + fid;

    fetch(url)
        .then(function(res) { return res.json(); })
        .then(function(data) {
            if (!mapPopup) return;
            if (data.status !== 'success') {
                mapPopup.setHTML(
                    '<div class="popup-header">Error</div>' +
                    '<div class="popup-body" style="color:#EF4444;">' + (data.error || 'ไม่พบข้อมูล') + '</div>'
                );
                return;
            }

            var props = data.properties || {};
            var rows = Object.entries(props)
                .filter(function(e) { return e[1] !== null && e[1] !== ''; })
                .map(function(e) {
                    var val = e[1];
                    if (typeof val === 'string' && /^\d{4}-\d{2}-\d{2}T/.test(val)) {
                        val = val.substring(0, 10);
                    }
                    return '<tr><td>' + escapeHtml(e[0]) + '</td><td>' + escapeHtml(String(val)) + '</td></tr>';
                })
                .join('');

            var count = Object.keys(props).length;
            mapPopup.setHTML(
                '<div class="popup-header">' + getLayerDisplayName(collection) + ' — ' + count + ' properties</div>' +
                '<div class="popup-body"><table>' + rows + '</table></div>'
            );
        })
        .catch(function() {
            if (mapPopup) {
                mapPopup.setHTML(
                    '<div class="popup-header">Error</div>' +
                    '<div class="popup-body" style="color:#EF4444;">โหลดข้อมูลล้มเหลว</div>'
                );
            }
        });
}

// ==========================================
// Geometry Helpers
// ==========================================

function extendBounds(bounds, type, coords) {
    switch (type) {
        case 'Point':
            if (coords.length >= 2) bounds.extend(coords);
            break;
        case 'MultiPoint':
        case 'LineString':
            coords.forEach(function(c) { if (c.length >= 2) bounds.extend(c); });
            break;
        case 'MultiLineString':
        case 'Polygon':
            coords.forEach(function(ring) {
                ring.forEach(function(c) { if (c.length >= 2) bounds.extend(c); });
            });
            break;
        case 'MultiPolygon':
            coords.forEach(function(poly) {
                poly.forEach(function(ring) {
                    ring.forEach(function(c) { if (c.length >= 2) bounds.extend(c); });
                });
            });
            break;
    }
}

// ==========================================
// Render Functions
// ==========================================

function renderDetailStats(data) {
    var container = document.getElementById('detailStats');
    var layers = data.layers || {};
    var total = data.total || 0;

    var topLayers = Object.entries(layers)
        .sort(function(a, b) { return b[1] - a[1]; })
        .slice(0, 4);

    var stats = [
        { label: 'ข้อมูลทั้งหมด', value: formatNumber(total), color: 'gold' }
    ];

    topLayers.forEach(function(l, i) {
        var ln = allLayers.find(function(al) { return al.name === l[0]; });
        stats.push({
            label: ln ? ln.display_name : l[0],
            value: formatNumber(l[1]),
            color: ['blue', 'green', 'cyan', 'blue'][i % 4]
        });
    });

    container.innerHTML = stats.map(function(s, i) {
        return '<div class="stat-card ' + s.color + ' fade-in" style="animation-delay:' + (i * 0.05) + 's">' +
            '<div class="stat-label">' + s.label + '</div>' +
            '<div class="stat-value">' + s.value + '</div></div>';
    }).join('');
}

function renderDetailChart(data) {
    var ctx = document.getElementById('detailPieChart').getContext('2d');
    var layers = data.layers || {};

    var entries = Object.entries(layers)
        .filter(function(e) { return e[1] > 0; })
        .sort(function(a, b) { return b[1] - a[1]; });

    var labels = entries.map(function(e) {
        var ln = allLayers.find(function(l) { return l.name === e[0]; });
        return ln ? ln.display_name : e[0];
    });
    var values = entries.map(function(e) { return e[1]; });

    if (detailPieInstance) detailPieInstance.destroy();

    detailPieInstance = new Chart(ctx, {
        type: 'doughnut',
        data: {
            labels: labels,
            datasets: [{
                data: values,
                backgroundColor: LAYER_COLORS.slice(0, labels.length),
                borderColor: 'rgba(10, 15, 26, 0.8)',
                borderWidth: 2,
                hoverOffset: 8
            }]
        },
        options: {
            responsive: true, maintainAspectRatio: true, cutout: '55%',
            plugins: {
                legend: {
                    position: 'bottom',
                    labels: { color: '#94A3B8', font: { family: 'IBM Plex Sans Thai', size: 11 }, padding: 12, usePointStyle: true, pointStyleWidth: 8 }
                },
                tooltip: {
                    callbacks: {
                        label: function(ctx) {
                            var total = ctx.dataset.data.reduce(function(a, b) { return a + b; }, 0);
                            var pct = total > 0 ? ((ctx.parsed / total) * 100).toFixed(1) : 0;
                            return ' ' + ctx.label + ': ' + formatNumber(ctx.parsed) + ' (' + pct + '%)';
                        }
                    }
                }
            }
        }
    });
}

function renderDetailTable(data) {
    var tbody = document.getElementById('layerCountBody');
    var layers = data.layers || {};
    var total = data.total || 0;

    document.getElementById('layerTotal').textContent = formatNumber(total);

    var entries = Object.entries(layers).sort(function(a, b) { return b[1] - a[1]; });

    tbody.innerHTML = entries.map(function(e) {
        var ln = allLayers.find(function(l) { return l.name === e[0]; });
        var displayName = ln ? ln.display_name : e[0];
        var pct = total > 0 ? ((e[1] / total) * 100).toFixed(1) : '0.0';
        var cls = e[1] === 0 ? 'zero' : '';

        return '<tr><td><span class="badge badge-blue">' + e[0] + '</span></td>' +
            '<td>' + displayName + '</td>' +
            '<td class="num ' + cls + '">' + formatNumber(e[1]) + '</td>' +
            '<td class="num">' + pct + '%</td></tr>';
    }).join('');
}

// ==========================================
// Export Functions
// ==========================================

function exportDetailExcel() {
    var pwaCode = document.getElementById('detailBranch').value;
    var zone = document.getElementById('detailZone').value;
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;

    if (!pwaCode && !zone) {
        showToast('กรุณาเลือกเขตหรือสาขา', 'error');
        return;
    }

    var url = '/pwa_gis_tracking/api/export/excel?';
    if (zone) url += 'zone=' + zone + '&';
    if (startDate) url += 'startDate=' + startDate + '&';
    if (endDate) url += 'endDate=' + endDate + '&';

    showToast('กำลังสร้างไฟล์ Excel...', 'info');
    window.location.href = url;
}

function exportGeoData() {
    var pwaCode = document.getElementById('detailBranch').value;
    var layer = document.getElementById('exportLayer').value;
    var format = document.getElementById('exportFormat').value;
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;

    if (!pwaCode) { showToast('กรุณาเลือกสาขา', 'error'); return; }
    if (!layer) { showToast('กรุณาเลือกชั้นข้อมูล', 'error'); return; }

    var url = '/pwa_gis_tracking/api/export/geodata?pwaCode=' + pwaCode +
        '&collection=' + layer + '&format=' + format;
    if (startDate) url += '&startDate=' + startDate;
    if (endDate) url += '&endDate=' + endDate;

    showToast('กำลัง export ' + getLayerDisplayName(layer) + '...', 'info');
    window.location.href = url;
}

// ==========================================
// Utilities
// ==========================================

function getLayerDisplayName(name) {
    var ln = allLayers.find(function(l) { return l.name === name; });
    return ln ? ln.display_name : name;
}

function formatNumber(n) {
    if (n === null || n === undefined) return '0';
    return Number(n).toLocaleString('th-TH');
}

function escapeHtml(str) {
    var div = document.createElement('div');
    div.appendChild(document.createTextNode(str));
    return div.innerHTML;
}

function showLoading(text) {
    document.getElementById('loadingOverlay').style.display = 'flex';
    document.getElementById('loadingText').textContent = text || 'กำลังโหลดข้อมูล...';
}

function hideLoading() {
    document.getElementById('loadingOverlay').style.display = 'none';
}

function showToast(message, type) {
    type = type || 'info';
    var existing = document.querySelector('.toast');
    if (existing) existing.remove();

    var toast = document.createElement('div');
    toast.className = 'toast ' + type;
    var icon = type === 'success' ? '✅' : (type === 'error' ? '❌' : 'ℹ️');
    toast.innerHTML = '<span>' + icon + '</span><span class="text-sm">' + message + '</span>';
    document.body.appendChild(toast);
    setTimeout(function() { toast.remove(); }, 4000);
}