/**
 * PWA GIS Online Tracking — Detail Page v2
 * Permission-aware branch/layer selection, drag-drop, map toggle, satellite basemap
 */

/* ─── State ──────────────────────────────────── */
var userSession = {};          // from /api/session/info
var allZones = [];
var allOffices = [];           // flat list of all offices [{pwa_code, name, zone}]
var allLayers = [];            // [{name, display_name}]
var selectedBranches = [];     // pwa_codes currently selected
var selectedLayers = [];       // layer names currently selected
var currentBranchData = null;
var detailPieInstance = null;
var detailMap = null;
var mapPopup = null;
var mapLoadedLayers = [];
var mapLoadedSources = [];
var currentBasemap = 'osm';    // 'osm' or 'satellite'
var branchSortableAvailable = null;
var branchSortableSelected = null;
var exportSortable = null;

/* ─── Layer Config ───────────────────────────── */
var LAYER_MAP_CONFIG = {
    pipe:           { color: '#E67E22', icon: null,                                              type: 'line' },
    valve:          { color: '#9B59B6', icon: '/pwa_gis_tracking/static/icons/Valve.svg',         type: 'point' },
    firehydrant:    { color: '#E74C3C', icon: '/pwa_gis_tracking/static/icons/FireHydrant.svg',   type: 'point' },
    meter:          { color: '#3498DB', icon: '/pwa_gis_tracking/static/icons/Meter.svg',         type: 'point' },
    bldg:           { color: '#2ECC71', icon: '/pwa_gis_tracking/static/icons/BLDG.svg',          type: 'polygon' },
    leakpoint:      { color: '#F39C12', icon: '/pwa_gis_tracking/static/icons/Leakpoint.svg',     type: 'point' },
    pwa_waterworks: { color: '#1ABC9C', icon: '/pwa_gis_tracking/static/icons/PWASmall.svg',      type: 'point' },
    struct:         { color: '#34495E', icon: null,                                              type: 'polygon' },
    pipe_serv:      { color: '#D35400', icon: null,                                              type: 'line' }
};
// Pipe color by sizeId (diameter in มม.) — from คำอธิบายสัญลักษณ์ท่อประปา legend
var PIPE_SIZE_COLORS = {
    '16': '#FFB6C1', '20': '#FFB6C1', '25': '#FFB6C1', '32': '#FFB6C1', '40': '#FFB6C1',
    '50': '#FF1493', '63': '#FF1493', '75': '#FF1493', '80': '#FF1493', '90': '#FF1493',
    '100': '#FFFF00', '110': '#FFFF00', '125': '#FFFF00', '140': '#FFFF00',
    '150': '#00C853', '160': '#00C853', '180': '#00C853',
    '200': '#0000FF', '225': '#0000FF', '250': '#FF0000', '280': '#FF0000',
    '300': '#CC0000', '315': '#CC0000', '350': '#9B59B6', '355': '#9B59B6',
    '400': '#00FFFF', '450': '#808080', '500': '#FF00FF', '560': '#FF00FF',
    '600': '#FFD700', '630': '#FFD700', '700': '#008080', '710': '#008080',
    '800': '#000080', '900': '#800080', '1000': '#00FF00',
    '1100': '#FF6347', '1200': '#FF6347', '1500': '#FF6347', '2000': '#FF6347'
};
var LAYER_COLORS = ['#3498DB', '#E74C3C', '#2ECC71', '#F39C12', '#9B59B6', '#1ABC9C', '#E67E22', '#34495E', '#D35400'];

/* ─── Init ───────────────────────────────────── */
document.addEventListener('DOMContentLoaded', async function () {
    ThaiDatePicker.init('#detailStartDate');
    ThaiDatePicker.init('#detailEndDate');
    await loadSessionInfo();
    await Promise.all([loadAllZonesAndOffices(), loadLayerList()]);
    renderBranchSelector();
    renderLayerGrid();
    initExportSortable();
});

/* ─── API Helper ─────────────────────────────── */
async function apiGet(url) {
    var res = await fetch(url);
    if (!res.ok) throw new Error('API error: ' + res.status);
    return res.json();
}

/* ─── Session ────────────────────────────────── */
async function loadSessionInfo() {
    try {
        userSession = await apiGet('/pwa_gis_tracking/api/session/info');
        document.getElementById('navUserName').textContent = userSession.uname || '';
        var permLevel = userSession.permission_leak || 'branch';
        var permText = { all: 'สำนักงานใหญ่', reg: 'ระดับเขต', branch: 'ระดับสาขา' };
        document.getElementById('permBadge').textContent = permText[permLevel] || permLevel;
        var banner = document.getElementById('permBanner');
        banner.className = 'perm-banner perm-' + permLevel;
        var icons = { all: 'fa-building', reg: 'fa-map', branch: 'fa-store' };
        banner.innerHTML = '<i class="fa-solid ' + (icons[permLevel] || 'fa-user') + '"></i> ' +
            '<span>สิทธิ์การใช้งาน: <strong>' + (permText[permLevel] || permLevel) + '</strong></span>' +
            (permLevel === 'reg' ? ' — เขต ' + (userSession.area || '') : '') +
            (permLevel === 'branch' ? ' — สาขา ' + (userSession.pwa_code || '') : '');
    } catch (e) { console.error('Session load error:', e); }
}

/* ─── Load Data ──────────────────────────────── */
async function loadAllZonesAndOffices() {
    try {
        var [zoneData, officeData] = await Promise.all([
            apiGet('/pwa_gis_tracking/api/zones'),
            apiGet('/pwa_gis_tracking/api/offices')
        ]);
        allZones = (zoneData.data || []).sort(function (a, b) { return Number(a.zone) - Number(b.zone); });
        allOffices = (officeData.data || []).sort(function (a, b) {
            if (a.zone !== b.zone) return Number(a.zone) - Number(b.zone);
            return (a.pwa_code || '').localeCompare(b.pwa_code || '');
        });
    } catch (e) { console.error('Load zones/offices error:', e); }
}

async function loadLayerList() {
    try {
        var data = await apiGet('/pwa_gis_tracking/api/layers');
        allLayers = data.data || [];
    } catch (e) { console.error('Load layers error:', e); }
}

