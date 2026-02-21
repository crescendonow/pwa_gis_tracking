/**
 * PWA GIS Online Tracking - Detail Page
 * Branch-level detail view with layer counts, charts, and export options.
 */

var detailPieInstance = null;
var currentBranchData = null;
var allLayers = [];

// Color palette for GIS layers
var LAYER_COLORS = [
    '#3498DB', '#E74C3C', '#2ECC71', '#F39C12', '#9B59B6',
    '#1ABC9C', '#E67E22', '#34495E', '#D35400'
];

// ==========================================
// Initialization
// ==========================================
document.addEventListener('DOMContentLoaded', async function() {
    // Initialize Thai Buddhist Era date pickers
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

/** Load zone options into the detail page dropdown. */
async function loadDetailZones() {
    try {
        var data = await apiGet('/api/zones');
        var select = document.getElementById('detailZone');
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

/** Load layer options into both the filter and export dropdowns. */
async function loadDetailLayers() {
    try {
        var data = await apiGet('/api/layers');
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

/** Load branches when zone selection changes. */
async function onDetailZoneChange() {
    var zone = document.getElementById('detailZone').value;
    var branchSelect = document.getElementById('detailBranch');
    branchSelect.innerHTML = '<option value="">Select branch</option>';
    if (!zone) return;

    try {
        var data = await apiGet('/api/offices?zone=' + zone);
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

/** Fetch feature counts for the selected branch and render results. */
async function loadBranchDetail() {
    var pwaCode = document.getElementById('detailBranch').value;
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;

    if (!pwaCode) {
        showToast('Please select a branch', 'error');
        return;
    }

    showLoading('Counting features...');

    try {
        var url = '/api/counts?pwaCode=' + pwaCode;
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
        }
    } catch (e) {
        console.error('Load detail error:', e);
        showToast('Failed to load data: ' + e.message, 'error');
    } finally {
        hideLoading();
    }
}

// ==========================================
// Render Functions
// ==========================================

/** Render summary stat cards for the selected branch. */
function renderDetailStats(data) {
    var container = document.getElementById('detailStats');
    var layers = data.layers || {};
    var total = data.total || 0;

    // Get top 4 layers by count
    var topLayers = Object.entries(layers)
        .sort(function(a, b) { return b[1] - a[1]; })
        .slice(0, 4);

    var stats = [
        { label: 'Grand Total', value: formatNumber(total), color: 'gold' }
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
        return '<div class="stat-card ' + s.color + ' fade-in" style="animation-delay: ' + (i * 0.05) + 's">' +
            '<div class="stat-label">' + s.label + '</div>' +
            '<div class="stat-value">' + s.value + '</div>' +
            '</div>';
    }).join('');
}

/** Render the doughnut chart for layer distribution of the selected branch. */
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

/** Render the layer count table for the selected branch. */
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

        return '<tr>' +
            '<td><span class="badge badge-blue">' + e[0] + '</span></td>' +
            '<td>' + displayName + '</td>' +
            '<td class="num ' + cls + '">' + formatNumber(e[1]) + '</td>' +
            '<td class="num">' + pct + '%</td>' +
            '</tr>';
    }).join('');
}

// ==========================================
// Export Functions
// ==========================================

/** Download Excel summary report for the selected zone/branch. */
function exportDetailExcel() {
    var pwaCode = document.getElementById('detailBranch').value;
    var zone = document.getElementById('detailZone').value;
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;

    if (!pwaCode && !zone) {
        showToast('Please select a zone or branch', 'error');
        return;
    }

    var url = '/api/export/excel?';
    if (zone) url += 'zone=' + zone + '&';
    if (startDate) url += 'startDate=' + startDate + '&';
    if (endDate) url += 'endDate=' + endDate + '&';

    showToast('Generating Excel file...', 'info');
    window.location.href = url;
}

/** Download GeoJSON/GPKG for the selected branch and layer. */
function exportGeoData() {
    var pwaCode = document.getElementById('detailBranch').value;
    var layer = document.getElementById('exportLayer').value;
    var format = document.getElementById('exportFormat').value;
    var startDate = document.getElementById('detailStartDate').value;
    var endDate = document.getElementById('detailEndDate').value;

    if (!pwaCode) {
        showToast('Please select a branch', 'error');
        return;
    }
    if (!layer) {
        showToast('Please select a layer', 'error');
        return;
    }

    var url = '/api/export/geodata?pwaCode=' + pwaCode +
        '&collection=' + layer +
        '&format=' + format;
    if (startDate) url += '&startDate=' + startDate;
    if (endDate) url += '&endDate=' + endDate;

    showToast('Exporting ' + layer + ' data...', 'info');
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
    document.getElementById('loadingText').textContent = text || 'Loading...';
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