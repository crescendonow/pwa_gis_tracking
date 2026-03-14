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
        loading: false,
        sortField: '',
        sortOrder: '',
        sortSpec: null,
        suggestTimer: null,
        suggestIdx: -1,
        facets: [],
        facetFilters: {}
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
                            'style="position:absolute;left:10px;top:50%;transform:translateY(-50%);color:#757575;pointer-events:none;">' +
                            '<circle cx="11" cy="11" r="8"/><path d="m21 21-4.3-4.3"/></svg>' +
                        '<input type="text" id="lmSearch" placeholder="ค้นหา... (เช่น HDPE 100, ชื่อลูกค้า)" autocomplete="off">' +
                        '<div class="lm-suggest" id="lmSuggest" style="display:none;"></div>' +
                    '</div>' +
                    '<div class="lm-toolbar-info" id="lmInfo"></div>' +
                '</div>' +
                /* Active filters bar */
                '<div class="lm-active-filters" id="lmActiveFilters" style="display:none;"></div>' +
                /* Facet panel (collapsible) */
                '<div class="lm-facet-bar" id="lmFacetBar" style="display:none;"></div>' +
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

        // Search with debounce + autocomplete
        var searchInput = document.getElementById('lmSearch');
        searchInput.addEventListener('input', function() {
            clearTimeout(state.searchTimer);
            state.searchTimer = setTimeout(function() {
                state.search = searchInput.value.trim();
                state.page = 1;
                loadData();
            }, 400);
            // Autocomplete suggestions (faster debounce)
            clearTimeout(state.suggestTimer);
            var q = searchInput.value.trim();
            if (q.length >= 2) {
                state.suggestTimer = setTimeout(function() { _fetchSuggestions(q); }, 250);
            } else {
                _hideSuggestions();
            }
        });
        // Keyboard navigation for suggestions + Enter = immediate search
        searchInput.addEventListener('keydown', function(e) {
            var suggestEl = document.getElementById('lmSuggest');
            var items = suggestEl ? suggestEl.querySelectorAll('.lm-suggest-item') : [];

            if (e.key === 'ArrowDown' && items.length > 0) {
                e.preventDefault();
                state.suggestIdx = Math.min((state.suggestIdx || -1) + 1, items.length - 1);
                _highlightSuggestion(items);
            } else if (e.key === 'ArrowUp' && items.length > 0) {
                e.preventDefault();
                state.suggestIdx = Math.max((state.suggestIdx || 0) - 1, 0);
                _highlightSuggestion(items);
            } else if (e.key === 'Enter') {
                if (state.suggestIdx >= 0 && items.length > 0 && items[state.suggestIdx]) {
                    e.preventDefault();
                    _selectSuggestion(items[state.suggestIdx]);
                } else {
                    clearTimeout(state.searchTimer);
                    _hideSuggestions();
                    state.search = searchInput.value.trim();
                    state.page = 1;
                    loadData();
                }
            } else if (e.key === 'Escape') {
                _hideSuggestions();
            }
        });
        // Hide suggestions on blur (with delay for click)
        searchInput.addEventListener('blur', function() {
            setTimeout(_hideSuggestions, 200);
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
            '.lm-dialog{background:var(--surface-1,#FFFFFF);border:1px solid var(--border,rgba(0,0,0,0.08));border-radius:16px;width:96vw;max-width:1300px;max-height:92vh;display:flex;flex-direction:column;box-shadow:0 24px 64px rgba(0,0,0,0.12);animation:lmUp .25s ease}',
            '@keyframes lmUp{from{transform:translateY(20px);opacity:0}to{transform:translateY(0);opacity:1}}',
            /* Header */
            '.lm-header{display:flex;align-items:center;justify-content:space-between;padding:14px 20px;border-bottom:1px solid var(--border,rgba(0,0,0,0.08))}',
            '.lm-header-left{display:flex;align-items:center;gap:10px;min-width:0}',
            '.lm-title{font-size:15px;font-weight:700;color:var(--text-primary,#1A1A1A);white-space:nowrap;overflow:hidden;text-overflow:ellipsis}',
            '.lm-badge{font-size:11px;padding:3px 10px;border-radius:12px;background:rgba(62,186,150,0.12);color:#15866B;font-weight:600;white-space:nowrap}',
            '.lm-close{background:none;border:none;color:var(--text-muted,#757575);font-size:24px;cursor:pointer;padding:4px 8px;border-radius:8px;line-height:1;transition:all .15s}',
            '.lm-close:hover{color:#2E2E2E;background:rgba(0,0,0,0.04)}',
            /* Toolbar */
            '.lm-toolbar{display:flex;align-items:center;gap:12px;padding:10px 20px;border-bottom:1px solid var(--border,rgba(0,0,0,0.06));flex-wrap:wrap}',
            '.lm-search-wrap{position:relative;flex:1;min-width:200px}',
            '#lmSearch{width:100%;padding:8px 12px 8px 34px;background:var(--surface-2,#F6F6F6);border:1px solid var(--border,rgba(0,0,0,0.08));border-radius:8px;color:var(--text-primary,#1A1A1A);font-size:13px;outline:none;font-family:inherit;transition:border-color .2s}',
            '#lmSearch:focus{border-color:var(--pwa-blue-light,#78AFE2);box-shadow:0 0 0 2px rgba(63,116,202,0.1)}',
            '.lm-toolbar-info{font-size:12px;color:var(--text-muted,#757575)}',
            /* Body */
            '.lm-body{flex:1;overflow:hidden;min-height:200px;position:relative;display:flex;flex-direction:column}',
            '.lm-table-wrap{flex:1;overflow:auto}',
            '.lm-table{width:100%;border-collapse:collapse;font-size:12px}',
            '.lm-table thead{position:sticky;top:0;z-index:2}',
            '.lm-table th{background:var(--surface-2,#F6F6F6);color:var(--text-secondary,#4A4A4A);padding:8px 10px;text-align:left;font-weight:700;font-size:11px;white-space:nowrap;border-bottom:2px solid var(--pwa-blue,#3F74CA);letter-spacing:.3px;position:relative;user-select:none}',
            '.lm-resize-handle{position:absolute;right:0;top:0;bottom:0;width:5px;cursor:col-resize;z-index:3}',
            '.lm-resize-handle:hover,.lm-resize-handle.active{background:var(--pwa-blue,#3F74CA)}',
            '.lm-table td{padding:6px 10px;border-bottom:1px solid var(--border,rgba(0,0,0,0.06));color:var(--text-primary,#1A1A1A);white-space:nowrap;max-width:280px;overflow:hidden;text-overflow:ellipsis}',
            '.lm-table tr:hover td{background:rgba(63,116,202,0.04)}',
            '.lm-table td.num{text-align:right;font-variant-numeric:tabular-nums}',
            '.lm-table td.empty{color:var(--text-muted,#757575);font-style:italic}',
            /* Loading */
            '.lm-loading{display:flex;flex-direction:column;align-items:center;justify-content:center;gap:12px;padding:60px 20px;color:var(--text-muted,#757575);font-size:13px}',
            '.lm-spinner{width:32px;height:32px;border:3px solid var(--border,rgba(0,0,0,0.08));border-top-color:#3EBA96;border-radius:50%;animation:lmSpin .6s linear infinite}',
            '@keyframes lmSpin{to{transform:rotate(360deg)}}',
            '.lm-empty{display:flex;flex-direction:column;align-items:center;justify-content:center;padding:60px 20px;color:var(--text-muted,#757575);font-size:14px}',
            /* Footer */
            '.lm-footer{display:flex;align-items:center;justify-content:space-between;padding:10px 20px;border-top:1px solid var(--border,rgba(0,0,0,0.08));flex-wrap:wrap;gap:8px}',
            '.lm-page-info{font-size:12px;color:var(--text-muted,#757575)}',
            '.lm-page-btns{display:flex;align-items:center;gap:6px}',
            '.lm-page-num{font-size:12px;color:var(--text-secondary,#4A4A4A);min-width:60px;text-align:center}',
            '.lm-btn{padding:5px 12px;font-size:12px;border:1px solid var(--border,rgba(0,0,0,0.08));border-radius:6px;background:none;color:var(--text-secondary,#4A4A4A);cursor:pointer;font-family:inherit;transition:all .15s}',
            '.lm-btn:hover:not(:disabled){background:rgba(0,0,0,0.03);color:#2E2E2E}',
            '.lm-btn:disabled{opacity:.3;cursor:not-allowed}',
            '.lm-hl{background:rgba(62,186,150,0.2);border-radius:2px;padding:0 1px}',
            /* Faceted filtering */
            '.lm-facet-bar{display:flex;gap:12px;padding:8px 20px;border-bottom:1px solid var(--border,rgba(0,0,0,0.06));overflow-x:auto;flex-wrap:wrap;max-height:120px;overflow-y:auto}',
            '.lm-facet-group{display:flex;align-items:center;gap:4px;flex-shrink:0;flex-wrap:wrap}',
            '.lm-facet-label{font-size:10px;font-weight:700;color:var(--text-muted,#757575);text-transform:uppercase;letter-spacing:.3px;margin-right:2px;white-space:nowrap}',
            '.lm-facet-chip{display:inline-flex;align-items:center;gap:3px;padding:3px 8px;font-size:11px;border:1px solid var(--border,rgba(0,0,0,0.1));border-radius:12px;cursor:pointer;color:var(--text-secondary,#4A4A4A);background:var(--surface-1,#fff);transition:all .15s;white-space:nowrap}',
            '.lm-facet-chip:hover{border-color:var(--pwa-blue-light,#78AFE2);background:rgba(63,116,202,0.04)}',
            '.lm-facet-chip.active{background:var(--pwa-blue,#3F74CA);color:#fff;border-color:var(--pwa-blue,#3F74CA)}',
            '.lm-facet-chip.active .lm-facet-cnt{background:rgba(255,255,255,0.25);color:#fff}',
            '.lm-facet-cnt{font-size:9px;padding:1px 5px;border-radius:8px;background:rgba(0,0,0,0.06);color:var(--text-muted,#999);font-weight:600}',
            /* Active filter tags */
            '.lm-active-filters{display:flex;align-items:center;gap:6px;padding:6px 20px;border-bottom:1px solid var(--border,rgba(0,0,0,0.06));flex-wrap:wrap;background:rgba(63,116,202,0.03)}',
            '.lm-af-label{font-size:11px;font-weight:700;color:var(--text-muted,#757575)}',
            '.lm-af-tag{display:inline-flex;align-items:center;gap:4px;padding:3px 8px;font-size:11px;background:var(--pwa-blue,#3F74CA);color:#fff;border-radius:12px;cursor:pointer;transition:opacity .15s}',
            '.lm-af-tag:hover{opacity:0.8}',
            '.lm-af-x{font-size:13px;font-weight:700;margin-left:2px;line-height:1}',
            '.lm-af-clear{font-size:11px;color:var(--pwa-blue,#3F74CA);cursor:pointer;text-decoration:underline;margin-left:4px}',
            '.lm-af-clear:hover{color:#2557A0}',
            /* Sortable column headers */
            '.lm-sortable{cursor:pointer;user-select:none;transition:background .15s}',
            '.lm-sortable:hover{background:rgba(63,116,202,0.08)}',
            '.lm-sort-arrow{margin-left:3px;font-size:10px;color:var(--pwa-blue,#3F74CA)}',
            /* Autocomplete suggestions */
            '.lm-suggest{position:absolute;top:100%;left:0;right:0;background:var(--surface-1,#fff);border:1px solid var(--border,rgba(0,0,0,0.12));border-top:none;border-radius:0 0 8px 8px;box-shadow:0 8px 24px rgba(0,0,0,0.12);z-index:10;max-height:280px;overflow-y:auto}',
            '.lm-suggest-item{display:flex;align-items:center;justify-content:space-between;padding:8px 12px;cursor:pointer;gap:8px;transition:background .1s}',
            '.lm-suggest-item:hover,.lm-suggest-item.active{background:rgba(63,116,202,0.08)}',
            '.lm-suggest-val{font-size:12px;color:var(--text-primary,#1A1A1A);white-space:nowrap;overflow:hidden;text-overflow:ellipsis;min-width:0}',
            '.lm-suggest-val strong{color:var(--pwa-blue,#3F74CA)}',
            '.lm-suggest-field{font-size:10px;color:var(--text-muted,#999);white-space:nowrap;flex-shrink:0;background:rgba(0,0,0,0.04);padding:2px 6px;border-radius:4px}',
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
        state.sortField = '';
        state.sortOrder = '';
        state.sortSpec = null;
        state.facets = [];
        state.facetFilters = {};

        document.getElementById('lmTitle').textContent =
            state.layerDisplayName + ' — สาขา ' + state.pwaCode;
        document.getElementById('lmSearch').value = '';
        var afEl = document.getElementById('lmActiveFilters');
        if (afEl) { afEl.style.display = 'none'; afEl.innerHTML = ''; }

        document.body.style.overflow = 'hidden';
        modalEl.style.display = 'flex';

        // Focus search after animation
        setTimeout(function() {
            document.getElementById('lmSearch').focus();
        }, 300);

        loadData();
        _fetchFacets();
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
            '&pageSize=' + state.pageSize +
            '&raw=1';

        if (state.startDate) url += '&startDate=' + enc(state.startDate);
        if (state.endDate) url += '&endDate=' + enc(state.endDate);

        // Sort parameters
        if (state.sortField) {
            url += '&sortBy=' + enc(state.sortField);
            url += '&sortOrder=' + (state.sortOrder === 'asc' ? 'asc' : 'desc');
        }

        // Build combined filters: facet selections + search-parsed filters
        var allFilters = {};

        // Facet filters (exact match)
        for (var ff in state.facetFilters) {
            if (state.facetFilters.hasOwnProperty(ff)) {
                allFilters[ff] = state.facetFilters[ff];
            }
        }

        // Search bar: parse structured filters + free text
        if (state.search) {
            var parsed = parseSearchFilters(state.search, state.collection);
            for (var pf in parsed.filters) {
                if (parsed.filters.hasOwnProperty(pf) && !allFilters[pf]) {
                    allFilters[pf] = parsed.filters[pf];
                }
            }
            if (parsed.freeText) {
                url += '&search=' + enc(parsed.freeText);
            }
        }

        if (Object.keys(allFilters).length > 0) {
            url += '&filters=' + enc(JSON.stringify(allFilters));
        }

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

        // Header — sortable columns
        if (!state.sortSpec) state.sortSpec = getSortableColumns(state.collection);
        var sortableCols = state.sortSpec.columns;

        var hh = '<tr><th style="width:40px;text-align:center">#</th>';
        for (var i = 0; i < cols.length; i++) {
            var colKey = cols[i].key;
            var isSortable = sortableCols.indexOf(colKey) !== -1;
            if (isSortable) {
                var arrow = '';
                if (state.sortField === colKey) {
                    arrow = state.sortOrder === 'asc' ? ' \u25B2' : ' \u25BC';
                }
                hh += '<th class="lm-sortable" data-field="' + esc(colKey) + '">' +
                      esc(colKey) + '<span class="lm-sort-arrow">' + arrow + '</span>' +
                      '<div class="lm-resize-handle"></div></th>';
            } else {
                hh += '<th>' + esc(colKey) + '<div class="lm-resize-handle"></div></th>';
            }
        }
        hh += '</tr>';
        thead.innerHTML = hh;

        // Attach sort click listeners
        var sortHeaders = thead.querySelectorAll('.lm-sortable');
        for (var si = 0; si < sortHeaders.length; si++) {
            sortHeaders[si].addEventListener('click', _onSortClick);
        }

        // Attach column resize handlers
        var resizeHandles = thead.querySelectorAll('.lm-resize-handle');
        for (var ri = 0; ri < resizeHandles.length; ri++) {
            resizeHandles[ri].addEventListener('mousedown', _onResizeStart);
        }

        // Rows
        var startIdx = (state.page - 1) * state.pageSize;
        var rows = '';
        for (var r = 0; r < state.data.length; r++) {
            var row = state.data[r];
            rows += '<tr><td style="text-align:center;color:#757575;font-size:11px">' + (startIdx + r + 1) + '</td>';
            for (var ci = 0; ci < cols.length; ci++) {
                var val = row[cols[ci].key];
                // Force ID columns to text so they display/filter correctly
                if (val !== null && val !== undefined && FORCE_TEXT_COLUMNS.indexOf(cols[ci].key) !== -1) {
                    val = String(val);
                }
                if (val === null || val === undefined || val === '') {
                    rows += '<td class="empty">—</td>';
                } else {
                    var str = fmtCell(val);
                    if (state.search) str = highlight(str, state.search);
                    var isNum = typeof val === 'number' && FORCE_TEXT_COLUMNS.indexOf(cols[ci].key) === -1;
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
    // ID columns that should be treated as text (not formatted as numbers)
    var FORCE_TEXT_COLUMNS = [
        'PIPE_ID', 'VALVE_ID', 'FIRE_ID', 'LEAK_ID', 'STRUCT_ID', 'BLDG_ID',
        'globalId', 'custCode', 'meterNo', 'houseCode'
    ];

    // Per-layer filterable columns from 12_filter_data_table.md
    // ::int suffix = numeric comparison support (>=, <=, >, <)
    var SORTABLE_COLUMNS = {
        pipe: ['PIPE_ID','class','classId','depth','functionId','gradeID','layingId',
               'length','locate','pipeIdPrev','productId','projectName','projectNo',
               'promiseDate','pwaCode','recordDate','remark','sizeId','typeId','yearInstall::int'],
        valve: ['VALVE_ID','depth','functionId','pwaCode','recordDate','roundOpen::int',
                'sizeId','statusId','typeId','yearInstall::int'],
        firehydrant: ['FIRE_ID','pressure::int','pwaCode','recordDate','sizeId','statusId'],
        meter: ['BLDG_ID','PIPE_ID','addressNo','averageWaterUsage::int','beginCustDate',
                'custAddress','custCode','custFullName','custStat','meterNo','meterRouteCode',
                'meterRouteSeq','meterSizeCode','meterSizeName','pipeId','presentWaterUsage::int',
                'pwaCode','recordDate','remark','validate'],
        bldg: ['BLDG_ID','addressNo','buildingTypeId','custCode','custCodeOld','custFullName',
               'district','floor','houseCode','province','pwaCode','recordDate','road','soi',
               'subDistrict','useStatusId','useTypeId','village','villageNo','zipcode'],
        leakpoint: ['LEAK_ID','PIPE_ID','cause','checker','depth','detail','informer',
                    'leakDatetime','leakNo','locate','pipeSizeId','pipeTypeId','pwaCode',
                    'recordDate','repairBy','repairCost','repairDatetime','typeDescription','typeId'],
        pwa_waterworks: ['costCenterId','depShortName','name','pwaAddress','pwaCode','pwaStationId'],
        struct: ['pwaCode'],
        pipe_serv: ['custCode']
    };

    function getSortableColumns(collection) {
        var specs = SORTABLE_COLUMNS[collection] || [];
        var result = { columns: [], intColumns: {} };
        for (var i = 0; i < specs.length; i++) {
            var spec = specs[i];
            if (spec.indexOf('::int') !== -1) {
                var col = spec.replace('::int', '');
                result.columns.push(col);
                result.intColumns[col] = true;
            } else {
                result.columns.push(spec);
            }
        }
        return result;
    }

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
    // Autocomplete Suggestions
    // ==========================================
    function _fetchSuggestions(q) {
        var url = '/pwa_gis_tracking/api/features/suggest?' +
            'pwaCode=' + enc(state.pwaCode) +
            '&collection=' + enc(state.collection) +
            '&q=' + enc(q) +
            '&limit=8';

        fetch(url)
            .then(function(res) { return res.json(); })
            .then(function(data) {
                var items = data.suggestions || [];
                if (items.length === 0) { _hideSuggestions(); return; }
                _showSuggestions(items, q);
            })
            .catch(function() { _hideSuggestions(); });
    }

    function _showSuggestions(items, query) {
        var el = document.getElementById('lmSuggest');
        if (!el) return;
        state.suggestIdx = -1;

        var html = '';
        for (var i = 0; i < items.length; i++) {
            var item = items[i];
            var displayVal = _highlightMatch(esc(item.value), query);
            html += '<div class="lm-suggest-item" data-value="' + esc(item.value) + '" data-field="' + esc(item.field) + '">' +
                    '<span class="lm-suggest-val">' + displayVal + '</span>' +
                    '<span class="lm-suggest-field">' + esc(item.field) + '</span>' +
                    '</div>';
        }
        el.innerHTML = html;
        el.style.display = 'block';

        // Attach click handlers
        var divs = el.querySelectorAll('.lm-suggest-item');
        for (var j = 0; j < divs.length; j++) {
            divs[j].addEventListener('mousedown', function(e) {
                e.preventDefault();
                _selectSuggestion(this);
            });
        }
    }

    function _hideSuggestions() {
        var el = document.getElementById('lmSuggest');
        if (el) { el.style.display = 'none'; el.innerHTML = ''; }
        state.suggestIdx = -1;
    }

    function _selectSuggestion(itemEl) {
        var value = itemEl.getAttribute('data-value');
        var searchInput = document.getElementById('lmSearch');
        if (searchInput) searchInput.value = value;
        _hideSuggestions();
        clearTimeout(state.searchTimer);
        state.search = value;
        state.page = 1;
        loadData();
    }

    function _highlightSuggestion(items) {
        for (var i = 0; i < items.length; i++) {
            items[i].classList.toggle('active', i === state.suggestIdx);
        }
    }

    function _highlightMatch(text, query) {
        if (!query) return text;
        var re = new RegExp('(' + query.replace(/[.*+?^${}()|[\]\\]/g, '\\$&') + ')', 'gi');
        return text.replace(re, '<strong>$1</strong>');
    }

    // ==========================================
    // Faceted Filtering
    // ==========================================
    function _fetchFacets() {
        var url = '/pwa_gis_tracking/api/features/facets?' +
            'pwaCode=' + enc(state.pwaCode) +
            '&collection=' + enc(state.collection);

        fetch(url)
            .then(function(res) { return res.json(); })
            .then(function(data) {
                state.facets = data.facets || [];
                _renderFacets();
            })
            .catch(function() { state.facets = []; });
    }

    function _renderFacets() {
        var bar = document.getElementById('lmFacetBar');
        if (!bar || state.facets.length === 0) {
            if (bar) bar.style.display = 'none';
            return;
        }

        var html = '';
        for (var i = 0; i < state.facets.length; i++) {
            var facet = state.facets[i];
            var isActive = state.facetFilters.hasOwnProperty(facet.field);
            html += '<div class="lm-facet-group">' +
                    '<span class="lm-facet-label">' + esc(facet.field) + '</span>';
            for (var j = 0; j < facet.values.length; j++) {
                var fv = facet.values[j];
                var selected = isActive && state.facetFilters[facet.field] === fv.value;
                html += '<span class="lm-facet-chip' + (selected ? ' active' : '') + '" ' +
                        'data-field="' + esc(facet.field) + '" data-value="' + esc(fv.value) + '">' +
                        esc(fv.value) + '<span class="lm-facet-cnt">' + fv.count + '</span></span>';
            }
            html += '</div>';
        }
        bar.innerHTML = html;
        bar.style.display = 'flex';

        // Attach click handlers
        var chips = bar.querySelectorAll('.lm-facet-chip');
        for (var k = 0; k < chips.length; k++) {
            chips[k].addEventListener('click', _onFacetClick);
        }
    }

    function _onFacetClick(e) {
        var chip = e.currentTarget;
        var field = chip.getAttribute('data-field');
        var value = chip.getAttribute('data-value');

        if (state.facetFilters[field] === value) {
            // Deselect
            delete state.facetFilters[field];
        } else {
            // Select
            state.facetFilters[field] = value;
        }
        state.page = 1;
        _renderFacets();
        _renderActiveFilters();
        loadData();
    }

    function _renderActiveFilters() {
        var el = document.getElementById('lmActiveFilters');
        if (!el) return;

        var keys = Object.keys(state.facetFilters);
        if (keys.length === 0) {
            el.style.display = 'none';
            el.innerHTML = '';
            return;
        }

        var html = '<span class="lm-af-label">Filter:</span>';
        for (var i = 0; i < keys.length; i++) {
            var f = keys[i];
            var v = state.facetFilters[f];
            html += '<span class="lm-af-tag" data-field="' + esc(f) + '">' +
                    esc(f) + ' = ' + esc(v) +
                    '<span class="lm-af-x">&times;</span></span>';
        }
        html += '<span class="lm-af-clear">ล้างทั้งหมด</span>';
        el.innerHTML = html;
        el.style.display = 'flex';

        // Remove single filter
        var tags = el.querySelectorAll('.lm-af-tag');
        for (var j = 0; j < tags.length; j++) {
            tags[j].addEventListener('click', function() {
                var ff = this.getAttribute('data-field');
                delete state.facetFilters[ff];
                state.page = 1;
                _renderFacets();
                _renderActiveFilters();
                loadData();
            });
        }
        // Clear all
        var clearBtn = el.querySelector('.lm-af-clear');
        if (clearBtn) {
            clearBtn.addEventListener('click', function() {
                state.facetFilters = {};
                state.page = 1;
                _renderFacets();
                _renderActiveFilters();
                loadData();
            });
        }
    }

    // ==========================================
    // Sort Helpers
    // ==========================================
    function _onResizeStart(e) {
        e.preventDefault();
        e.stopPropagation();
        var handle = e.currentTarget;
        var th = handle.parentElement;
        var startX = e.clientX;
        var startW = th.offsetWidth;
        handle.classList.add('active');

        function onMove(ev) {
            var newW = Math.max(40, startW + (ev.clientX - startX));
            th.style.width = newW + 'px';
            th.style.minWidth = newW + 'px';
        }
        function onUp() {
            handle.classList.remove('active');
            document.removeEventListener('mousemove', onMove);
            document.removeEventListener('mouseup', onUp);
        }
        document.addEventListener('mousemove', onMove);
        document.addEventListener('mouseup', onUp);
    }

    function _onSortClick(e) {
        // Ignore clicks from resize handle
        if (e.target.classList.contains('lm-resize-handle')) return;
        var th = e.currentTarget;
        var field = th.getAttribute('data-field');
        if (state.sortField === field) {
            // Toggle: asc → desc → clear
            if (state.sortOrder === 'asc') {
                state.sortOrder = 'desc';
            } else {
                state.sortField = '';
                state.sortOrder = '';
            }
        } else {
            state.sortField = field;
            state.sortOrder = 'asc';
        }
        state.page = 1;
        loadData();
    }

    // ==========================================
    // Smart Search Filter Parser
    // ==========================================
    var THAI_MONTHS = {
        'ม.ค.': '01', 'มค': '01', 'มกราคม': '01',
        'ก.พ.': '02', 'กพ': '02', 'กุมภาพันธ์': '02',
        'มี.ค.': '03', 'มีค': '03', 'มีนาคม': '03',
        'เม.ย.': '04', 'เมย': '04', 'เมษายน': '04',
        'พ.ค.': '05', 'พค': '05', 'พฤษภาคม': '05',
        'มิ.ย.': '06', 'มิย': '06', 'มิถุนายน': '06',
        'ก.ค.': '07', 'กค': '07', 'กรกฎาคม': '07',
        'ส.ค.': '08', 'สค': '08', 'สิงหาคม': '08',
        'ก.ย.': '09', 'กย': '09', 'กันยายน': '09',
        'ต.ค.': '10', 'ตค': '10', 'ตุลาคม': '10',
        'พ.ย.': '11', 'พย': '11', 'พฤศจิกายน': '11',
        'ธ.ค.': '12', 'ธค': '12', 'ธันวาคม': '12'
    };
    var SORTED_MONTH_KW = Object.keys(THAI_MONTHS).sort(function(a, b) { return b.length - a.length; });

    // Column keywords per layer (Thai keyword → MongoDB field name)
    var FILTER_KEYWORDS = {
        meter: {
            'สถานะมาตร': 'custStat', 'สถานะ': 'custStat',
            'ชื่อผู้ใช้น้ำ': 'custFullName', 'ชื่อลูกค้า': 'custFullName', 'ชื่อ': 'custFullName',
            'เลขมาตร': 'meterNo', 'ขนาดมาตร': 'meterSizeCode', 'ขนาด': 'meterSizeCode',
            'รหัสลูกค้า': 'custCode', 'ที่อยู่': 'addressNo',
            'วันที่ลงข้อมูล': 'recordDate', 'วันที่': 'recordDate',
            'วันเริ่มใช้น้ำ': 'beginCustDate'
        },
        pipe: {
            'ชนิดท่อ': 'typeId', 'ชนิด': 'typeId',
            'ขนาดท่อ': 'sizeId', 'ขนาด': 'sizeId',
            'หน้าที่': 'functionId', 'ฟังก์ชัน': 'functionId',
            'เกรด': 'gradeId', 'ชั้นท่อ': 'classId',
            'วันที่ลงข้อมูล': 'recordDate', 'วันที่': 'recordDate',
            'ปีติดตั้ง': 'yearInstall'
        },
        valve: {
            'ชนิด': 'typeId', 'ขนาด': 'sizeId', 'สถานะ': 'statusId',
            'วันที่': 'recordDate'
        },
        firehydrant: {
            'ขนาด': 'sizeId', 'สถานะ': 'statusId',
            'วันที่': 'recordDate'
        },
        leakpoint: {
            'สาเหตุ': 'cause', 'สถานะ': 'LeakStatus',
            'วันที่แจ้ง': 'leakDatetime', 'วันที่': 'leakDatetime',
            'ค่าซ่อม': 'repairCost', 'ชนิดท่อ': 'pipeTypeId'
        },
        bldg: {
            'สถานะ': 'useStatusId', 'ชื่อ': 'custFullName',
            'ที่อยู่': 'addressNo', 'วันที่': 'recordDate'
        }
    };

    function parseThaiDate(text) {
        // Match "21 ม.ค. 2569" / "21 มกราคม 2569"
        for (var i = 0; i < SORTED_MONTH_KW.length; i++) {
            var kw = SORTED_MONTH_KW[i];
            var re = new RegExp('(\\d{1,2})\\s*' + kw.replace(/\./g, '\\.') + '\\s*(\\d{4})');
            var m = text.match(re);
            if (m) {
                var day = m[1].padStart(2, '0');
                var month = THAI_MONTHS[kw];
                var year = parseInt(m[2]);
                if (year > 2400) year -= 543;
                return { date: year + '-' + month + '-' + day, matched: m[0] };
            }
        }
        return null;
    }

    // Pipe type abbreviations for smart detection
    var PIPE_TYPES_RE = /\b(PVC[_\-]?O|HDPE|PVC|AC|DI|CI|GS|ST|PB|GRP)\b/i;

    // Primary ID column per collection (for pure-numeric search)
    var PRIMARY_ID = {
        pipe: 'PIPE_ID', valve: 'VALVE_ID', firehydrant: 'FIRE_ID',
        meter: 'meterNo', bldg: 'BLDG_ID', leakpoint: 'LEAK_ID'
    };

    function parseSearchFilters(text, collection) {
        var keywords = FILTER_KEYWORDS[collection] || {};
        var sortedKw = Object.keys(keywords).sort(function(a, b) { return b.length - a.length; });
        var filters = {};
        var remaining = text;

        // ── Smart detection: pipe type abbreviation + optional size ──
        // e.g. "AC 100" → typeId=AC, sizeId=100
        if (collection === 'pipe') {
            var ptMatch = remaining.match(PIPE_TYPES_RE);
            if (ptMatch) {
                var typeVal = ptMatch[1].toUpperCase().replace('-', '_');
                if (typeVal === 'PVCO') typeVal = 'PVC_O';
                filters['typeId'] = typeVal;
                remaining = remaining.replace(ptMatch[0], '').trim();
                // Adjacent number → sizeId (2-4 digit range)
                var sizeAfter = remaining.match(/^\s*(\d{2,4})\b/);
                if (sizeAfter) {
                    filters['sizeId'] = sizeAfter[1];
                    remaining = remaining.replace(sizeAfter[0], '').trim();
                }
            }
        }

        // ── Smart detection: pure numeric (4+ digits) → primary ID column ──
        // e.g. "11542" on pipe → PIPE_ID=11542
        if (!Object.keys(filters).length && /^\d{4,}$/.test(remaining.trim())) {
            var idCol = PRIMARY_ID[collection];
            if (idCol) {
                filters[idCol] = remaining.trim();
                remaining = '';
            }
        }

        // Extract Thai date first
        var dateResult = parseThaiDate(remaining);
        if (dateResult) {
            // Find which date field keyword precedes the date
            var dateField = 'recordDate'; // default
            for (var di = 0; di < sortedKw.length; di++) {
                var dk = sortedKw[di];
                var field = keywords[dk];
                if ((field === 'recordDate' || field === 'leakDatetime' || field === 'beginCustDate') &&
                    remaining.indexOf(dk) !== -1 && remaining.indexOf(dk) < remaining.indexOf(dateResult.matched)) {
                    dateField = field;
                    remaining = remaining.replace(dk, '');
                    break;
                }
            }
            filters[dateField] = dateResult.date;
            remaining = remaining.replace(dateResult.matched, '');
        }

        // Extract "keyword เป็น value" / "keyword value" patterns
        for (var ki = 0; ki < sortedKw.length; ki++) {
            var kw = sortedKw[ki];
            var mongoField = keywords[kw];
            if (filters[mongoField]) continue; // already set by date

            // Pattern: "keyword เป็น value" or "keyword = value" or "keyword value"
            var re = new RegExp(kw + '\\s*(?:เป็น|คือ|=|:|)\\s*([^\\s,]+)');
            var match = remaining.match(re);
            if (match) {
                filters[mongoField] = match[1];
                remaining = remaining.replace(match[0], '');
            }
        }

        remaining = remaining.replace(/\s+/g, ' ').trim();
        return { filters: filters, freeText: remaining };
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