/* ═══════════════════════════════════════════════ */
/* BRANCH SELECTOR (Permission-aware)             */
/* ═══════════════════════════════════════════════ */
function renderBranchSelector() {
    var panel = document.getElementById('branchSelectionPanel');
    var permLevel = userSession.permission_leak || 'branch';

    if (permLevel === 'branch') {
        // Fixed single branch
        var code = userSession.pwa_code || '';
        var office = allOffices.find(function (o) { return o.pwa_code === code; });
        var name = office ? office.name : code;
        selectedBranches = [code];
        panel.innerHTML =
            '<label class="filter-label mb-1 block"><i class="fa-solid fa-store"></i> สาขา</label>' +
            '<div class="p-3 rounded-lg" style="background:var(--surface-2);border:1px solid var(--border)">' +
            '<span class="text-sm" style="color:var(--pwa-gold)">' + code + '</span> — ' +
            '<span class="text-sm" style="color:var(--text-primary)">' + escapeHtml(name) + '</span></div>';
        return;
    }

    // "all" or "reg" — dual-list drag-drop
    var availableOffices = allOffices;
    if (permLevel === 'reg') {
        var userZone = String(userSession.area || '');
        availableOffices = allOffices.filter(function (o) { return String(o.zone) === userZone; });
    }

    var html = '<label class="filter-label mb-2 block"><i class="fa-solid fa-building"></i> เลือกสาขา (Drag & Drop)</label>';

    // Zone filter (for "all" only)
    if (permLevel === 'all') {
        html += '<div class="flex flex-wrap gap-2 mb-3" id="zoneFilterBtns">';
        html += '<button class="text-xs px-3 py-1 rounded zone-filter-btn active" data-zone="" onclick="filterAvailableByZone(this)">ทั้งหมด</button>';
        allZones.forEach(function (z) {
            html += '<button class="text-xs px-3 py-1 rounded zone-filter-btn" data-zone="' + z.zone + '" onclick="filterAvailableByZone(this)">เขต ' + z.zone + '</button>';
        });
        html += '</div>';
    }

    html += '<div class="dual-list">' +
        // Available
        '<div class="dual-list-panel"><div class="dual-list-header"><span>รายการสาขา</span><span class="count" id="availCount">(' + availableOffices.length + ')</span></div>' +
        '<input class="dual-list-search" placeholder="ค้นหาสาขา..." oninput="filterAvailableList(this.value)" />' +
        '<div class="dual-list-body" id="branchAvailable"></div></div>' +
        // Arrow buttons
        '<div class="flex flex-col justify-center gap-2">' +
        '<button class="btn text-xs py-1 px-2" onclick="moveAllToSelected()" title="เพิ่มทั้งหมด" style="background:var(--surface-2);border:1px solid var(--border);color:var(--pwa-gold)"><i class="fa-solid fa-angles-right"></i></button>' +
        '<button class="btn text-xs py-1 px-2" onclick="moveAllToAvailable()" title="ลบทั้งหมด" style="background:var(--surface-2);border:1px solid var(--border);color:var(--text-muted)"><i class="fa-solid fa-angles-left"></i></button>' +
        '</div>' +
        // Selected
        '<div class="dual-list-panel"><div class="dual-list-header"><span style="color:var(--pwa-gold)">สาขาที่เลือก</span><span class="count" id="selectedCount">(0)</span></div>' +
        '<div class="dual-list-body" id="branchSelected"></div></div>' +
        '</div>';

    panel.innerHTML = html;

    // Style zone buttons
    document.querySelectorAll('.zone-filter-btn').forEach(function (btn) {
        btn.style.cssText = 'background:var(--surface-2);border:1px solid var(--border);color:var(--text-secondary);cursor:pointer;';
    });
    var activeBtn = document.querySelector('.zone-filter-btn.active');
    if (activeBtn) activeBtn.style.cssText = 'background:rgba(212,168,67,0.15);border:1px solid rgba(212,168,67,0.4);color:var(--pwa-gold);cursor:pointer;';

    // Populate available list
    populateAvailableList(availableOffices);

    // Init SortableJS
    branchSortableAvailable = new Sortable(document.getElementById('branchAvailable'), {
        group: 'branches', animation: 150, ghostClass: 'sortable-ghost',
        onEnd: updateSelectedBranches
    });
    branchSortableSelected = new Sortable(document.getElementById('branchSelected'), {
        group: 'branches', animation: 150, ghostClass: 'sortable-ghost',
        onEnd: updateSelectedBranches
    });
}

function populateAvailableList(offices) {
    var container = document.getElementById('branchAvailable');
    if (!container) return;
    container.innerHTML = '';
    offices.forEach(function (o) {
        if (selectedBranches.indexOf(o.pwa_code) >= 0) return; // skip already selected
        var div = document.createElement('div');
        div.className = 'dual-list-item';
        div.setAttribute('data-pwa', o.pwa_code);
        div.setAttribute('data-zone', o.zone || '');
        div.setAttribute('data-name', (o.name || '').toLowerCase());
        div.innerHTML = '<span class="grip"><i class="fa-solid fa-grip-vertical"></i></span>' +
            '<span class="code">' + o.pwa_code + '</span>' +
            '<span style="color:var(--text-secondary);font-size:11px">' + escapeHtml(o.name || '') + '</span>';
        div.ondblclick = function () { moveToSelected(o.pwa_code); };
        container.appendChild(div);
    });
    document.getElementById('availCount').textContent = '(' + container.children.length + ')';
}

function filterAvailableByZone(btn) {
    document.querySelectorAll('.zone-filter-btn').forEach(function (b) {
        b.style.cssText = 'background:var(--surface-2);border:1px solid var(--border);color:var(--text-secondary);cursor:pointer;';
        b.classList.remove('active');
    });
    btn.style.cssText = 'background:rgba(212,168,67,0.15);border:1px solid rgba(212,168,67,0.4);color:var(--pwa-gold);cursor:pointer;';
    btn.classList.add('active');
    var zone = btn.getAttribute('data-zone');
    var items = document.querySelectorAll('#branchAvailable .dual-list-item');
    items.forEach(function (item) {
        item.style.display = (!zone || item.getAttribute('data-zone') === zone) ? '' : 'none';
    });
}

function filterAvailableList(query) {
    var q = query.toLowerCase();
    document.querySelectorAll('#branchAvailable .dual-list-item').forEach(function (item) {
        var pwa = item.getAttribute('data-pwa') || '';
        var name = item.getAttribute('data-name') || '';
        item.style.display = (!q || pwa.indexOf(q) >= 0 || name.indexOf(q) >= 0) ? '' : 'none';
    });
}

function moveToSelected(pwaCode) {
    var item = document.querySelector('#branchAvailable [data-pwa="' + pwaCode + '"]');
    if (item) document.getElementById('branchSelected').appendChild(item);
    updateSelectedBranches();
}

