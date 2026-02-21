/**
 * PWA GIS Online Tracking - Dashboard Page
 * - Uses ThaiDatePicker (thai_custom_date.js) for Buddhist Era calendar
 * - Loads branch markers from /api/offices/geom (wkb_geometry)
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
        var data = await apiGet('/api/zones');
        var select = document.getElementById('filterZone');
        if (data.data) {
            data.data.forEach(function(z) {
                var opt = document.createElement('option');
                opt.value = z.zone;
                opt.textContent = 'Zone ' + z.zone + ' (' + z.branch_count + ' branches)';
                select.appendChild(opt);
            });
        }
    } catch (e) { console.error('Load zones error:', e); }
}

/** Load year options (displayed as both AD and BE). */
async function loadYears() {
    try {
        var data = await apiGet('/api/years');
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
        var data = await apiGet('/api/layers');
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
            '<div style="font-size:15px;font-weight:700;margin-bottom:4px;">Zone ' + zoneId + '</div>' +
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
            document.getElementById('detailTitle').textContent = 'Branches in Zone ' + zoneId;
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
async function loadBranchMarkers() {
    try {
        var data = await apiGet('/api/offices/geom');
        if (!data.data || !zoneMap) return;

        branchMarkerLayer = L.layerGroup().addTo(zoneMap);

        data.data.forEach(function(office) {
            if (!office.lat || !office.lng) return;
            // Sanity check: Thailand bounds (lat ~5-21, lng ~97-106)
            if (office.lat < 4 || office.lat > 22 || office.lng < 96 || office.lng > 107) return;

            var zoneIdx = parseInt(office.zone) - 1;
            var color = ZONE_COLORS[zoneIdx >= 0 ? zoneIdx : 0] || '#999';

            var icon = L.divIcon({
                className: 'branch-marker',
                html: '<div style="' +
                    'width:10px;height:10px;border-radius:50%;' +
                    'background:' + color + ';' +
                    'border:2px solid rgba(255,255,255,0.8);' +
                    'box-shadow:0 1px 4px rgba(0,0,0,0.3);' +
                    '"></div>',
                iconSize: [10, 10],
                iconAnchor: [5, 5]
            });

            var m = L.marker([office.lat, office.lng], { icon: icon }).addTo(branchMarkerLayer);
            m.bindPopup(
                '<div style="font-family:\'IBM Plex Sans Thai\',sans-serif;min-width:140px;">' +
                '<div style="font-weight:600;font-size:13px;">' + office.name + '</div>' +
                '<div style="color:#666;font-size:11px;">Code: ' + office.pwa_code + '</div>' +
                '<div style="color:#888;font-size:11px;">Zone: ' + office.zone + '</div>' +
                '</div>'
            );
        });

    } catch (e) {
        console.error('Load branch markers error:', e);
    }
}

/** Update zone popup content with feature counts after dashboard data loads. */
function updateMapPopups(data) {
    if (!zoneMap) return;
    var zt = data.zone_totals || {};
    Object.keys(ZONE_CENTERS).forEach(function(zoneId) {
        var zd = zt[zoneId] || {};
        var total = zd._total || 0;
        var branches = zd._branches || 0;
        var el = document.getElementById('mapPop' + zoneId);
        if (el) {
            el.innerHTML = formatNumber(total) + ' records<br>' +
                '<span style="font-size:11px;color:#888;">' + branches + ' branches</span>';
        }
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

        var url = '/api/dashboard?';
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

    var stats = [
        { label: 'Total Branches', value: formatNumber(totalBranches), color: 'blue', suffix: 'branches' },
        { label: 'Total Features', value: formatNumber(totalFeatures), color: 'gold', suffix: 'records' },
        { label: 'Zones', value: data.zone_names ? data.zone_names.length : 0, color: 'green', suffix: 'zones' }
    ];

    topLayers.forEach(function(l, i) {
        var dn = layerNames.find(function(ln) { return ln.name === l[0]; });
        stats.push({
            label: dn ? dn.display_name : l[0],
            value: formatNumber(l[1]),
            color: ['cyan', 'blue', 'green'][i % 3],
            suffix: 'records'
        });
    });

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

    document.getElementById('totalZones').textContent = zoneNames.length + ' zones';

    // "All zones" item
    var html = '<li class="zone-item active" onclick="selectZone(this,\'\')" data-zone="">' +
        '<span class="zone-name">All Zones</span>' +
        '<span class="zone-count">' + formatNumber(data.total_branches) + ' branches</span>' +
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
            '<span class="zone-name">Zone ' + z + '</span>' +
            '<div class="text-xs" style="color:var(--text-muted)">' + formatNumber(total) + ' records</div>' +
            '</div>' +
            '</div>' +
            '<span class="zone-count">' + branchCount + ' branches</span>' +
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
        document.getElementById('detailTitle').textContent = 'Branches in Zone ' + zone;

        // Fly map to selected zone
        if (zoneMap && ZONE_CENTERS[zone]) {
            zoneMap.flyTo([ZONE_CENTERS[zone].lat, ZONE_CENTERS[zone].lng], 8, { duration: 0.8 });
            if (zoneMarkers[zone]) zoneMarkers[zone].openPopup();
        }
    } else {
        document.getElementById('detailTitle').textContent = 'All Branches';
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

/** Render the branch list (right sidebar), sorted by total features descending. */
function renderBranchList(branches) {
    var tbody = document.getElementById('branchTableBody');
    document.getElementById('branchCountBadge').textContent = branches.length + ' branches';

    var sorted = branches.slice().sort(function(a, b) { return b.total - a.total; });

    tbody.innerHTML = sorted.map(function(b, i) {
        return '<tr>' +
            '<td class="text-xs" style="color:var(--text-muted)">' + (i + 1) + '</td>' +
            '<td><span class="badge badge-blue">' + b.pwa_code + '</span></td>' +
            '<td class="text-sm">' + b.branch_name + '</td>' +
            '<td class="num ' + (b.total === 0 ? 'zero' : '') + '">' + formatNumber(b.total) + '</td>' +
            '</tr>';
    }).join('');
}

/** Render the full data table with all layers as columns. */
function renderFullTable(branches) {
    var thead = document.getElementById('fullTableHeader');
    var tbody = document.getElementById('fullTableBody');
    var tfoot = document.getElementById('fullTableFooter');
    var layers = layerNames.map(function(l) { return l.name; });

    // Build header
    var hh = '<th>#</th><th>Code</th><th>Branch</th><th>Zone</th>';
    layerNames.forEach(function(l) { hh += '<th class="text-right">' + l.display_name + '</th>'; });
    hh += '<th class="text-right">Total</th>';
    thead.innerHTML = hh;

    // Sort by zone (numeric) then pwa_code
    var sorted = branches.slice().sort(function(a, b) {
        var za = parseInt(a.zone) || 0, zb = parseInt(b.zone) || 0;
        if (za !== zb) return za - zb;
        return a.pwa_code.localeCompare(b.pwa_code);
    });

    // Build body rows
    tbody.innerHTML = sorted.map(function(b, i) {
        var r = '<td class="text-xs" style="color:var(--text-muted)">' + (i + 1) + '</td>' +
            '<td><span class="badge badge-blue">' + b.pwa_code + '</span></td>' +
            '<td class="text-sm">' + b.branch_name + '</td>' +
            '<td><span class="badge badge-gold">' + b.zone + '</span></td>';
        layers.forEach(function(l) {
            var val = (b.layers || {})[l] || 0;
            r += '<td class="num ' + (val === 0 ? 'zero' : '') + '">' + formatNumber(val) + '</td>';
        });
        r += '<td class="num" style="font-weight:600;color:var(--pwa-gold)">' + formatNumber(b.total) + '</td>';
        return '<tr>' + r + '</tr>';
    }).join('');

    // Build footer totals
    var totals = {};
    var grandTotal = 0;
    layers.forEach(function(l) { totals[l] = 0; });
    sorted.forEach(function(b) {
        layers.forEach(function(l) { totals[l] += (b.layers || {})[l] || 0; });
        grandTotal += b.total;
    });

    var fh = '<tr class="total-row"><td colspan="4"><strong>Grand Total</strong></td>';
    layers.forEach(function(l) { fh += '<td class="num">' + formatNumber(totals[l]) + '</td>'; });
    fh += '<td class="num" style="color:var(--pwa-gold);font-weight:700">' + formatNumber(grandTotal) + '</td></tr>';
    tfoot.innerHTML = fh;
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

    var url = '/api/export/excel?';
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
