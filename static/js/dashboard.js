/**
 * PWA GIS Online Tracking - Dashboard Page
 * - Uses ThaiDatePicker (thai_custom_date.js) for Buddhist Era calendar
 * - Loads branch markers from /pwa_gis_tracking/api/offices/geom (wkb_geometry)
 * - Numeric zone sorting (1,2,3,...10)
 */

// ==========================================
// Global State
// ==========================================
var dashboardData = null;
var layerChartInstance = null;
var allBranches = [];
var layerNames = [];
var zoneMap = null;
var zoneMarkers = {};
var branchMarkerLayer = null;
var branchMarkerMap = {};   // pwa_code -> { marker, office }

// Pagination for full data table
var fullTablePage = 1;
var ROWS_PER_PAGE = 20;
var fullTableSorted = [];

// Color palette for zones (indexed by zone number - 1)
var ZONE_COLORS = [
    '#2E86C1', '#E67E22', '#27AE60', '#8E44AD', '#E74C3C',
    '#1ABC9C', '#F39C12', '#3498DB', '#D35400', '#2ECC71',
    '#9B59B6', '#C0392B', '#16A085', '#F1C40F', '#2980B9'
];

// Color palette for GIS layers
var LAYER_COLORS = [
    '#3498DB', '#E74C3C', '#2ECC71', '#F39C12', '#9B59B6',
    '#1ABC9C', '#E67E22', '#34495E', '#D35400'
];

// PWA Zone office centers (approximate coordinates for zone-level markers)
var ZONE_CENTERS = {
    "1":  { lat: 13.36, lng: 101.15, name: "East",           region: "Zone 1 (Chonburi)" },
    "2":  { lat: 14.53, lng: 100.90, name: "Central",        region: "Zone 2 (Saraburi)" },
    "3":  { lat: 13.54, lng: 99.82,  name: "West",           region: "Zone 3 (Ratchaburi)" },
    "4":  { lat: 9.40,  lng: 99.10,  name: "Upper South",    region: "Zone 4 (Surat Thani)" },
    "5":  { lat: 7.20,  lng: 100.50, name: "Lower South",    region: "Zone 5 (Songkhla)" },
    "6":  { lat: 16.20, lng: 102.80, name: "Central Isan",   region: "Zone 6 (Khon Kaen)" },
    "7":  { lat: 17.40, lng: 102.30, name: "Upper Isan",     region: "Zone 7 (Udon Thani)" },
    "8":  { lat: 15.25, lng: 104.85, name: "Lower Isan",     region: "Zone 8 (Ubon Ratchathani)" },
    "9":  { lat: 18.50, lng: 99.00,  name: "Upper North",    region: "Zone 9 (Chiang Mai)" },
    "10": { lat: 16.40, lng: 100.30, name: "Lower North",    region: "Zone 10 (Nakhon Sawan)" }
};