function moveAllToSelected() {
    var available = document.getElementById('branchAvailable');
    var selected = document.getElementById('branchSelected');
    var items = Array.from(available.querySelectorAll('.dual-list-item'));
    items.forEach(function (item) {
        if (item.style.display !== 'none') selected.appendChild(item);
    });
    updateSelectedBranches();
}

function moveAllToAvailable() {
    var available = document.getElementById('branchAvailable');
    var selected = document.getElementById('branchSelected');
    Array.from(selected.children).forEach(function (item) { available.appendChild(item); });
    updateSelectedBranches();
}

function updateSelectedBranches() {
    var items = document.querySelectorAll('#branchSelected .dual-list-item');
    selectedBranches = Array.from(items).map(function (el) { return el.getAttribute('data-pwa'); });
    var countEl = document.getElementById('selectedCount');
    if (countEl) countEl.textContent = '(' + selectedBranches.length + ')';
    var availCountEl = document.getElementById('availCount');
    if (availCountEl) {
        var avail = document.querySelectorAll('#branchAvailable .dual-list-item');
        availCountEl.textContent = '(' + avail.length + ')';
    }
}

/* ═══════════════════════════════════════════════ */
/* LAYER SELECTOR                                 */
/* ═══════════════════════════════════════════════ */
function renderLayerGrid() {
    var grid = document.getElementById('layerGrid');
    grid.innerHTML = '';
    allLayers.forEach(function (l) {
        var cfg = LAYER_MAP_CONFIG[l.name] || { color: '#888' };
        var label = document.createElement('label');
        label.className = 'layer-chip selected';
        label.innerHTML = '<input type="checkbox" checked value="' + l.name + '" onchange="onLayerCheckChange(this)" />' +
            '<span class="dot" style="background:' + cfg.color + '"></span>' +
            '<span>' + escapeHtml(l.display_name) + '</span>';
        grid.appendChild(label);
    });
    selectedLayers = allLayers.map(function (l) { return l.name; });
}

function onLayerCheckChange(cb) {
    var chip = cb.closest('.layer-chip');
    if (cb.checked) {
        chip.classList.add('selected');
        if (selectedLayers.indexOf(cb.value) < 0) selectedLayers.push(cb.value);
    } else {
        chip.classList.remove('selected');
        selectedLayers = selectedLayers.filter(function (n) { return n !== cb.value; });
    }
}

function selectAllLayers() {
    document.querySelectorAll('#layerGrid input[type="checkbox"]').forEach(function (cb) {
        cb.checked = true; cb.closest('.layer-chip').classList.add('selected');
    });
    selectedLayers = allLayers.map(function (l) { return l.name; });
}

function deselectAllLayers() {
    document.querySelectorAll('#layerGrid input[type="checkbox"]').forEach(function (cb) {
        cb.checked = false; cb.closest('.layer-chip').classList.remove('selected');
    });
    selectedLayers = [];
}

/* ═══════════════════════════════════════════════ */
/* EXECUTE QUERY                                  */
/* ═══════════════════════════════════════════════ */
async function executeQuery() {
    if (selectedBranches.length === 0) { showToast('กรุณาเลือกสาขา', 'error'); return; }
    if (selectedLayers.length === 0) { showToast('กรุณาเลือกชั้นข้อมูล', 'error'); return; }

    if (selectedBranches.length === 1) {
        await loadSingleBranchDetail(selectedBranches[0]);
    } else {
        await loadMultiBranchSummary();
    }
}

async function loadSingleBranchDetail(pwaCode) {
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;
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
            populateExportLayers();
            // Load map for selected layers
            await loadMapForBranch(pwaCode, startDate, endDate);
        }
    } catch (e) { console.error('Load detail error:', e); showToast('เกิดข้อผิดพลาด', 'error'); }
    hideLoading();
}

async function loadMultiBranchSummary() {
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;
    showLoading('กำลังโหลดข้อมูล ' + selectedBranches.length + ' สาขา...');
    try {
        // Collect counts for all selected branches
        var results = [];
        for (var i = 0; i < selectedBranches.length; i++) {
            var pwa = selectedBranches[i];
            showLoading('กำลังนับ ' + pwa + ' (' + (i + 1) + '/' + selectedBranches.length + ')...');
            try {
                var url = '/pwa_gis_tracking/api/counts?pwaCode=' + pwa;
                if (startDate) url += '&startDate=' + startDate;
                if (endDate) url += '&endDate=' + endDate;
                var data = await apiGet(url);
                if (data.status === 'success') {
                    var office = allOffices.find(function (o) { return o.pwa_code === pwa; });
                    results.push({ pwa_code: pwa, name: office ? office.name : pwa, zone: office ? office.zone : '', layers: data.layers || {} });
                }
            } catch (e) { console.error('Count error for ' + pwa, e); }
        }
        // Display multi-branch summary
        document.getElementById('resultsSection').style.display = 'block';
        document.getElementById('emptyState').style.display = 'none';
        renderMultiBranchTable(results);
        populateExportLayers();

        // Load map for ALL selected branches
        await loadMapForMultiBranch(selectedBranches, startDate, endDate);
    } catch (e) { console.error('Multi-branch error:', e); }
    hideLoading();
}

