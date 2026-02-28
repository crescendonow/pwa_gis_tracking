/**
 * PWA GIS Online Tracking — Layer Data Modal
 * ============================================
 * Reusable modal for viewing paginated feature data per branch+layer.
 *
 * Usage:
 *   LayerModal.open({
 *     pwaCode: '1020',
 *     collection: 'pipe',
 *     layerDisplayName: 'ท่อประปา',
 *     startDate: '2025-01-01',
 *     endDate: '2025-12-31'
 *   });
 *
 * Features:
 *   - Dynamic columns from FieldMapping via /api/features/list
 *   - Paginated (50 records/page)
 *   - Case-insensitive search
 *   - Responsive with horizontal scroll
 *   - Keyboard navigation (Escape to close)
 *
 * Dependencies: none (vanilla JS, styles self-injected)
 */

var LayerModal = (function() {
    'use strict';

    // ==========================================
    // State
    // ==========================================
    var state = {
        pwaCode: '',
        collection: '',
        layerDisplayName: '',
        startDate: '',
        endDate: '',
        columns: [],
        data: [],
        page: 1,
        pageSize: 50,
        total: 0,
        totalPages: 0,
        search: '',
        searchTimer: null,
        loading: false
    };

    var modalEl = null;
    var stylesInjected = false;

    // ==========================================
    // Ensure Modal DOM exists
    // ==========================================
    function ensureModal() {
        if (modalEl) return;

        modalEl = document.createElement('div');
        modalEl.id = 'layerDataModal';
        modalEl.className = 'lm-overlay';
        modalEl.style.display = 'none';
        modalEl.innerHTML =
            '<div class="lm-dialog">' +
                /* Header */
                '<div class="lm-header">' +
                    '<div class="lm-header-left">' +
                        '<span class="lm-title" id="lmTitle">ข้อมูลชั้นข้อมูล</span>' +
                        '<span class="lm-badge" id="lmBadge">0 records</span>' +
                    '</div>' +
                    '<button class="lm-close" onclick="LayerModal.close()" title="ปิด">&times;</button>' +
                '</div>' +
                /* Toolbar */
                '<div class="lm-toolbar">' +
                    '<div class="lm-search-wrap">' +
                        '<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" ' +
                            'style="position:absolute;left:10px;top:50%;transform:translateY(-50%);color:#64748B;pointer-events:none;">' +
                            '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>' +
                        '<input type="text" id="lmSearch" placeholder="ค้นหา... (เช่น AC, 100, ชื่อลูกค้า)" autocomplete="off">' +
                    '</div>' +
                    '<div class="lm-toolbar-info" id="lmInfo"></div>' +
                '</div>' +
                /* Body */
                '<div class="lm-body">' +
                    '<div class="lm-loading" id="lmLoading">' +
                        '<div class="lm-spinner"></div>' +
                        '<div>กำลังโหลดข้อมูล...</div>' +
                    '</div>' +
                    '<div class="lm-table-wrap" id="lmTableWrap" style="display:none;">' +
                        '<table class="lm-table" id="lmTable">' +
                            '<thead id="lmThead"></thead>' +
                            '<tbody id="lmTbody"></tbody>' +
                        '</table>' +
                    '</div>' +
                    '<div class="lm-empty" id="lmEmpty" style="display:none;">' +
                        '<div style="font-size:36px;margin-bottom:8px;">📭</div>' +
                        '<div>ไม่พบข้อมูล</div>' +
                    '</div>' +
                '</div>' +
                /* Footer */
                '<div class="lm-footer">' +
                    '<div class="lm-page-info" id="lmPageInfo">—</div>' +
                    '<div class="lm-page-btns">' +
                        '<button class="lm-btn" id="lmFirst" onclick="LayerModal.firstPage()" title="หน้าแรก">⏮</button>' +
                        '<button class="lm-btn" id="lmPrev" onclick="LayerModal.prevPage()">◀ ก่อนหน้า</button>' +
                        '<span class="lm-page-num" id="lmPageNum">1 / 1</span>' +
                        '<button class="lm-btn" id="lmNext" onclick="LayerModal.nextPage()">ถัดไป ▶</button>' +
                        '<button class="lm-btn" id="lmLast" onclick="LayerModal.lastPage()" title="หน้าสุดท้าย">⏭</button>' +
                    '</div>' +
                '</div>' +
            '</div>';

        document.body.appendChild(modalEl);

        // Search with debounce
        var searchInput = document.getElementById('lmSearch');
        searchInput.addEventListener('input', function() {
            clearTimeout(state.searchTimer);
            state.searchTimer = setTimeout(function() {
                state.search = searchInput.value.trim();
                state.page = 1;
                loadData();
            }, 400);
        });
        // Enter = immediate search
        searchInput.addEventListener('keydown', function(e) {
            if (e.key === 'Enter') {
                clearTimeout(state.searchTimer);
                state.search = searchInput.value.trim();
                state.page = 1;
                loadData();
            }
        });

        // Close on overlay click
        modalEl.addEventListener('click', function(e) {
            if (e.target === modalEl) close();
        });

        // Close on Escape
        document.addEventListener('keydown', function(e) {
            if (e.key === 'Escape' && modalEl && modalEl.style.display !== 'none') close();
        });

        injectStyles();
    }

    // ==========================================
    // CSS (self-injected once)
    // ==========================================
    function injectStyles() {
        if (stylesInjected) return;
        stylesInjected = true;

        var css = document.createElement('style');
        css.id = 'lmStyles';
        css.textContent = [
            '.lm-overlay{position:fixed;inset:0;z-index:9999;background:rgba(0,0,0,0.6);backdrop-filter:blur(4px);display:flex;align-items:center;justify-content:center;padding:16px;animation:lmFade .2s ease}',
            '@keyframes lmFade{from{opacity:0}to{opacity:1}}',
            '.lm-dialog{background:var(--surface-1,#111827);border:1px solid var(--border,rgba(255,255,255,0.06));border-radius:16px;width:96vw;max-width:1300px;max-height:92vh;display:flex;flex-direction:column;box-shadow:0 24px 64px rgba(0,0,0,0.5);animation:lmUp .25s ease}',
            '@keyframes lmUp{from{transform:translateY(20px);opacity:0}to{transform:translateY(0);opacity:1}}',
            /* Header */
            '.lm-header{display:flex;align-items:center;justify-content:space-between;padding:14px 20px;border-bottom:1px solid var(--border,rgba(255,255,255,0.06))}',
            '.lm-header-left{display:flex;align-items:center;gap:10px;min-width:0}',
            '.lm-title{font-size:15px;font-weight:700;color:var(--text-primary,#F1F5F9);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}',
            '.lm-badge{font-size:11px;padding:3px 10px;border-radius:12px;background:rgba(212,168,67,0.15);color:#D4A843;font-weight:600;white-space:nowrap}',
            '.lm-close{background:none;border:none;color:var(--text-muted,#64748B);font-size:24px;cursor:pointer;padding:4px 8px;border-radius:8px;line-height:1;transition:all .15s}',
            '.lm-close:hover{color:#F1F5F9;background:rgba(255,255,255,0.06)}',
            /* Toolbar */
            '.lm-toolbar{display:flex;align-items:center;gap:12px;padding:10px 20px;border-bottom:1px solid var(--border,rgba(255,255,255,0.04));flex-wrap:wrap}',
            '.lm-search-wrap{position:relative;flex:1;min-width:200px}',
            '#lmSearch{width:100%;padding:8px 12px 8px 34px;background:var(--bg-secondary,#0A0F1A);border:1px solid var(--border,rgba(255,255,255,0.06));border-radius:8px;color:var(--text-primary,#F1F5F9);font-size:13px;outline:none;font-family:inherit;transition:border-color .2s}',
            '#lmSearch:focus{border-color:var(--pwa-blue-light,#2E86C1);box-shadow:0 0 0 2px rgba(46,134,193,0.15)}',
            '.lm-toolbar-info{font-size:12px;color:var(--text-muted,#64748B)}',
            /* Body */
            '.lm-body{flex:1;overflow:hidden;min-height:200px;position:relative;display:flex;flex-direction:column}',
            '.lm-table-wrap{flex:1;overflow:auto}',
            '.lm-table{width:100%;border-collapse:collapse;font-size:12px}',
            '.lm-table thead{position:sticky;top:0;z-index:2}',
            '.lm-table th{background:var(--surface-2,#1A2332);color:var(--text-secondary,#94A3B8);padding:8px 10px;text-align:left;font-weight:600;font-size:11px;white-space:nowrap;border-bottom:2px solid var(--border,rgba(255,255,255,0.06));letter-spacing:.3px}',
            '.lm-table td{padding:6px 10px;border-bottom:1px solid var(--border,rgba(255,255,255,0.03));color:var(--text-primary,#F1F5F9);white-space:nowrap;max-width:280px;overflow:hidden;text-overflow:ellipsis}',
            '.lm-table tr:hover td{background:rgba(46,134,193,0.06)}',
            '.lm-table td.num{text-align:right;font-variant-numeric:tabular-nums}',
            '.lm-table td.empty{color:var(--text-muted,#64748B);font-style:italic}',
            /* Loading */
            '.lm-loading{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;padding:60px 20px;color:var(--text-muted,#64748B);font-size:13px}',
            '.lm-spinner{width:32px;height:32px;border:3px solid var(--border,rgba(255,255,255,0.06));border-top-color:#D4A843;border-radius:50%;animation:lmSpin .6s linear infinite}',
            '@keyframes lmSpin{to{transform:rotate(360deg)}}',
            '.lm-empty{display:flex;flex-direction:column;align-items:center;justify-content:center;padding:60px 20px;color:var(--text-muted,#64748B);font-size:14px}',
            /* Footer */
            '.lm-footer{display:flex;align-items:center;justify-content:space-between;padding:10px 20px;border-top:1px solid var(--border,rgba(255,255,255,0.06));flex-wrap:wrap;gap:8px}',
            '.lm-page-info{font-size:12px;color:var(--text-muted,#64748B)}',
            '.lm-page-btns{display:flex;align-items:center;gap:6px}',
            '.lm-page-num{font-size:12px;color:var(--text-secondary,#94A3B8);min-width:60px;text-align:center}',
            '.lm-btn{padding:5px 12px;font-size:12px;border:1px solid var(--border,rgba(255,255,255,0.1));border-radius:6px;background:none;color:var(--text-secondary,#94A3B8);cursor:pointer;font-family:inherit;transition:all .15s}',
            '.lm-btn:hover:not(:disabled){background:rgba(255,255,255,0.05);color:#F1F5F9}',
            '.lm-btn:disabled{opacity:.3;cursor:not-allowed}',
            '.lm-hl{background:rgba(212,168,67,0.25);border-radius:2px;padding:0 1px}',
            /* Responsive */
            '@media(max-width:768px){',
            '  .lm-dialog{width:100vw;max-width:100vw;max-height:100dvh;border-radius:12px 12px 0 0;margin-top:auto}',
            '  .lm-toolbar{flex-direction:column;gap:8px}',
            '  .lm-search-wrap{min-width:100%}',
            '  #lmSearch{font-size:16px!important;min-height:44px}',
            '  .lm-footer{flex-direction:column;text-align:center}',
            '  .lm-table th,.lm-table td{padding:5px 7px;font-size:11px}',
            '  .lm-title{font-size:13px}',
            '}'
        ].join('\n');
        document.head.appendChild(css);
    }

    // ==========================================
    // Public API
    // ==========================================
    function open(opts) {
        ensureModal();

        state.pwaCode = opts.pwaCode || '';
        state.collection = opts.collection || '';
        state.layerDisplayName = opts.layerDisplayName || opts.collection;
        state.startDate = opts.startDate || '';
        state.endDate = opts.endDate || '';
        state.page = 1;
        state.search = '';
        state.columns = [];
        state.data = [];

        document.getElementById('lmTitle').textContent =
            state.layerDisplayName + ' — สาขา ' + state.pwaCode;
        document.getElementById('lmSearch').value = '';

        document.body.style.overflow = 'hidden';
        modalEl.style.display = 'flex';

        // Focus search after animation
        setTimeout(function() {
            document.getElementById('lmSearch').focus();
        }, 300);

        loadData();
    }

    function close() {
        if (!modalEl) return;
        modalEl.style.display = 'none';
        document.body.style.overflow = '';
    }

    function nextPage() {
        if (state.page < state.totalPages) { state.page++; loadData(); }
    }
    function prevPage() {
        if (state.page > 1) { state.page--; loadData(); }
    }
    function firstPage() {
        if (state.page !== 1) { state.page = 1; loadData(); }
    }
    function lastPage() {
        if (state.page !== state.totalPages) { state.page = state.totalPages; loadData(); }
    }

    // ==========================================
    // Data Loading
    // ==========================================
    function loadData() {
        setLoading(true);

        var url = '/pwa_gis_tracking/api/features/list?' +
            'pwaCode=' + enc(state.pwaCode) +
            '&collection=' + enc(state.collection) +
            '&page=' + state.page +
            '&pageSize=' + state.pageSize;

        if (state.startDate) url += '&startDate=' + enc(state.startDate);
        if (state.endDate) url += '&endDate=' + enc(state.endDate);
        if (state.search) url += '&search=' + enc(state.search);

        fetch(url)
            .then(function(res) {
                if (!res.ok) throw new Error('HTTP ' + res.status);
                return res.json();
            })
            .then(function(resp) {
                if (resp.status !== 'success') {
                    showError(resp.error || 'Unknown error');
                    return;
                }

                state.data = resp.data || [];
                state.columns = resp.columns || [];
                state.total = resp.total || 0;
                state.totalPages = resp.total_pages || 1;
                state.page = resp.page || 1;

                render();
                setLoading(false);
            })
            .catch(function(err) {
                showError('โหลดข้อมูลล้มเหลว: ' + err.message);
            });
    }

    // ==========================================
    // Rendering
    // ==========================================
    function render() {
        var thead = document.getElementById('lmThead');
        var tbody = document.getElementById('lmTbody');

        if (state.data.length === 0) {
            document.getElementById('lmTableWrap').style.display = 'none';
            var emptyEl = document.getElementById('lmEmpty');
            emptyEl.style.display = 'flex';
            emptyEl.innerHTML = '<div style="font-size:36px;margin-bottom:8px;">📭</div><div>ไม่พบข้อมูล</div>';
            updateFooter();
            return;
        }

        document.getElementById('lmTableWrap').style.display = 'flex';
        document.getElementById('lmEmpty').style.display = 'none';

        // Columns: use API columns, filter out hidden ones
        var cols = state.columns.filter(function(c) {
            return c.key !== 'password' && c.key !== '_doc_id';
        });

        // Fallback: auto-detect from first data row
        if (cols.length === 0) {
            cols = Object.keys(state.data[0])
                .filter(function(k) { return k !== '_doc_id' && k !== 'password'; })
                .map(function(k) { return { key: k, mongo_key: k }; });
        }

        // Header
        var hh = '<tr><th style="width:40px;text-align:center">#</th>';
        for (var i = 0; i < cols.length; i++) {
            hh += '<th>' + esc(cols[i].key) + '</th>';
        }
        hh += '</tr>';
        thead.innerHTML = hh;

        // Rows
        var startIdx = (state.page - 1) * state.pageSize;
        var rows = '';
        for (var r = 0; r < state.data.length; r++) {
            var row = state.data[r];
            rows += '<tr><td style="text-align:center;color:#64748B;font-size:11px">' + (startIdx + r + 1) + '</td>';
            for (var ci = 0; ci < cols.length; ci++) {
                var val = row[cols[ci].key];
                if (val === null || val === undefined || val === '') {
                    rows += '<td class="empty">—</td>';
                } else {
                    var str = fmtCell(val);
                    if (state.search) str = highlight(str, state.search);
                    var isNum = typeof val === 'number';
                    rows += '<td' + (isNum ? ' class="num"' : '') + ' title="' + esc(String(val)) + '">' + str + '</td>';
                }
            }
            rows += '</tr>';
        }
        tbody.innerHTML = rows;

        updateFooter();
    }

    function updateFooter() {
        document.getElementById('lmBadge').textContent = fmtNum(state.total) + ' records';

        var from = state.total === 0 ? 0 : (state.page - 1) * state.pageSize + 1;
        var to = Math.min(state.page * state.pageSize, state.total);
        document.getElementById('lmPageInfo').textContent =
            'แสดง ' + fmtNum(from) + ' – ' + fmtNum(to) + ' จาก ' + fmtNum(state.total) + ' records';

        var tp = Math.max(1, state.totalPages);
        document.getElementById('lmPageNum').textContent = state.page + ' / ' + tp;

        document.getElementById('lmFirst').disabled = state.page <= 1;
        document.getElementById('lmPrev').disabled = state.page <= 1;
        document.getElementById('lmNext').disabled = state.page >= state.totalPages;
        document.getElementById('lmLast').disabled = state.page >= state.totalPages;

        var info = state.collection;
        if (state.search) info += ' — ค้นหา "' + state.search + '"';
        document.getElementById('lmInfo').textContent = info;
    }

    function setLoading(show) {
        state.loading = show;
        document.getElementById('lmLoading').style.display = show ? 'flex' : 'none';
        if (show) {
            document.getElementById('lmTableWrap').style.display = 'none';
            document.getElementById('lmEmpty').style.display = 'none';
        }
    }

    function showError(msg) {
        setLoading(false);
        document.getElementById('lmTableWrap').style.display = 'none';
        var emptyEl = document.getElementById('lmEmpty');
        emptyEl.style.display = 'flex';
        emptyEl.innerHTML =
            '<div style="font-size:36px;margin-bottom:8px;">⚠️</div>' +
            '<div style="color:#EF4444;">' + esc(msg) + '</div>';
    }

    // ==========================================
    // Utility
    // ==========================================
    function fmtCell(val) {
        if (typeof val === 'string') {
            if (/^\d{4}-\d{2}-\d{2}T/.test(val)) return val.substring(0, 10);
            return esc(val);
        }
        if (typeof val === 'number') {
            if (Number.isInteger(val)) return val.toLocaleString('th-TH');
            return val.toLocaleString('th-TH', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
        }
        if (typeof val === 'boolean') return val ? 'Yes' : 'No';
        return esc(String(val));
    }

    function highlight(text, term) {
        if (!term) return text;
        var re = new RegExp('(' + term.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + ')', 'gi');
        return text.replace(re, '<span class="lm-hl">$1</span>');
    }

    function esc(s) {
        var d = document.createElement('div');
        d.appendChild(document.createTextNode(s));
        return d.innerHTML;
    }
    function enc(s) { return encodeURIComponent(s); }
    function fmtNum(n) {
        if (n === null || n === undefined) return '0';
        return Number(n).toLocaleString('th-TH');
    }

    // ==========================================
    // Expose
    // ==========================================
    return {
        open: open,
        close: close,
        nextPage: nextPage,
        prevPage: prevPage,
        firstPage: firstPage,
        lastPage: lastPage
    };

})();