// ==========================================
// Initialization (uses ThaiDatePicker from thai_custom_date.js)
// ==========================================
document.addEventListener('DOMContentLoaded', async function() {
    // Initialize Thai Buddhist Era date pickers
    ThaiDatePicker.init('#filterStartDate');
    ThaiDatePicker.init('#filterEndDate');

    await loadZones();
    await loadYears();
    await loadLayers();
    initZoneMap();
    loadDashboard();
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

/** Load zone options into the filter dropdown (sorted numerically by backend). */
async function loadZones() {
    try {
        var data = await apiGet('/pwa_gis_tracking/api/zones');
        var select = document.getElementById('filterZone');
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

/** Load year options (displayed as both AD and BE). */
async function loadYears() {
    try {
        var data = await apiGet('/pwa_gis_tracking/api/years');
        var select = document.getElementById('filterYear');
        if (data.data) {
            data.data.reverse().forEach(function(y) {
                var opt = document.createElement('option');
                opt.value = y;
                opt.textContent = (y + 543) + ' (' + y + ')';
                select.appendChild(opt);
            });
        }
    } catch (e) { console.error('Load years error:', e); }
}

/** Load GIS layer names for chart labels. */
async function loadLayers() {
    try {
        var data = await apiGet('/pwa_gis_tracking/api/layers');
        if (data.data) { layerNames = data.data; }
    } catch (e) { console.error('Load layers error:', e); }
}

// ==========================================
// Leaflet Map with Zone + Branch Markers
// ==========================================

/** Initialize the Leaflet map with zone center markers. */
function initZoneMap() {
    if (!window.L) return;

    zoneMap = L.map('zoneMap', {
        center: [13.0, 101.0],
        zoom: 6,
        zoomControl: true,
        scrollWheelZoom: true,
        attributionControl: false
    });

    // OpenStreetMap tile layer
    L.tileLayer('https://{s}.tile.openstreetmap.org/{z}/{x}/{y}.png', {
        maxZoom: 18,
        opacity: 0.55
    }).addTo(zoneMap);

    // Add numbered zone markers (large circles at zone centers)
    Object.keys(ZONE_CENTERS).forEach(function(zoneId) {
        var z = ZONE_CENTERS[zoneId];
        var color = ZONE_COLORS[parseInt(zoneId) - 1] || '#999';

        var icon = L.divIcon({
            className: 'zone-map-marker',
            html: '<div style="' +
                'background:' + color + ';color:#fff;' +
                'width:42px;height:42px;border-radius:50%;' +
                'display:flex;align-items:center;justify-content:center;' +
                'font-weight:700;font-size:15px;' +
                'border:3px solid rgba(255,255,255,0.9);' +
                'box-shadow:0 2px 10px rgba(0,0,0,0.4);cursor:pointer;' +
                'transition:transform .15s ease;z-index:1000;' +
                '" onmouseover="this.style.transform=\'scale(1.2)\'" ' +
                'onmouseout="this.style.transform=\'scale(1)\'">' + zoneId + '</div>',
            iconSize: [42, 42],
            iconAnchor: [21, 21]
        });

        var marker = L.marker([z.lat, z.lng], { icon: icon, zIndexOffset: 1000 }).addTo(zoneMap);
        marker.bindPopup(
            '<div style="text-align:center;min-width:160px;font-family:\'IBM Plex Sans Thai\',sans-serif;">' +
            '<div style="font-size:15px;font-weight:700;margin-bottom:4px;">เขต ' + zoneId + '</div>' +
            '<div style="color:#555;font-size:12px;">' + z.region + '</div>' +
            '<div style="color:#888;font-size:11px;margin-bottom:6px;">' + z.name + '</div>' +
            '<div id="mapPop' + zoneId + '" style="font-size:13px;font-weight:700;color:' + color + ';">—</div>' +
            '</div>'
        );

        // Click zone marker -> filter dashboard to this zone
        marker.on('click', function() {
            document.getElementById('filterZone').value = zoneId;
            document.querySelectorAll('.zone-item').forEach(function(item) {
                item.classList.remove('active');
                if (item.getAttribute('data-zone') === zoneId) item.classList.add('active');
            });
            var filtered = allBranches.filter(function(b) { return b.zone === zoneId; });
            document.getElementById('detailTitle').textContent = 'สาขาในเขต ' + zoneId;
            renderBranchList(filtered);
            renderFullTable(filtered);
        });

        zoneMarkers[zoneId] = marker;
    });

    // Load individual branch markers from database geometry
    loadBranchMarkers();
}

/**
 * Load branch markers with real coordinates from PostgreSQL wkb_geometry.
 * Each branch is shown as a small colored dot on the map.
 */
/**
 * Load branch markers from /pwa_gis_tracking/api/offices/geom.
 * Uses FontAwesome house-flood-water icon colored by zone.
 * Stores markers for hover tooltip enrichment after dashboard loads.
 */
async function loadBranchMarkers() {
    try {
        var data = await apiGet('/pwa_gis_tracking/api/offices/geom');
        if (!data.data || !zoneMap) return;

        branchMarkerLayer = L.layerGroup().addTo(zoneMap);
        branchMarkerMap = {};

        data.data.forEach(function(office) {
            if (!office.lat || !office.lng) return;
            if (office.lat < 4 || office.lat > 22 || office.lng < 96 || office.lng > 107) return;

            var zoneIdx = parseInt(office.zone) - 1;
            var color = ZONE_COLORS[zoneIdx >= 0 ? zoneIdx : 0] || '#999';

            var icon = L.divIcon({
                className: 'branch-marker',
                html: '<div style="' +
                    'color:' + color + ';font-size:14px;' +
                    'text-shadow:0 1px 3px rgba(0,0,0,0.4),0 0 2px rgba(255,255,255,0.8);' +
                    'cursor:pointer;' +
                    '"><i class="fa-solid fa-house-flood-water"></i></div>',
                iconSize: [16, 16],
                iconAnchor: [8, 8]
            });

            var m = L.marker([office.lat, office.lng], { icon: icon }).addTo(branchMarkerLayer);

            // Default popup (click)
            m.bindPopup(
                '<div style="font-family:\'IBM Plex Sans Thai\',sans-serif;min-width:140px;">' +
                '<div style="font-weight:600;font-size:13px;">' + office.name + '</div>' +
                '<div style="color:#666;font-size:11px;">รหัส: ' + office.pwa_code + '</div>' +
                '<div style="color:#888;font-size:11px;">เขต: ' + office.zone + '</div>' +
                '</div>'
            );

            // Default hover tooltip (enriched after dashboard data loads)
            m.bindTooltip(office.pwa_code + ' ' + office.name, {
                direction: 'top', offset: [0, -10], className: 'branch-tooltip'
            });

            branchMarkerMap[office.pwa_code] = { marker: m, office: office };
        });

    } catch (e) {
        console.error('Load branch markers error:', e);
    }
}

/**
 * Enrich branch marker tooltips with meter count and pipe length.
 * Called after dashboard data loads.
 */
function updateBranchTooltips() {
    if (!allBranches || !allBranches.length) return;

    allBranches.forEach(function(b) {
        var entry = branchMarkerMap[b.pwa_code];
        if (!entry) return;

        var meterCount = (b.layers || {}).meter || 0;
        var pipeLong = b.pipe_long || 0;
        var office = entry.office;
        var zoneIdx = parseInt(office.zone) - 1;
        var color = ZONE_COLORS[zoneIdx >= 0 ? zoneIdx : 0] || '#999';

        // Rebind hover tooltip with data
        entry.marker.unbindTooltip();
        entry.marker.bindTooltip(
            '<div style="font-family:\'IBM Plex Sans Thai\',sans-serif;min-width:180px;line-height:1.5;">' +
            '<div style="font-weight:700;font-size:13px;color:' + color + ';">' +
            '<i class="fa-solid fa-house-flood-water" style="margin-right:4px;"></i>' + office.name + '</div>' +
            '<div style="color:#555;font-size:11px;margin-bottom:4px;">รหัส ' + b.pwa_code + ' | เขต ' + b.zone + '</div>' +
            '<hr style="margin:0 0 4px 0;border:0;border-top:1px solid #e0e0e0;">' +
            '<div style="display:flex;justify-content:space-between;font-size:12px;margin-bottom:2px;">' +
            '<span style="color:#666;">มาตรวัดน้ำ</span>' +
            '<span style="font-weight:700;color:#3498DB;">' + formatNumber(meterCount) + '</span></div>' +
            '<div style="display:flex;justify-content:space-between;font-size:12px;">' +
            '<span style="color:#666;">ความยาวท่อ(ม.)</span>' +
            '<span style="font-weight:700;color:#E67E22;">' + formatDecimal(pipeLong) + '</span></div>' +
            '</div>',
            { direction: 'top', offset: [0, -10], className: 'branch-tooltip', sticky: false }
        );

        // Also enrich click popup
        entry.marker.unbindPopup();
        entry.marker.bindPopup(
            '<div style="font-family:\'IBM Plex Sans Thai\',sans-serif;min-width:190px;line-height:1.6;">' +
            '<div style="font-weight:700;font-size:14px;color:' + color + ';">' +
            '<i class="fa-solid fa-house-flood-water" style="margin-right:4px;"></i>' + office.name + '</div>' +
            '<div style="color:#666;font-size:11px;margin-bottom:6px;">รหัส ' + b.pwa_code + ' | เขต ' + b.zone + '</div>' +
            '<div style="background:#f8f9fa;border-radius:6px;padding:6px 8px;font-size:12px;">' +
            '<div style="display:flex;justify-content:space-between;margin-bottom:3px;">' +
            '<span>💧 มาตรวัดน้ำ</span><strong style="color:#3498DB;">' + formatNumber(meterCount) + '</strong></div>' +
            '<div style="display:flex;justify-content:space-between;">' +
            '<span>📏 ความยาวท่อ</span><strong style="color:#E67E22;">' + formatDecimal(pipeLong) + ' ม.</strong></div>' +
            '</div></div>'
        );
    });
}

/** Update zone popup content with feature counts after dashboard data loads. */
function updateMapPopups(data) {
    if (!zoneMap) return;
    var zt = data.zone_totals || {};
    var branches = data.branches || [];

    // Compute per-zone meter count and pipe length
    var zoneMeter = {};
    var zonePipe = {};
    branches.forEach(function(b) {
        var z = b.zone;
        if (!zoneMeter[z]) zoneMeter[z] = 0;
        if (!zonePipe[z]) zonePipe[z] = 0;
        zoneMeter[z] += (b.layers || {}).meter || 0;
        zonePipe[z] += b.pipe_long || 0;
    });

    Object.keys(ZONE_CENTERS).forEach(function(zoneId) {
        var zd = zt[zoneId] || {};
        var branchCount = zd._branches || 0;
        var meterCount = zoneMeter[zoneId] || 0;
        var pipeLong = zonePipe[zoneId] || 0;
        var el = document.getElementById('mapPop' + zoneId);
        if (el) {
            el.innerHTML =
                '<div style="font-size:12px;text-align:left;margin-top:4px;">' +
                '<div style="display:flex;justify-content:space-between;margin-bottom:2px;">' +
                '<span style="color:#666;">สาขา</span>' +
                '<span style="font-weight:700;">' + branchCount + ' สาขา</span></div>' +
                '<div style="display:flex;justify-content:space-between;margin-bottom:2px;">' +
                '<span style="color:#666;">💧 จำนวนมาตรวัดน้ำ</span>' +
                '<span style="font-weight:700;color:#3498DB;">' + formatNumber(meterCount) + ' เครื่อง</span></div>' +
                '<div style="display:flex;justify-content:space-between;">' +
                '<span style="color:#666;">📏 ความยาวท่อรวม</span>' +
                '<span style="font-weight:700;color:#E67E22;">' + formatDecimal(pipeLong) + ' ม.</span></div>' +
                '</div>';
        }
    });

    // Also add zone marker hover tooltips with meter/pipe data
    Object.keys(ZONE_CENTERS).forEach(function(zoneId) {
        var marker = zoneMarkers[zoneId];
        if (!marker) return;
        var meterCount = zoneMeter[zoneId] || 0;
        var pipeLong = zonePipe[zoneId] || 0;
        var color = ZONE_COLORS[parseInt(zoneId) - 1] || '#999';

        marker.unbindTooltip();
        marker.bindTooltip(
            '<div style="font-family:\'IBM Plex Sans Thai\',sans-serif;min-width:200px;line-height:1.5;">' +
            '<div style="font-weight:700;font-size:13px;color:' + color + ';margin-bottom:4px;">เขต ' + zoneId + '</div>' +
            '<hr style="margin:0 0 4px 0;border:0;border-top:1px solid #e0e0e0;">' +
            '<div style="display:flex;justify-content:space-between;font-size:12px;margin-bottom:2px;">' +
            '<span style="color:#666;">💧 จำนวนมาตรวัดน้ำในเขต</span>' +
            '<span style="font-weight:700;color:#3498DB;">' + formatNumber(meterCount) + ' เครื่อง</span></div>' +
            '<div style="display:flex;justify-content:space-between;font-size:12px;">' +
            '<span style="color:#666;">📏 ความยาวท่อรวมในเขต</span>' +
            '<span style="font-weight:700;color:#E67E22;">' + formatDecimal(pipeLong) + ' ม.</span></div>' +
            '</div>',
            { direction: 'top', offset: [0, -25], className: 'zone-tooltip', sticky: false }
        );
    });
}

// ==========================================
// Dashboard Data Loading
// ==========================================

/** Fetch and render the full dashboard summary. */
async function loadDashboard() {
    showLoading('Loading dashboard data...');

    try {
        var zone = document.getElementById('filterZone').value;
        var startDate = document.getElementById('filterStartDate').value;
        var endDate = document.getElementById('filterEndDate').value;

        var url = '/pwa_gis_tracking/api/dashboard?';
        if (zone) url += 'zone=' + zone + '&';
        if (startDate) url += 'startDate=' + startDate + '&';
        if (endDate) url += 'endDate=' + endDate + '&';

        updateProgress('Fetching data from MongoDB...');
        var data = await apiGet(url);

        if (data.status === 'success') {
            dashboardData = data;
            allBranches = data.branches || [];
            renderStats(data);
            renderZoneList(data);
            renderLayerChart(data);
            renderBranchList(allBranches);
            renderFullTable(allBranches);
            updateMapPopups(data);
            updateBranchTooltips();
        }
    } catch (e) {
        console.error('Dashboard load error:', e);
        showToast('Failed to load data: ' + e.message, 'error');
    } finally {
        hideLoading();
    }
}

// ==========================================
// Render Functions
// ==========================================

/** Render summary stat cards at the top of the dashboard. */
function renderStats(data) {
    var container = document.getElementById('statsCards');
    var gt = data.grand_total || {};
    var totalBranches = data.total_branches || 0;
    var totalFeatures = gt._total || 0;

    // Get top 3 layers by count
    var topLayers = Object.entries(gt)
        .filter(function(e) { return !e[0].startsWith('_'); })
        .sort(function(a, b) { return b[1] - a[1]; })
        .slice(0, 3);

    // Compute pipe length totals across all branches
    var totalPipeLongM = 0;
    (data.branches || []).forEach(function(b) { totalPipeLongM += b.pipe_long || 0; });
    var totalPipeLongKm = totalPipeLongM / 1000;

    // Get specific layer counts from grand total
    var firehydrantCount = gt.firehydrant || 0;
    var valveCount = gt.valve || 0;

    var stats = [
        { label: 'สาขาทั้งหมด', value: formatNumber(totalBranches), color: 'blue', suffix: 'สาขา' },
        { label: 'ปริมาณข้อมูลทั้งหมด (features)', value: formatNumber(totalFeatures), color: 'gold', suffix: '' },
        { label: 'เขต', value: data.zone_names ? data.zone_names.length : 0, color: 'green', suffix: 'เขต' },
        { label: 'ความยาวท่อรวม (ม.)', value: formatDecimal(totalPipeLongM), color: 'cyan', suffix: '' },
        { label: 'ความยาวท่อรวม (กม.)', value: formatDecimal(totalPipeLongKm), color: 'cyan', suffix: '' },
        { label: 'หัวดับเพลิง', value: formatNumber(firehydrantCount), color: 'blue', suffix: 'records' },
        { label: 'ประตูน้ำ', value: formatNumber(valveCount), color: 'green', suffix: 'records' },
        { label: 'มาตรวัดน้ำ', value: formatNumber(gt.meter || 0), color: 'blue', suffix: 'records' }
    ];

    container.innerHTML = stats.map(function(s, i) {
        return '<div class="stat-card ' + s.color + ' fade-in" style="animation-delay:' + (i * 0.05) + 's">' +
            '<div class="stat-label">' + s.label + '</div>' +
            '<div class="stat-value">' + s.value + '</div>' +
            '<div class="text-xs mt-1" style="color:var(--text-muted)">' + s.suffix + '</div>' +
            '</div>';
    }).join('');
}

/** Render the zone sidebar list with branch counts and totals. */
function renderZoneList(data) {
    var container = document.getElementById('zoneList');
    var zt = data.zone_totals || {};
    var zoneNames = data.zone_names || [];

    document.getElementById('totalZones').textContent = zoneNames.length + ' เขต';

    // "All zones" item
    var html = '<li class="zone-item active" onclick="selectZone(this,\'\')" data-zone="">' +
        '<span class="zone-name">ข้อมูลทั้งหมด</span>' +
        '<span class="zone-count">' + formatNumber(data.total_branches) + ' สาขา</span>' +
        '</li>';

    zoneNames.forEach(function(z) {
        var zd = zt[z] || {};
        var branchCount = zd._branches || 0;
        var total = zd._total || 0;
        var color = ZONE_COLORS[parseInt(z) - 1] || '#999';

        html += '<li class="zone-item" onclick="selectZone(this,\'' + z + '\')" data-zone="' + z + '">' +
            '<div style="display:flex;align-items:center;gap:8px;">' +
            '<span style="width:12px;height:12px;border-radius:50%;background:' + color + ';display:inline-block;flex-shrink:0;"></span>' +
            '<div>' +
            '<span class="zone-name">เขต ' + z + '</span>' +
            '<div class="text-xs" style="color:var(--text-muted)">' + formatNumber(total) + ' records</div>' +
            '</div>' +
            '</div>' +
            '<span class="zone-count">' + branchCount + ' สาขา</span>' +
            '</li>';
    });

    container.innerHTML = html;
}

/** Handle zone selection from the sidebar list. */
function selectZone(el, zone) {
    document.querySelectorAll('.zone-item').forEach(function(it) { it.classList.remove('active'); });
    el.classList.add('active');

    var filtered = allBranches;
    if (zone) {
        filtered = allBranches.filter(function(b) { return b.zone === zone; });
        document.getElementById('detailTitle').textContent = 'สาขาในเขต ' + zone;

        // Fly map to selected zone
        if (zoneMap && ZONE_CENTERS[zone]) {
            zoneMap.flyTo([ZONE_CENTERS[zone].lat, ZONE_CENTERS[zone].lng], 8, { duration: 0.8 });
            if (zoneMarkers[zone]) zoneMarkers[zone].openPopup();
        }
    } else {
        document.getElementById('detailTitle').textContent = 'มาตรวัดน้ำทั้งหมด';
        if (zoneMap) zoneMap.flyTo([13.0, 101.0], 6, { duration: 0.8 });
    }

    renderBranchList(filtered);
    renderFullTable(filtered);
}

/** Render the doughnut chart showing feature distribution by layer. */
function renderLayerChart(data) {
    var ctx = document.getElementById('layerChart').getContext('2d');
    var gt = data.grand_total || {};

    var layers = Object.entries(gt)
        .filter(function(e) { return !e[0].startsWith('_'); })
        .sort(function(a, b) { return b[1] - a[1]; });

    var labels = layers.map(function(e) {
        var ln = layerNames.find(function(l) { return l.name === e[0]; });
        return ln ? ln.display_name : e[0];
    });
    var values = layers.map(function(e) { return e[1]; });

    if (layerChartInstance) layerChartInstance.destroy();

    layerChartInstance = new Chart(ctx, {
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
            responsive: true,
            maintainAspectRatio: true,
            cutout: '55%',
            plugins: {
                legend: {
                    position: 'bottom',
                    labels: {
                        color: '#94A3B8',
                        font: { family: 'IBM Plex Sans Thai', size: 11 },
                        padding: 12,
                        usePointStyle: true,
                        pointStyleWidth: 8
                    }
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

/** Render the branch list (right sidebar), sorted by meter count descending. */
function renderBranchList(branches) {
    var tbody = document.getElementById('branchTableBody');
    document.getElementById('branchCountBadge').textContent = branches.length + ' สาขา';

    var sorted = branches.slice().sort(function(a, b) {
        var mA = (a.layers || {}).meter || 0;
        var mB = (b.layers || {}).meter || 0;
        return mB - mA;
    });

    tbody.innerHTML = sorted.map(function(b, i) {
        var meterCount = (b.layers || {}).meter || 0;
        return '<tr>' +
            '<td class="text-xs" style="color:var(--text-muted)">' + (i + 1) + '</td>' +
            '<td><span class="badge badge-blue">' + b.pwa_code + '</span></td>' +
            '<td class="text-sm">' + b.branch_name + '</td>' +
            '<td class="num ' + (meterCount === 0 ? 'zero' : '') + '">' + formatNumber(meterCount) + '</td>' +
            '</tr>';
    }).join('');
}

/**
 * Render the full data table.
 * - "ความยาวท่อ(ม.)" column inserted right after "ท่อประปา" (pipe)
 * - Pagination: 20 rows per page
 */
function renderFullTable(branches) {
    var thead = document.getElementById('fullTableHeader');
    var tfoot = document.getElementById('fullTableFooter');
    var layers = layerNames.map(function(l) { return l.name; });
    var pipeIdx = layers.indexOf('pipe');

    // Header: #, รหัสสาขา, ชื่อสาขา, เขต, [layers + ความยาวท่อ after pipe], ผลรวม
    var hh = '<th>#</th><th>รหัสสาขา</th><th>ชื่อสาขา</th><th>เขต</th>';
    layerNames.forEach(function(l, idx) {
        hh += '<th class="text-right">' + l.display_name + '</th>';
        if (idx === pipeIdx) {
            hh += '<th class="text-right" style="color:#E67E22;">ความยาวท่อ(ม.)</th>';
        }
    });
    hh += '<th class="text-right">ผลรวม</th>';
    thead.innerHTML = hh;

    // Sort by zone (numeric) then pwa_code
    fullTableSorted = branches.slice().sort(function(a, b) {
        var za = parseInt(a.zone) || 0, zb = parseInt(b.zone) || 0;
        if (za !== zb) return za - zb;
        return a.pwa_code.localeCompare(b.pwa_code);
    });

    // Reset to page 1 and render
    fullTablePage = 1;
    renderTablePage();

    // Footer totals (always for full dataset)
    var totals = {};
    var grandTotal = 0;
    var totalPipeLong = 0;
    layers.forEach(function(l) { totals[l] = 0; });
    fullTableSorted.forEach(function(b) {
        layers.forEach(function(l) { totals[l] += (b.layers || {})[l] || 0; });
        grandTotal += b.total;
        totalPipeLong += b.pipe_long || 0;
    });

    var fh = '<tr class="total-row"><td colspan="4"><strong>รวมทั้งหมด (' + fullTableSorted.length + ' สาขา)</strong></td>';
    layers.forEach(function(l, idx) {
        fh += '<td class="num">' + formatNumber(totals[l]) + '</td>';
        if (idx === pipeIdx) {
            fh += '<td class="num" style="color:#E67E22;font-weight:600;">' + formatDecimal(totalPipeLong) + '</td>';
        }
    });
    fh += '<td class="num" style="color:var(--pwa-gold);font-weight:700">' + formatNumber(grandTotal) + '</td></tr>';
    tfoot.innerHTML = fh;
}

/** Render current page of the table body + pagination controls. */
function renderTablePage() {
    var tbody = document.getElementById('fullTableBody');
    var layers = layerNames.map(function(l) { return l.name; });
    var pipeIdx = layers.indexOf('pipe');

    var total = fullTableSorted.length;
    var maxPage = Math.max(1, Math.ceil(total / ROWS_PER_PAGE));
    if (fullTablePage > maxPage) fullTablePage = maxPage;

    var start = (fullTablePage - 1) * ROWS_PER_PAGE;
    var end = Math.min(total, start + ROWS_PER_PAGE);
    var pageData = fullTableSorted.slice(start, end);

    tbody.innerHTML = pageData.map(function(b, pi) {
        var i = start + pi;
        var r = '<td class="text-xs" style="color:var(--text-muted)">' + (i + 1) + '</td>' +
            '<td><span class="badge badge-blue">' + b.pwa_code + '</span></td>' +
            '<td class="text-sm">' + b.branch_name + '</td>' +
            '<td><span class="badge badge-gold">' + b.zone + '</span></td>';
        layers.forEach(function(l, idx) {
            var val = (b.layers || {})[l] || 0;
            r += '<td class="num ' + (val === 0 ? 'zero' : '') + '">' + formatNumber(val) + '</td>';
            if (idx === pipeIdx) {
                var pl = b.pipe_long || 0;
                r += '<td class="num ' + (pl === 0 ? 'zero' : '') + '" style="color:#E67E22;">' + formatDecimal(pl) + '</td>';
            }
        });
        r += '<td class="num" style="font-weight:600;color:var(--pwa-gold)">' + formatNumber(b.total) + '</td>';
        return '<tr>' + r + '</tr>';
    }).join('');

    // Render pagination controls
    var pag = document.getElementById('fullTablePagination');
    if (!pag) return;

    var from = total === 0 ? 0 : start + 1;
    pag.innerHTML =
        '<div class="text-xs" style="color:var(--text-muted);">แสดง ' + from + ' – ' + end + ' จาก ' + total + ' สาขา</div>' +
        '<div style="display:flex;align-items:center;gap:8px;">' +
        '<button class="btn btn-outline" style="padding:4px 12px;font-size:12px;" id="pgPrev" ' +
            (fullTablePage <= 1 ? 'disabled' : '') + '>◀ ก่อนหน้า</button>' +
        '<span class="text-xs" style="color:var(--text-secondary);">หน้า ' + fullTablePage + ' / ' + maxPage + '</span>' +
        '<button class="btn btn-outline" style="padding:4px 12px;font-size:12px;" id="pgNext" ' +
            (fullTablePage >= maxPage ? 'disabled' : '') + '>ถัดไป ▶</button>' +
        '</div>';

    document.getElementById('pgPrev').onclick = function() {
        if (fullTablePage > 1) { fullTablePage--; renderTablePage(); }
    };
    document.getElementById('pgNext').onclick = function() {
        if (fullTablePage < maxPage) { fullTablePage++; renderTablePage(); }
    };
}

// ==========================================
// Filter Functions
// ==========================================

/** Placeholder for zone filter change event. */
function onFilterChange() {}

/** Set date range when year filter changes. */
function onYearChange() {
    var year = document.getElementById('filterYear').value;
    if (year) {
        var fpStart = document.getElementById('filterStartDate')._flatpickr;
        var fpEnd = document.getElementById('filterEndDate')._flatpickr;
        if (fpStart) fpStart.setDate(year + '-01-01', true);
        if (fpEnd) fpEnd.setDate(year + '-12-31', true);
    }
}

/** Reset all filters and reload dashboard. */
function resetFilters() {
    document.getElementById('filterZone').value = '';
    document.getElementById('filterYear').value = '';

    var fpStart = document.getElementById('filterStartDate')._flatpickr;
    var fpEnd = document.getElementById('filterEndDate')._flatpickr;
    if (fpStart) fpStart.clear();
    if (fpEnd) fpEnd.clear();

    if (zoneMap) zoneMap.flyTo([13.0, 101.0], 6, { duration: 0.5 });
    loadDashboard();
}

/** Client-side table search filtering. */
function filterTable() {
    var search = document.getElementById('tableSearch').value.toLowerCase();
    var rows = document.querySelectorAll('#fullTableBody tr');
    rows.forEach(function(row) {
        row.style.display = row.textContent.toLowerCase().includes(search) ? '' : 'none';
    });
}

// ==========================================
// Export
// ==========================================

/** Trigger Excel download via API. */
function exportExcel() {
    var zone = document.getElementById('filterZone').value;
    var startDate = document.getElementById('filterStartDate').value;
    var endDate = document.getElementById('filterEndDate').value;

    var url = '/pwa_gis_tracking/api/export/excel?';
    if (zone) url += 'zone=' + zone + '&';
    if (startDate) url += 'startDate=' + startDate + '&';
    if (endDate) url += 'endDate=' + endDate + '&';

    showToast('Generating Excel file...', 'info');
    window.location.href = url;
}

// ==========================================
// Utilities
// ==========================================

/** Format number with Thai locale separators. */
function formatNumber(n) {
    if (n === null || n === undefined) return '0';
    return Number(n).toLocaleString('th-TH');
}

/** Format decimal number with 2 decimal places. */
function formatDecimal(n) {
    if (n === null || n === undefined || n === 0) return '0.00';
    return Number(n).toLocaleString('th-TH', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

/** Show loading overlay with message. */
function showLoading(text) {
    document.getElementById('loadingOverlay').style.display = 'flex';
    document.querySelector('.loading-text').textContent = text || 'Loading...';
}

/** Update the progress text in the loading overlay. */
function updateProgress(text) {
    var el = document.getElementById('loadingProgress');
    if (el) el.textContent = text;
}

/** Hide loading overlay. */
function hideLoading() {
    document.getElementById('loadingOverlay').style.display = 'none';
}

/** Show a toast notification. */
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