/* ─── Load Map for Multiple Branches ─────────── */
async function loadMapForMultiBranch(pwaCodes, startDate, endDate) {
    var container = document.getElementById('detailMapContainer');
    container.style.display = 'block';
    var map = ensureMap();
    if (!map) return;
    if (!map.loaded()) await new Promise(function (r) { map.on('load', r); });
    clearMapFeatures();
    document.getElementById('mapTitle').textContent = 'แผนที่: ' + pwaCodes.length + ' สาขา';
    var bounds = new maplibregl.LngLatBounds();
    var totalFeatures = 0;
    var layerPanelHtml = '';
    var layerFeatureCounts = {}; // track total features per layer across all branches

    for (var li = 0; li < selectedLayers.length; li++) {
        var layerName = selectedLayers[li];
        var displayName = getLayerDisplayName(layerName);

        // Merge GeoJSON features from all branches for this layer
        var mergedFeatures = [];
        var branchPwaForClick = {}; // map featureId → pwaCode for popup

        for (var bi = 0; bi < pwaCodes.length; bi++) {
            var pwa = pwaCodes[bi];
            showLoading('โหลดแผนที่ ' + displayName + ' — ' + pwa + ' (' + (bi + 1) + '/' + pwaCodes.length + ')...');
            try {
                var url = '/pwa_gis_tracking/api/features/map?pwaCode=' + pwa + '&collection=' + layerName;
                if (startDate) url += '&startDate=' + startDate;
                if (endDate) url += '&endDate=' + endDate;
                var res = await fetch(url);
                if (!res.ok) continue;
                var geojson = await res.json();
                if (!geojson.features || !geojson.features.length) continue;

                // Tag each feature with its pwaCode for click-handler
                geojson.features.forEach(function (f) {
                    if (f.properties) f.properties._pwaCode = pwa;
                    mergedFeatures.push(f);
                });
            } catch (e) { console.error('Load map ' + layerName + '/' + pwa + ':', e); }
        }

        if (mergedFeatures.length === 0) continue;

        var mergedGeoJSON = { type: 'FeatureCollection', features: mergedFeatures };
        var sourceId = 'src-' + layerName;
        map.addSource(sourceId, {
            type: 'geojson', data: mergedGeoJSON,
            tolerance: (layerName === 'pipe' || layerName === 'pipe_serv') ? 0.5 : 0.375,
            buffer: 64
        });
        mapLoadedSources.push(sourceId);
        // Pass null pwaCode — click handler will read _pwaCode from feature properties
        addLayerToMap(map, layerName, sourceId, null);
        mergedFeatures.forEach(function (f) {
            if (f.geometry && f.geometry.coordinates) extendBounds(bounds, f.geometry.type, f.geometry.coordinates);
        });
        totalFeatures += mergedFeatures.length;

        var cfg = LAYER_MAP_CONFIG[layerName] || { color: '#888' };
        layerPanelHtml += '<label class="map-layer-toggle"><input type="checkbox" checked onchange="toggleMapLayer(this,\'' + layerName + '\')" />' +
            '<span class="ldot" style="background:' + cfg.color + '"></span>' + escapeHtml(displayName) + ' (' + mergedFeatures.length + ')</label>';
    }

    document.getElementById('mapFeatureCount').textContent = formatNumber(totalFeatures) + ' features';
    document.getElementById('mapLayerPanel').innerHTML = '<div style="font-size:11px;font-weight:600;color:var(--pwa-gold);padding:4px 6px;margin-bottom:4px">ชั้นข้อมูล</div>' + layerPanelHtml;
    document.getElementById('mapLayerPanel').style.display = layerPanelHtml ? 'block' : 'none';

    if (totalFeatures > 0) map.fitBounds(bounds, { padding: 50, maxZoom: 16, duration: 800 });
    map._pwaQuery = { pwaCode: null, collection: null, multi: true };
}

/* ─── Render Multi-Branch Table ──────────────── */
function renderMultiBranchTable(results) {
    // Use the stats area to show summary
    document.getElementById('detailStats').innerHTML =
        '<div class="stat-card" style="grid-column:span 2"><div class="stat-value" style="color:var(--pwa-gold)">' + results.length + '</div><div class="stat-label">สาขาที่เลือก</div></div>';

    // Build table
    var totalByLayer = {};
    var tbody = document.getElementById('layerCountBody');
    tbody.innerHTML = '';
    // Build a summary table showing each branch
    var html = '';
    results.forEach(function (r) {
        var total = 0;
        selectedLayers.forEach(function (l) { total += (r.layers[l] || 0); totalByLayer[l] = (totalByLayer[l] || 0) + (r.layers[l] || 0); });
        html += '<tr><td>' + r.pwa_code + '</td><td>' + escapeHtml(r.name) + '</td><td class="num">' + formatNumber(total) + '</td><td class="num">เขต ' + r.zone + '</td></tr>';
    });
    tbody.innerHTML = html;
    // Update table headers for multi-branch
    document.querySelector('#layerCountTable thead tr').innerHTML = '<th>รหัสสาขา</th><th>ชื่อสาขา</th><th class="text-right">รวม</th><th class="text-right">เขต</th>';
    var grandTotal = 0;
    for (var k in totalByLayer) grandTotal += totalByLayer[k];
    document.getElementById('layerTotal').textContent = formatNumber(grandTotal);

    // Chart: total by layer
    var labels = [], values = [], colors = [];
    selectedLayers.forEach(function (l, i) {
        if (totalByLayer[l]) {
            labels.push(getLayerDisplayName(l));
            values.push(totalByLayer[l]);
            colors.push(LAYER_COLORS[i % LAYER_COLORS.length]);
        }
    });
    renderPieChart(labels, values, colors);
}

/* ═══════════════════════════════════════════════ */
/* MAP — Layer Toggle + Satellite Basemap         */
/* ═══════════════════════════════════════════════ */
var BASEMAPS = {
    osm: { name: 'OpenStreetMap', style: { version: 8, sources: { osm: { type: 'raster', tiles: ['https://tile.openstreetmap.org/{z}/{x}/{y}.png'], tileSize: 256 } }, layers: [{ id: 'osm', type: 'raster', source: 'osm' }] } },
    satellite: { name: 'Satellite', style: { version: 8, sources: { sat: { type: 'raster', tiles: ['https://mt1.google.com/vt/lyrs=s&x={x}&y={y}&z={z}'], tileSize: 256 } }, layers: [{ id: 'sat', type: 'raster', source: 'sat' }] } }
};

function ensureMap() {
    if (detailMap) return detailMap;
    try {
        detailMap = new maplibregl.Map({
            container: 'detailMap', style: BASEMAPS[currentBasemap].style,
            center: [100.5, 13.75], zoom: 8
        });
        detailMap.addControl(new maplibregl.NavigationControl(), 'top-right');
        return detailMap;
    } catch (e) { console.error('Map init error:', e); return null; }
}

function toggleBasemap() {
    currentBasemap = currentBasemap === 'osm' ? 'satellite' : 'osm';
    var label = document.getElementById('basemapLabel');
    var thumb = document.getElementById('basemapThumb');
    if (currentBasemap === 'osm') {
        label.textContent = 'Satellite';
        thumb.querySelector('img').src = 'https://mt1.google.com/vt/lyrs=s&x=0&y=0&z=0';
    } else {
        label.textContent = 'Street Map';
        thumb.querySelector('img').src = 'https://tile.openstreetmap.org/0/0/0.png';
    }
    if (!detailMap) return;

    // Save GeoJSON data only (layer objects have circular refs)
    var savedSourceData = {};
    mapLoadedSources.forEach(function (srcId) {
        var src = detailMap.getSource(srcId);
        if (src && src._data) savedSourceData[srcId] = src._data;
    });
    var pwaCode = (detailMap._pwaQuery && detailMap._pwaQuery.pwaCode) || '';

    // Clear tracking (style change removes everything)
    mapLoadedLayers = [];
    mapLoadedSources = [];

    // Switch basemap
    detailMap.setStyle(BASEMAPS[currentBasemap].style);

    // After style loads, re-add sources + rebuild layers via addLayerToMap
    detailMap.once('styledata', function () {
        for (var srcId in savedSourceData) {
            if (!detailMap.getSource(srcId)) {
                var ln = srcId.replace(/^src-/, '');
                detailMap.addSource(srcId, {
                    type: 'geojson', data: savedSourceData[srcId],
                    tolerance: (ln === 'pipe' || ln === 'pipe_serv') ? 0.5 : 0.375, buffer: 64
                });
                mapLoadedSources.push(srcId);
            }
            addLayerToMap(detailMap, srcId.replace(/^src-/, ''), srcId, pwaCode);
        }
        detailMap._pwaQuery = { pwaCode: pwaCode, collection: null };
    });
}

function clearMapFeatures() {
    if (!detailMap) return;
    mapLoadedLayers.forEach(function (id) { if (detailMap.getLayer(id)) detailMap.removeLayer(id); });
    var removed = {};
    mapLoadedLayers.forEach(function (id) {
        var layerName = id.replace(/-point$|-line$|-fill$|-outline$|-circle$/, '');
        var srcId = 'src-' + layerName;
        if (!removed[srcId] && detailMap.getSource(srcId)) {
            try { detailMap.removeSource(srcId); } catch (e) { }
            removed[srcId] = true;
        }
    });
    mapLoadedLayers = [];
    mapLoadedSources = [];
    if (mapPopup) { mapPopup.remove(); mapPopup = null; }
}

async function loadMapForBranch(pwaCode, startDate, endDate) {
    var container = document.getElementById('detailMapContainer');
    container.style.display = 'block';
    var map = ensureMap();
    if (!map) return;
    if (!map.loaded()) await new Promise(function (r) { map.on('load', r); });
    clearMapFeatures();
    document.getElementById('mapTitle').textContent = 'แผนที่: ' + pwaCode;
    var bounds = new maplibregl.LngLatBounds();
    var totalFeatures = 0;
    var layerPanelHtml = '';

    for (var i = 0; i < selectedLayers.length; i++) {
        var layerName = selectedLayers[i];
        var displayName = getLayerDisplayName(layerName);
        showLoading('โหลดแผนที่ ' + displayName + '...');
        try {
            var url = '/pwa_gis_tracking/api/features/map?pwaCode=' + pwaCode + '&collection=' + layerName;
            if (startDate) url += '&startDate=' + startDate;
            if (endDate) url += '&endDate=' + endDate;
            var res = await fetch(url);
            if (!res.ok) continue;
            var geojson = await res.json();
            if (!geojson.features || !geojson.features.length) continue;

            var sourceId = 'src-' + layerName;
            map.addSource(sourceId, {
                type: 'geojson', data: geojson,
                tolerance: (layerName === 'pipe' || layerName === 'pipe_serv') ? 0.5 : 0.375,
                buffer: 64
            });
            mapLoadedSources.push(sourceId);
            addLayerToMap(map, layerName, sourceId, pwaCode);
            geojson.features.forEach(function (f) {
                if (f.geometry && f.geometry.coordinates) extendBounds(bounds, f.geometry.type, f.geometry.coordinates);
            });
            totalFeatures += geojson.features.length;

            var cfg = LAYER_MAP_CONFIG[layerName] || { color: '#888' };
            layerPanelHtml += '<label class="map-layer-toggle"><input type="checkbox" checked onchange="toggleMapLayer(this,\'' + layerName + '\')" />' +
                '<span class="ldot" style="background:' + cfg.color + '"></span>' + escapeHtml(displayName) + ' (' + geojson.features.length + ')</label>';
        } catch (e) { console.error('Load map layer ' + layerName + ':', e); }
    }

    document.getElementById('mapFeatureCount').textContent = formatNumber(totalFeatures) + ' features';
    document.getElementById('mapLayerPanel').innerHTML = '<div style="font-size:11px;font-weight:600;color:var(--pwa-gold);padding:4px 6px;margin-bottom:4px">ชั้นข้อมูล</div>' + layerPanelHtml;
    document.getElementById('mapLayerPanel').style.display = layerPanelHtml ? 'block' : 'none';

    if (totalFeatures > 0) map.fitBounds(bounds, { padding: 50, maxZoom: 16, duration: 800 });
    map._pwaQuery = { pwaCode: pwaCode, collection: null };
}

function toggleMapLayer(cb, layerName) {
    if (!detailMap) return;
    var visibility = cb.checked ? 'visible' : 'none';
    // Match all sub-layers: valve-point, valve-circle, bldg-fill, bldg-outline, pipe-line, etc.
    mapLoadedLayers.forEach(function (lid) {
        if ((lid === layerName || lid.indexOf(layerName + '-') === 0) && detailMap.getLayer(lid)) {
            detailMap.setLayoutProperty(lid, 'visibility', visibility);
        }
    });
}

function toggleLayerPanel() {
    var panel = document.getElementById('mapLayerPanel');
    panel.style.display = panel.style.display === 'none' ? 'block' : 'none';
}

function addLayerToMap(map, layerName, sourceId, pwaCode) {
    var cfg = LAYER_MAP_CONFIG[layerName] || { color: '#888', icon: null, type: 'point' };

    if (layerName === 'pipe') {
        // ─── Pipe: data-driven color by sizeId (diameter) ─────
        var pipeId = layerName + '-line';
        map.addLayer({
            id: pipeId,
            type: 'line',
            source: sourceId,
            paint: {
                'line-color': buildPipeColorExpression(),
                'line-width': ['interpolate', ['linear'], ['zoom'],
                    8, 1, 12, 2.5, 16, 4
                ],
                'line-opacity': 0.85
            }
        });
        mapLoadedLayers.push(pipeId);
        bindClickHandler(map, pipeId, pwaCode, layerName);

    } else if (cfg.type === 'line') {
        // ─── Line layers (pipe_serv) ─────────────────────────
        var lineId = layerName + '-line';
        map.addLayer({
            id: lineId,
            type: 'line',
            source: sourceId,
            paint: {
                'line-color': cfg.color,
                'line-width': ['interpolate', ['linear'], ['zoom'],
                    8, 1, 12, 2, 16, 3.5
                ],
                'line-opacity': 0.8
            }
        });
        mapLoadedLayers.push(lineId);
        bindClickHandler(map, lineId, pwaCode, layerName);

    } else if (cfg.type === 'polygon') {
        // ─── Polygon layers (bldg, struct) ───────────────────
        var fillId = layerName + '-fill';
        var outlineId = layerName + '-outline';
        map.addLayer({
            id: fillId,
            type: 'fill',
            source: sourceId,
            paint: { 'fill-color': cfg.color, 'fill-opacity': ['interpolate', ['linear'], ['zoom'], 8, 0.05, 13, 0.15, 16, 0.25] }
        });
        map.addLayer({
            id: outlineId,
            type: 'line',
            source: sourceId,
            paint: { 'line-color': cfg.color, 'line-width': ['interpolate', ['linear'], ['zoom'], 8, 0.5, 13, 1, 16, 2] }
        });
        mapLoadedLayers.push(fillId, outlineId);
        bindClickHandler(map, fillId, pwaCode, layerName);

    } else if (cfg.icon) {
        // ─── Point layers WITH SVG icon (valve, meter, etc.) ─
        var iconId = 'icon-' + layerName;
        var symbolId = layerName + '-point';

        // Load SVG → render as symbol layer
        loadSvgIcon(map, iconId, cfg.icon, function () {
            if (map.getLayer(symbolId)) return;
            map.addLayer({
                id: symbolId,
                type: 'symbol',
                source: sourceId,
                filter: ['==', '$type', 'Point'],
                layout: {
                    'icon-image': iconId,
                    'icon-size': ['interpolate', ['linear'], ['zoom'],
                        6, 0.15,
                        10, 0.35,
                        13, 0.55,
                        16, 0.8,
                        18, 1.0
                    ],
                    'icon-allow-overlap': ['step', ['zoom'], false, 14, true],
                    'icon-ignore-placement': false
                }
            });
            mapLoadedLayers.push(symbolId);
            bindClickHandler(map, symbolId, pwaCode, layerName);
        });

        // Fallback circle beneath icon (for click detection reliability)
        var circleId = layerName + '-circle';
        map.addLayer({
            id: circleId,
            type: 'circle',
            source: sourceId,
            filter: ['==', '$type', 'Point'],
            paint: {
                'circle-radius': ['interpolate', ['linear'], ['zoom'],
                    6, 1, 10, 2, 14, 3, 18, 5
                ],
                'circle-color': cfg.color,
                'circle-opacity': 0.3,
                'circle-stroke-width': 0
            }
        });
        mapLoadedLayers.push(circleId);

    } else {
        // ─── Point layers WITHOUT icon — plain circle ────────
        var ptId = layerName + '-point';
        map.addLayer({
            id: ptId,
            type: 'circle',
            source: sourceId,
            filter: ['==', '$type', 'Point'],
            paint: {
                'circle-radius': ['interpolate', ['linear'], ['zoom'],
                    6, 2, 10, 3, 13, 5, 16, 8, 18, 12
                ],
                'circle-color': cfg.color,
                'circle-stroke-width': 1,
                'circle-stroke-color': 'rgba(255,255,255,0.5)'
            }
        });
        mapLoadedLayers.push(ptId);
        bindClickHandler(map, ptId, pwaCode, layerName);
    }
}

/**
 * Load an SVG file as a MapLibre image for symbol layers.
 * SVG → Image → Canvas → ImageData → map.addImage()
 */
function loadSvgIcon(map, iconId, svgUrl, callback) {
    if (map.hasImage(iconId)) { callback(); return; }

    var img = new Image();
    img.crossOrigin = 'anonymous';
    img.onload = function () {
        var size = 48;
        var canvas = document.createElement('canvas');
        canvas.width = size;
        canvas.height = size;
        var ctx = canvas.getContext('2d');
        ctx.drawImage(img, 0, 0, size, size);
        var imageData = ctx.getImageData(0, 0, size, size);
        if (!map.hasImage(iconId)) {
            map.addImage(iconId, { width: size, height: size, data: imageData.data });
        }
        callback();
    };
    img.onerror = function () {
        console.warn('Icon load failed:', svgUrl, '— using circle fallback');
        callback();
    };
    img.src = svgUrl;
}

function buildPipeColorExpression() {
    // Color pipe by sizeId (diameter in mm)
    var expr = ['match', ['to-string', ['get', 'sizeId']]];
    for (var k in PIPE_SIZE_COLORS) expr.push(k, PIPE_SIZE_COLORS[k]);
    expr.push('#888888'); // default for unknown sizes
    return expr;
}

function bindClickHandler(map, layerId, pwaCode, layerName) {
    map.on('click', layerId, function (e) {
        var feat = e.features && e.features[0];
        if (!feat) return;
        var fid = feat.id || (feat.properties && feat.properties._fid) || '';
        if (!fid) return;
        // Multi-branch: read pwaCode from feature properties; fallback to passed pwaCode
        var effectivePwa = (feat.properties && feat.properties._pwaCode) || pwaCode || '';
        if (!effectivePwa) return;
        var coords = e.lngLat;
        var branchLabel = effectivePwa;
        var office = allOffices.find(function (o) { return o.pwa_code === effectivePwa; });
        if (office) branchLabel = effectivePwa + ' — ' + office.name;
        var popupHtml = '<div class="popup-header">' + escapeHtml(getLayerDisplayName(layerName)) +
            '<span style="font-weight:normal;font-size:11px;color:var(--text-muted);margin-left:8px">' + escapeHtml(branchLabel) + '</span>' +
            '</div><div class="popup-body"><div class="popup-loading"><div class="spinner-sm"></div><br>กำลังโหลด...</div></div>';
        if (mapPopup) mapPopup.remove();
        mapPopup = new maplibregl.Popup({ maxWidth: '380px' }).setLngLat(coords).setHTML(popupHtml).addTo(map);
        fetch('/pwa_gis_tracking/api/features/properties?pwaCode=' + effectivePwa + '&collection=' + layerName + '&featureId=' + fid)
            .then(function (r) { return r.json(); }).then(function (data) {
                var props = data.properties || {};
                var rows = Object.entries(props).filter(function (e) { return typeof e[1] !== 'object'; })
                    .map(function (e) { return '<tr><td>' + escapeHtml(e[0]) + '</td><td>' + escapeHtml(String(e[1] === null ? '' : e[1])) + '</td></tr>'; }).join('');
                var popup = document.querySelector('.maplibregl-popup-content');
                if (popup) popup.querySelector('.popup-body').innerHTML = '<table>' + rows + '</table>';
            }).catch(function () { var p = document.querySelector('.popup-body'); if (p) p.innerHTML = '<p style="padding:10px;color:#e74c3c">โหลดข้อมูลไม่สำเร็จ</p>'; });
    });
    map.on('mouseenter', layerId, function () { map.getCanvas().style.cursor = 'pointer'; });
    map.on('mouseleave', layerId, function () { map.getCanvas().style.cursor = ''; });
}

/* ═══════════════════════════════════════════════ */
/* RENDER — Stats, Chart, Table                   */
/* ═══════════════════════════════════════════════ */
function renderDetailStats(data) {
    var layers = data.layers || {};
    var total = 0; for (var k in layers) total += layers[k];
    var pwaCode = selectedBranches[0] || '';
    var office = allOffices.find(function (o) { return o.pwa_code === pwaCode; });
    var officeName = office ? escapeHtml(office.name) : pwaCode;
    // Auto-size: use smaller font for long branch names to keep consistent look
    var nameStyle = 'color:var(--pwa-gold)';
    if (officeName.length > 15) nameStyle += ';font-size:clamp(14px, 2vw, 20px)';
    var html = '<div class="stat-card"><div class="stat-value" style="' + nameStyle + '">' + officeName + '</div><div class="stat-label">สาขา ' + pwaCode + '</div></div>' +
        '<div class="stat-card"><div class="stat-value">' + formatNumber(total) + '</div><div class="stat-label">รวมทั้งหมด</div></div>';
    var count = 0;
    for (var k in layers) { if (layers[k] > 0) count++; }
    html += '<div class="stat-card"><div class="stat-value">' + count + '</div><div class="stat-label">ชั้นข้อมูลที่มีข้อมูล</div></div>';
    if (data.pipe_length !== undefined) html += '<div class="stat-card"><div class="stat-value">' + formatNumber(Math.round(data.pipe_length)) + '</div><div class="stat-label">ความยาวท่อ (ม.)</div></div>';
    document.getElementById('detailStats').innerHTML = html;
}

function renderDetailChart(data) {
    var layers = data.layers || {};
    var labels = [], values = [], colors = [];
    allLayers.forEach(function (l, i) {
        if (layers[l.name] > 0) {
            labels.push(l.display_name);
            values.push(layers[l.name]);
            colors.push(LAYER_COLORS[i % LAYER_COLORS.length]);
        }
    });
    renderPieChart(labels, values, colors);
}

function renderPieChart(labels, values, colors) {
    if (detailPieInstance) detailPieInstance.destroy();
    var ctx = document.getElementById('detailPieChart');
    if (!ctx) return;
    detailPieInstance = new Chart(ctx, {
        type: 'doughnut', data: { labels: labels, datasets: [{ data: values, backgroundColor: colors, borderWidth: 0 }] },
        options: { responsive: true, plugins: { legend: { position: 'bottom', labels: { color: '#94a3b8', font: { size: 11 }, padding: 8 } } } }
    });
}

function renderDetailTable(data) {
    var layers = data.layers || {};
    var total = 0; for (var k in layers) total += layers[k];
    // Reset table headers for single branch
    document.querySelector('#layerCountTable thead tr').innerHTML = '<th>ชั้นข้อมูล</th><th>ชื่อภาษาไทย</th><th class="text-right">จำนวน</th><th class="text-right">สัดส่วน %</th>';
    var tbody = document.getElementById('layerCountBody');
    tbody.innerHTML = '';
    allLayers.forEach(function (l) {
        var count = layers[l.name] || 0;
        var pct = total > 0 ? (count / total * 100).toFixed(1) : '0.0';
        var clickable = count > 0;
        var tr = document.createElement('tr');
        if (clickable) {
            tr.style.cursor = 'pointer';
            tr.title = 'คลิกเพื่อดูข้อมูล ' + l.display_name;
            tr.onclick = function () { openLayerModal(l.name, l.display_name); };
        }
        tr.innerHTML = '<td>' + l.name + (clickable ? ' <i class="fa-solid fa-magnifying-glass" style="font-size:10px;color:var(--pwa-gold)"></i>' : '') + '</td>' +
            '<td>' + escapeHtml(l.display_name) + '</td>' +
            '<td class="num">' + formatNumber(count) + '</td>' +
            '<td class="num">' + pct + '%</td>';
        tbody.appendChild(tr);
    });
    document.getElementById('layerTotal').textContent = formatNumber(total);
}

function openLayerModal(layerName, displayName) {
    if (typeof LayerModal === 'undefined') return;
    var pwaCode = selectedBranches[0] || '';
    LayerModal.open({
        pwaCode: pwaCode, collection: layerName, layerDisplayName: displayName,
        startDate: document.getElementById('detailStartDate').value,
        endDate: document.getElementById('detailEndDate').value
    });
}

/* ═══════════════════════════════════════════════ */
/* EXPORT — Drag-drop layer list                  */
/* ═══════════════════════════════════════════════ */
function initExportSortable() {
    var el = document.getElementById('exportLayerList');
    if (el) exportSortable = new Sortable(el, { animation: 150, ghostClass: 'sortable-ghost' });
}

function populateExportLayers() {
    var list = document.getElementById('exportLayerList');
    list.innerHTML = '';
    selectedLayers.forEach(function (name) {
        var cfg = LAYER_MAP_CONFIG[name] || { color: '#888' };
        var div = document.createElement('div');
        div.className = 'export-layer-item';
        div.setAttribute('data-layer', name);
        div.innerHTML = '<span class="grip"><i class="fa-solid fa-grip-vertical"></i></span>' +
            '<span class="dot" style="width:8px;height:8px;border-radius:50%;background:' + cfg.color + '"></span>' +
            '<span>' + escapeHtml(getLayerDisplayName(name)) + '</span>' +
            '<button onclick="this.parentElement.remove()" style="margin-left:auto;color:var(--text-muted);background:none;border:none;cursor:pointer;font-size:10px"><i class="fa-solid fa-xmark"></i></button>';
        list.appendChild(div);
    });
}

function addAllExportLayers() { populateExportLayers(); updateMergeModeOptions(); }

function exportGeoData() {
    if (selectedBranches.length === 0) { showToast('กรุณาเลือกสาขา', 'error'); return; }
    var format = document.getElementById('exportFormat').value;
    var exportItems = document.querySelectorAll('#exportLayerList .export-layer-item');
    var layers = Array.from(exportItems).map(function (el) { return el.getAttribute('data-layer'); });
    if (layers.length === 0) { showToast('กรุณาเลือกชั้นข้อมูลสำหรับ export', 'error'); return; }

    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;

    // Check merge mode
    var mergeEl = document.getElementById('exportMergeMode');
    var mergeMode = mergeEl ? mergeEl.value : 'split';

    if (mergeMode === 'merge_all' && (selectedBranches.length > 1 || layers.length > 1)) {
        // Merge all branches + all layers → single file
        var pwaCodes = selectedBranches.join(',');
        var layerNames = layers.join(',');
        showToast('กำลัง export (รวมทุกสาขา+ทุกชั้นข้อมูล) 1 ไฟล์...', 'info');
        var url = '/pwa_gis_tracking/api/export/geodata?pwaCode=' + pwaCodes + '&collection=' + layerNames + '&format=' + format + '&merge=all';
        if (startDate) url += '&startDate=' + startDate;
        if (endDate) url += '&endDate=' + endDate;
        var iframe = document.createElement('iframe');
        iframe.style.display = 'none';
        iframe.src = url;
        document.body.appendChild(iframe);
        setTimeout(function () { iframe.remove(); }, 60000);

    } else if (mergeMode === 'merge_branch' && selectedBranches.length > 1) {
        // Merge all branches per layer → one file per layer
        var total = layers.length;
        showToast('กำลัง export (รวมสาขาแยกชั้นข้อมูล) ' + total + ' ไฟล์...', 'info');
        var pwaCodes = selectedBranches.join(',');
        layers.forEach(function (layer) {
            var url = '/pwa_gis_tracking/api/export/geodata?pwaCode=' + pwaCodes + '&collection=' + layer + '&format=' + format + '&merge=branch';
            if (startDate) url += '&startDate=' + startDate;
            if (endDate) url += '&endDate=' + endDate;
            var iframe = document.createElement('iframe');
            iframe.style.display = 'none';
            iframe.src = url;
            document.body.appendChild(iframe);
            setTimeout(function () { iframe.remove(); }, 60000);
        });

    } else if (mergeMode === 'merge_layer' && layers.length > 1) {
        // Merge all layers per branch → one file per branch
        var total = selectedBranches.length;
        showToast('กำลัง export (แยกสาขา รวมชั้นข้อมูล) ' + total + ' ไฟล์...', 'info');
        var layerNames = layers.join(',');
        selectedBranches.forEach(function (pwa) {
            var url = '/pwa_gis_tracking/api/export/geodata?pwaCode=' + pwa + '&collection=' + layerNames + '&format=' + format + '&merge=layer';
            if (startDate) url += '&startDate=' + startDate;
            if (endDate) url += '&endDate=' + endDate;
            var iframe = document.createElement('iframe');
            iframe.style.display = 'none';
            iframe.src = url;
            document.body.appendChild(iframe);
            setTimeout(function () { iframe.remove(); }, 60000);
        });

    } else {
        // Split mode: export each branch+layer combo separately
        var total = selectedBranches.length * layers.length;
        showToast('กำลัง export ' + total + ' ไฟล์ (แยกรายสาขา×ชั้นข้อมูล)...', 'info');

        selectedBranches.forEach(function (pwa) {
            layers.forEach(function (layer) {
                var url = '/pwa_gis_tracking/api/export/geodata?pwaCode=' + pwa + '&collection=' + layer + '&format=' + format;
                if (startDate) url += '&startDate=' + startDate;
                if (endDate) url += '&endDate=' + endDate;
                // Use hidden iframe for multi-download
                var iframe = document.createElement('iframe');
                iframe.style.display = 'none';
                iframe.src = url;
                document.body.appendChild(iframe);
                setTimeout(function () { iframe.remove(); }, 30000);
            });
        });
    }
}

function updateMergeModeOptions() {
    var mergeEl = document.getElementById('exportMergeMode');
    if (!mergeEl) return;
    var hasManyBranches = selectedBranches.length > 1;
    var exportItems = document.querySelectorAll('#exportLayerList .export-layer-item');
    var hasManyLayers = exportItems.length > 1;
    // Show/hide merge selector
    var container = document.getElementById('exportMergeContainer');
    if (container) {
        container.style.display = (hasManyBranches || hasManyLayers) ? 'flex' : 'none';
    }
}

function exportDetailExcel() {
    var zone = '';
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;
    if (selectedBranches.length === 0) { showToast('กรุณาเลือกสาขา', 'error'); return; }
    var url = '/pwa_gis_tracking/api/export/excel?';
    if (zone) url += 'zone=' + zone + '&';
    if (startDate) url += 'startDate=' + startDate + '&';
    if (endDate) url += 'endDate=' + endDate + '&';
    showToast('กำลังสร้างไฟล์ Excel...', 'info');
    window.location.href = url;
}

function resetSelection() {
    selectedBranches = [];
    selectedLayers = [];
    document.getElementById('resultsSection').style.display = 'none';
    document.getElementById('emptyState').style.display = 'block';
    renderBranchSelector();
    renderLayerGrid();
}

/* ═══════════════════════════════════════════════ */
/* UTILITIES                                      */
/* ═══════════════════════════════════════════════ */
function getLayerDisplayName(name) {
    var ln = allLayers.find(function (l) { return l.name === name; });
    return ln ? ln.display_name : name;
}
function formatNumber(n) { return n == null ? '0' : Number(n).toLocaleString('th-TH'); }
function escapeHtml(str) { var d = document.createElement('div'); d.appendChild(document.createTextNode(str || '')); return d.innerHTML; }
function showLoading(text) { document.getElementById('loadingOverlay').style.display = 'flex'; document.getElementById('loadingText').textContent = text || 'กำลังโหลด...'; }
function hideLoading() { document.getElementById('loadingOverlay').style.display = 'none'; }
function showToast(msg, type) {
    type = type || 'info';
    var ex = document.querySelector('.toast'); if (ex) ex.remove();
    var t = document.createElement('div'); t.className = 'toast ' + type;
    var icon = type === 'success' ? '✅' : (type === 'error' ? '❌' : 'ℹ️');
    t.innerHTML = '<span>' + icon + '</span><span class="text-sm">' + msg + '</span>';
    document.body.appendChild(t);
    setTimeout(function () { t.remove(); }, 4000);
}

function extendBounds(bounds, geomType, coords) {
    try {
        if (geomType === 'Point') { bounds.extend(coords); }
        else if (geomType === 'LineString' || geomType === 'MultiPoint') { coords.forEach(function (c) { bounds.extend(c); }); }
        else if (geomType === 'Polygon' || geomType === 'MultiLineString') { coords.forEach(function (ring) { ring.forEach(function (c) { bounds.extend(c); }); }); }
        else if (geomType === 'MultiPolygon') { coords.forEach(function (poly) { poly.forEach(function (ring) { ring.forEach(function (c) { bounds.extend(c); }); }); }); }
    } catch (e) { }
}