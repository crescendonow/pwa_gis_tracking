/**
 * AdvancedQuery — Advanced Query Builder with AG Grid results.
 * IIFE module following same pattern as LayerModal.
 *
 * Usage:
 *   AdvancedQuery.open({
 *     pwaCode: '5531022',
 *     collection: 'pipe',
 *     availableLayers: ['pipe','valve','firehydrant'],
 *     startDate: '', endDate: ''
 *   });
 */
/* global agGrid */
var AdvancedQuery = (function () {
  "use strict";

  // ═══════════════════════════════════════════════
  // State
  // ═══════════════════════════════════════════════
  var state = {
    pwaCode: "",
    pwaCodes: [],       // multiple branches
    collection: "",
    availableLayers: [],
    startDate: "",
    endDate: "",
    fieldMapping: null, // {mongoKey: pgKey, ...}
    reverseMapping: null, // {pgKey: mongoKey, ...}
    columns: [], // ColumnInfo from API
    conditions: null, // root ConditionGroup {logic, rules}
    results: [],
    total: 0,
    page: 1,
    pageSize: 50,
    totalPages: 0,
    limit: 5000,
    sortBy: "",
    sortOrder: "desc",
    gridApi: null,
    gridColumnApi: null,
    loading: false,
    suggestTimer: null,
    executed: false,
    viewMode: "table",  // "table" or "map"
    mapInstance: null,
  };

  var modalEl = null;
  var nextId = 1;
  var BASE = "/pwa_gis_tracking";

  // ═══════════════════════════════════════════════
  // Condition Model
  // ═══════════════════════════════════════════════
  function newRule() {
    return {
      id: nextId++,
      type: "rule",
      field: "",
      operator: "=",
      value: "",
      value2: "",
    };
  }
  function newGroup(logic) {
    return {
      id: nextId++,
      type: "group",
      logic: logic || "AND",
      rules: [newRule()],
    };
  }

  // Serialize conditions to JSON for API
  function serializeConditions(group) {
    if (!group) return null;
    var result = { logic: group.logic, rules: [] };
    for (var i = 0; i < group.rules.length; i++) {
      var r = group.rules[i];
      if (r.type === "group") {
        result.rules.push(serializeConditions(r));
      } else {
        if (!r.field) continue; // skip incomplete rules
        var rule = { field: r.field, operator: r.operator, value: r.value };
        if (r.operator === "between") {
          rule.value2 = r.value2;
        }
        result.rules.push(rule);
      }
    }
    return result;
  }

  // ═══════════════════════════════════════════════
  // Modal DOM
  // ═══════════════════════════════════════════════
  function ensureModal() {
    if (modalEl) return;

    modalEl = document.createElement("div");
    modalEl.id = "aqModal";
    modalEl.className = "aq-overlay";
    modalEl.style.display = "none";

    modalEl.innerHTML =
      '<div class="aq-dialog">' +
      // Header
      '  <div class="aq-header">' +
      '    <div class="aq-header-left">' +
      '      <span class="aq-title"><i class="fa-solid fa-filter"></i> การค้นหาขั้นสูง</span>' +
      '      <select id="aqCollectionSelect" class="aq-collection-select"></select>' +
      '      <span class="aq-badge" id="aqBadge">0</span>' +
      "    </div>" +
      '    <div class="aq-header-right">' +
      '      <div id="aqBranchTags" class="aq-branch-tags"></div>' +
      '      <select id="aqTemplateSelect" class="aq-template-select" title="โหลด Template">' +
      '        <option value="">-- Template --</option>' +
      "      </select>" +
      '      <button class="aq-btn aq-btn-sm" onclick="AdvancedQuery._saveTemplate()" title="บันทึก Template">' +
      '        <i class="fa-solid fa-floppy-disk"></i>' +
      "      </button>" +
      '      <button class="aq-close" onclick="AdvancedQuery.close()">&times;</button>' +
      "    </div>" +
      "  </div>" +
      // Builder section
      '  <div class="aq-builder">' +
      '    <div class="aq-conditions-panel" id="aqConditions"></div>' +
      '    <div class="aq-preview-panel">' +
      '      <div class="aq-preview-label">Query Preview</div>' +
      '      <div class="aq-preview-text" id="aqPreview">-</div>' +
      '      <div class="aq-preview-actions">' +
      '        <div class="aq-limit-row">' +
      '          <label>Limit:</label>' +
      '          <input type="number" id="aqLimit" value="5000" min="1" max="10000" class="aq-limit-input" />' +
      "        </div>" +
      '        <button class="aq-btn aq-btn-primary aq-btn-exec" onclick="AdvancedQuery._execute()">' +
      '          <i class="fa-solid fa-play"></i> ค้นหา' +
      "        </button>" +
      "      </div>" +
      "    </div>" +
      "  </div>" +
      // Results section
      '  <div class="aq-results" id="aqResultsSection" style="display:none">' +
      '    <div class="aq-grid-toolbar" id="aqGridToolbar">' +
      '      <span id="aqTotalInfo" class="aq-total-info"></span>' +
      '      <div class="aq-view-toggle">' +
      '        <button class="aq-btn aq-btn-xs aq-view-btn active" id="aqViewTable" onclick="AdvancedQuery._setView(\'table\')">' +
      '          <i class="fa-solid fa-table"></i> Table</button>' +
      '        <button class="aq-btn aq-btn-xs aq-view-btn" id="aqViewMap" onclick="AdvancedQuery._setView(\'map\')">' +
      '          <i class="fa-solid fa-map"></i> Map</button>' +
      "      </div>" +
      '      <div class="aq-export-btns">' +
      '        <span class="aq-export-label">Export:</span>' +
      '        <button class="aq-btn aq-btn-xs" onclick="AdvancedQuery._export(\'csv\')">CSV</button>' +
      '        <button class="aq-btn aq-btn-xs" onclick="AdvancedQuery._export(\'geojson\')">GeoJSON</button>' +
      '        <button class="aq-btn aq-btn-xs" onclick="AdvancedQuery._export(\'gpkg\')">GPKG</button>' +
      '        <button class="aq-btn aq-btn-xs" onclick="AdvancedQuery._export(\'shp\')">SHP</button>' +
      '        <button class="aq-btn aq-btn-xs" onclick="AdvancedQuery._export(\'fgb\')">FGB</button>' +
      '        <button class="aq-btn aq-btn-xs" onclick="AdvancedQuery._export(\'pmtiles\')">PMTiles</button>' +
      "      </div>" +
      "    </div>" +
      '    <div id="aqGrid" class="aq-grid ag-theme-alpine-dark"></div>' +
      '    <div id="aqMapContainer" class="aq-map-container" style="display:none"></div>' +
      '    <div class="aq-footer">' +
      '      <span id="aqPageInfo" class="aq-page-info"></span>' +
      '      <div class="aq-page-btns">' +
      '        <button class="aq-btn aq-btn-sm" onclick="AdvancedQuery._firstPage()">&#171;</button>' +
      '        <button class="aq-btn aq-btn-sm" onclick="AdvancedQuery._prevPage()">&#8249;</button>' +
      '        <span id="aqPageNum" class="aq-page-num">1</span>' +
      '        <button class="aq-btn aq-btn-sm" onclick="AdvancedQuery._nextPage()">&#8250;</button>' +
      '        <button class="aq-btn aq-btn-sm" onclick="AdvancedQuery._lastPage()">&#187;</button>' +
      "      </div>" +
      "    </div>" +
      "  </div>" +
      // Loading overlay
      '  <div id="aqLoading" class="aq-loading" style="display:none">' +
      '    <div class="aq-spinner"></div>' +
      "  </div>" +
      "</div>";

    document.body.appendChild(modalEl);

    // Close on overlay click
    modalEl.addEventListener("click", function (e) {
      if (e.target === modalEl) close();
    });
    // Close on Escape
    document.addEventListener("keydown", function (e) {
      if (e.key === "Escape" && modalEl && modalEl.style.display !== "none") {
        close();
      }
    });

    // Collection change
    var colSelect = document.getElementById("aqCollectionSelect");
    colSelect.addEventListener("change", function () {
      state.collection = this.value;
      state.conditions = newGroup("AND");
      state.executed = false;
      document.getElementById("aqResultsSection").style.display = "none";
      loadFieldMapping(function () {
        renderConditions();
        updatePreview();
        loadTemplates();
      });
    });

    // Template load
    var tplSelect = document.getElementById("aqTemplateSelect");
    tplSelect.addEventListener("change", function () {
      if (!this.value) return;
      _loadTemplate(this.value);
      this.value = "";
    });
  }

  // ═══════════════════════════════════════════════
  // Public API
  // ═══════════════════════════════════════════════
  function open(opts) {
    ensureModal();

    state.pwaCode = opts.pwaCode || "";
    state.pwaCodes = opts.pwaCodes || (opts.pwaCode ? [opts.pwaCode] : []);
    state.collection = opts.collection || "";
    state.availableLayers = opts.availableLayers || [opts.collection];
    state.startDate = opts.startDate || "";
    state.endDate = opts.endDate || "";
    state.page = 1;
    state.total = 0;
    state.totalPages = 0;
    state.results = [];
    state.executed = false;
    state.limit = 5000;
    state.sortBy = "";
    state.sortOrder = "desc";
    state.gridApi = null;
    state.viewMode = "table";

    // Reset conditions
    state.conditions = newGroup("AND");

    // Populate collection select
    var colSelect = document.getElementById("aqCollectionSelect");
    colSelect.innerHTML = "";
    var layerNames = {
      pipe: "ท่อประปา",
      valve: "วาล์ว",
      firehydrant: "หัวดับเพลิง",
      meter: "มาตรวัดน้ำ",
      bldg: "อาคาร",
      leakpoint: "จุดซ่อม",
      pwa_waterworks: "สถานีผลิต",
      struct: "สิ่งก่อสร้าง",
      pipe_serv: "ท่อบริการ",
    };
    for (var i = 0; i < state.availableLayers.length; i++) {
      var lyr = state.availableLayers[i];
      var opt = document.createElement("option");
      opt.value = lyr;
      opt.textContent = layerNames[lyr] || lyr;
      if (lyr === state.collection) opt.selected = true;
      colSelect.appendChild(opt);
    }

    // Render branch tags
    renderBranchTags();

    // Reset UI
    document.getElementById("aqResultsSection").style.display = "none";
    document.getElementById("aqBadge").textContent = "0";
    document.getElementById("aqLimit").value = "5000";

    modalEl.style.display = "";
    document.body.style.overflow = "hidden";

    loadFieldMapping(function () {
      renderConditions();
      updatePreview();
      loadTemplates();
    });
  }

  function close() {
    if (modalEl) {
      modalEl.style.display = "none";
      document.body.style.overflow = "";
    }
  }

  // ═══════════════════════════════════════════════
  // Field Mapping
  // ═══════════════════════════════════════════════
  function loadFieldMapping(cb) {
    var url = BASE + "/api/field-mapping?collection=" + enc(state.collection);
    fetch(url, { credentials: "same-origin" })
      .then(function (r) {
        return r.json();
      })
      .then(function (data) {
        if (data.status === "success" && data.mapping) {
          state.fieldMapping = data.mapping;
          state.reverseMapping = {};
          for (var mk in data.mapping) {
            if (data.mapping.hasOwnProperty(mk) && data.mapping[mk] !== "password") {
              state.reverseMapping[data.mapping[mk]] = mk;
            }
          }
          if (data.columns) {
            state.columns = data.columns;
          }
        } else {
          state.fieldMapping = {};
          state.reverseMapping = {};
        }
        if (cb) cb();
      })
      .catch(function () {
        state.fieldMapping = {};
        state.reverseMapping = {};
        if (cb) cb();
      });
  }

  // Get display name for a MongoDB field
  function displayName(mongoKey) {
    if (state.fieldMapping && state.fieldMapping[mongoKey]) {
      return state.fieldMapping[mongoKey];
    }
    return mongoKey;
  }

  // Get all available fields as [{mongoKey, displayKey}]
  function getFieldList() {
    var fields = [];
    if (!state.fieldMapping) return fields;
    for (var mk in state.fieldMapping) {
      if (state.fieldMapping.hasOwnProperty(mk)) {
        var pk = state.fieldMapping[mk];
        if (pk === "password") continue;
        fields.push({ mongoKey: mk, displayKey: pk });
      }
    }
    fields.sort(function (a, b) {
      return a.displayKey.localeCompare(b.displayKey);
    });
    return fields;
  }

  // ═══════════════════════════════════════════════
  // Condition Builder UI
  // ═══════════════════════════════════════════════
  var OPERATORS = [
    { value: "=", label: "=" },
    { value: "!=", label: "!=" },
    { value: ">", label: ">" },
    { value: "<", label: "<" },
    { value: ">=", label: ">=" },
    { value: "<=", label: "<=" },
    { value: "contains", label: "contains" },
    { value: "not_contains", label: "not contains" },
    { value: "in", label: "IN" },
    { value: "between", label: "BETWEEN" },
    { value: "is_empty", label: "is empty" },
    { value: "is_not_empty", label: "is not empty" },
  ];

  function renderConditions() {
    var container = document.getElementById("aqConditions");
    if (!container || !state.conditions) return;
    container.innerHTML = "";
    container.appendChild(renderGroup(state.conditions, 0, true));
  }

  function renderGroup(group, depth, isRoot) {
    var div = document.createElement("div");
    div.className = "aq-group";
    div.setAttribute("data-logic", group.logic);
    div.setAttribute("data-id", group.id);

    // Group header
    var header = document.createElement("div");
    header.className = "aq-group-header";

    // Logic toggle (AND/OR)
    var logicBtn = document.createElement("button");
    logicBtn.className =
      "aq-logic-toggle " + (group.logic === "OR" ? "aq-logic-or" : "");
    logicBtn.textContent = group.logic;
    logicBtn.title = 'สลับ AND/OR';
    logicBtn.onclick = function () {
      group.logic = group.logic === "AND" ? "OR" : "AND";
      renderConditions();
      updatePreview();
    };
    header.appendChild(logicBtn);

    // Add Rule button
    var addRuleBtn = document.createElement("button");
    addRuleBtn.className = "aq-btn aq-btn-xs aq-btn-add";
    addRuleBtn.innerHTML = '<i class="fa-solid fa-plus"></i> เงื่อนไข';
    addRuleBtn.onclick = function () {
      group.rules.push(newRule());
      renderConditions();
      updatePreview();
    };
    header.appendChild(addRuleBtn);

    // Add Group button
    var addGroupBtn = document.createElement("button");
    addGroupBtn.className = "aq-btn aq-btn-xs aq-btn-add";
    addGroupBtn.innerHTML = '<i class="fa-solid fa-layer-group"></i> กลุ่ม';
    addGroupBtn.onclick = function () {
      group.rules.push(newGroup(group.logic === "AND" ? "OR" : "AND"));
      renderConditions();
      updatePreview();
    };
    header.appendChild(addGroupBtn);

    // Remove group (not root)
    if (!isRoot) {
      var removeBtn = document.createElement("button");
      removeBtn.className = "aq-btn aq-btn-xs aq-btn-danger";
      removeBtn.innerHTML = '<i class="fa-solid fa-trash"></i>';
      removeBtn.title = "ลบกลุ่ม";
      removeBtn.onclick = function () {
        removeFromParent(state.conditions, group.id);
        renderConditions();
        updatePreview();
      };
      header.appendChild(removeBtn);
    }

    div.appendChild(header);

    // Rules
    var body = document.createElement("div");
    body.className = "aq-group-body";

    for (var i = 0; i < group.rules.length; i++) {
      var r = group.rules[i];
      if (r.type === "group") {
        body.appendChild(renderGroup(r, depth + 1, false));
      } else {
        body.appendChild(renderRule(r, group));
      }

      // Add AND/OR separator between rules (not after last)
      if (i < group.rules.length - 1) {
        var sep = document.createElement("div");
        sep.className = "aq-separator";
        sep.textContent = group.logic;
        body.appendChild(sep);
      }
    }

    div.appendChild(body);
    return div;
  }

  function renderRule(rule, parentGroup) {
    var div = document.createElement("div");
    div.className = "aq-rule";
    div.setAttribute("data-id", rule.id);

    // Field select (searchable)
    var fieldWrap = document.createElement("div");
    fieldWrap.className = "aq-field-wrap";
    var fieldInput = document.createElement("input");
    fieldInput.type = "text";
    fieldInput.className = "aq-field-input";
    fieldInput.placeholder = "เลือก column...";
    fieldInput.value = rule.field ? displayName(rule.field) : "";
    fieldInput.setAttribute("data-rule-id", rule.id);
    fieldInput.setAttribute("autocomplete", "off");

    var fieldDropdown = document.createElement("div");
    fieldDropdown.className = "aq-field-dropdown";
    fieldDropdown.style.display = "none";

    fieldInput.addEventListener("focus", function () {
      showFieldDropdown(this, fieldDropdown, rule);
    });
    fieldInput.addEventListener("input", function () {
      showFieldDropdown(this, fieldDropdown, rule);
    });
    fieldInput.addEventListener("blur", function () {
      var dd = fieldDropdown;
      setTimeout(function () {
        dd.style.display = "none";
      }, 200);
    });

    fieldWrap.appendChild(fieldInput);
    fieldWrap.appendChild(fieldDropdown);
    div.appendChild(fieldWrap);

    // Operator select
    var opSelect = document.createElement("select");
    opSelect.className = "aq-op-select";
    for (var i = 0; i < OPERATORS.length; i++) {
      var opt = document.createElement("option");
      opt.value = OPERATORS[i].value;
      opt.textContent = OPERATORS[i].label;
      if (OPERATORS[i].value === rule.operator) opt.selected = true;
      opSelect.appendChild(opt);
    }
    opSelect.onchange = function () {
      rule.operator = this.value;
      renderConditions();
      updatePreview();
    };
    div.appendChild(opSelect);

    // Value input(s)
    var hideValue =
      rule.operator === "is_empty" || rule.operator === "is_not_empty";

    if (!hideValue) {
      if (rule.operator === "between") {
        // Two value inputs
        var v1 = createValueInput(rule, "value");
        div.appendChild(v1);
        var andSpan = document.createElement("span");
        andSpan.className = "aq-between-and";
        andSpan.textContent = "~";
        div.appendChild(andSpan);
        var v2 = createValueInput(rule, "value2");
        div.appendChild(v2);
      } else if (rule.operator === "in") {
        // Comma-separated input
        var inInput = document.createElement("input");
        inInput.type = "text";
        inInput.className = "aq-value-input aq-value-in";
        inInput.placeholder = "ค่า1, ค่า2, ค่า3";
        inInput.value = rule.value || "";
        inInput.onchange = function () {
          rule.value = this.value;
          updatePreview();
        };
        inInput.oninput = function () {
          rule.value = this.value;
          updatePreview();
        };
        div.appendChild(inInput);
      } else {
        div.appendChild(createValueInput(rule, "value"));
      }
    }

    // Remove rule button
    var removeBtn = document.createElement("button");
    removeBtn.className = "aq-btn aq-btn-xs aq-btn-danger aq-rule-remove";
    removeBtn.innerHTML = "&times;";
    removeBtn.title = "ลบเงื่อนไข";
    removeBtn.onclick = function () {
      // Don't remove the last rule in a group
      if (parentGroup.rules.length <= 1) return;
      var idx = parentGroup.rules.indexOf(rule);
      if (idx > -1) parentGroup.rules.splice(idx, 1);
      renderConditions();
      updatePreview();
    };
    div.appendChild(removeBtn);

    return div;
  }

  function createValueInput(rule, valueKey) {
    var wrap = document.createElement("div");
    wrap.className = "aq-value-wrap";

    var input = document.createElement("input");
    input.type = "text";
    input.className = "aq-value-input";
    input.placeholder = "ค่า...";
    input.value = rule[valueKey] || "";
    input.setAttribute("autocomplete", "off");

    var dropdown = document.createElement("div");
    dropdown.className = "aq-suggest-dropdown";
    dropdown.style.display = "none";

    input.addEventListener("input", function () {
      rule[valueKey] = this.value;
      updatePreview();
      debouncedSuggest(rule.field, this.value, dropdown, input, rule, valueKey);
    });
    input.addEventListener("blur", function () {
      var dd = dropdown;
      setTimeout(function () {
        dd.style.display = "none";
      }, 200);
    });
    input.addEventListener("keydown", function (e) {
      if (e.key === "Enter") {
        dropdown.style.display = "none";
      }
    });

    wrap.appendChild(input);
    wrap.appendChild(dropdown);
    return wrap;
  }

  // ═══════════════════════════════════════════════
  // Field Dropdown (autocomplete column names)
  // ═══════════════════════════════════════════════
  function showFieldDropdown(input, dropdown, rule) {
    var fields = getFieldList();
    var query = input.value.toLowerCase();

    var filtered = fields.filter(function (f) {
      return (
        f.displayKey.toLowerCase().indexOf(query) >= 0 ||
        f.mongoKey.toLowerCase().indexOf(query) >= 0
      );
    });

    if (filtered.length === 0) {
      dropdown.style.display = "none";
      return;
    }

    dropdown.innerHTML = "";
    for (var i = 0; i < filtered.length; i++) {
      (function (f) {
        var item = document.createElement("div");
        item.className = "aq-field-item";
        item.innerHTML =
          "<span>" + esc(f.displayKey) + "</span>" +
          '<span class="aq-field-mongo">' + esc(f.mongoKey) + "</span>";
        item.onmousedown = function (e) {
          e.preventDefault();
          rule.field = f.mongoKey;
          input.value = f.displayKey;
          dropdown.style.display = "none";
          updatePreview();
        };
        dropdown.appendChild(item);
      })(filtered[i]);
    }
    dropdown.style.display = "";
  }

  // ═══════════════════════════════════════════════
  // Value Suggestions
  // ═══════════════════════════════════════════════
  function debouncedSuggest(field, query, dropdown, input, rule, valueKey) {
    if (state.suggestTimer) clearTimeout(state.suggestTimer);
    if (!field || query.length < 2) {
      dropdown.style.display = "none";
      return;
    }
    state.suggestTimer = setTimeout(function () {
      fetchSuggestions(field, query, dropdown, input, rule, valueKey);
    }, 300);
  }

  function fetchSuggestions(field, query, dropdown, input, rule, valueKey) {
    var url =
      BASE +
      "/api/features/suggest?pwaCode=" +
      enc(state.pwaCode) +
      "&collection=" +
      enc(state.collection) +
      "&q=" +
      enc(query) +
      "&limit=8";

    fetch(url, { credentials: "same-origin" })
      .then(function (r) {
        return r.json();
      })
      .then(function (data) {
        if (!data.suggestions || data.suggestions.length === 0) {
          dropdown.style.display = "none";
          return;
        }

        // Filter suggestions to match the selected field
        var filtered = data.suggestions.filter(function (s) {
          return s.field === field;
        });

        if (filtered.length === 0) {
          // Show all suggestions if no field-specific match
          filtered = data.suggestions.slice(0, 5);
        }

        dropdown.innerHTML = "";
        for (var i = 0; i < filtered.length; i++) {
          (function (s) {
            var item = document.createElement("div");
            item.className = "aq-suggest-item";
            item.innerHTML =
              "<span>" + esc(s.value) + "</span>" +
              '<span class="aq-suggest-field">' + esc(s.field) + "</span>";
            item.onmousedown = function (e) {
              e.preventDefault();
              rule[valueKey] = s.value;
              input.value = s.value;
              dropdown.style.display = "none";
              updatePreview();
            };
            dropdown.appendChild(item);
          })(filtered[i]);
        }
        dropdown.style.display = "";
      })
      .catch(function () {
        dropdown.style.display = "none";
      });
  }

  // ═══════════════════════════════════════════════
  // Query Preview
  // ═══════════════════════════════════════════════
  function updatePreview() {
    var el = document.getElementById("aqPreview");
    if (!el || !state.conditions) return;
    var text = buildPreviewText(state.conditions);
    el.innerHTML = text || '<span class="aq-preview-empty">กรุณาเพิ่มเงื่อนไข</span>';
  }

  function buildPreviewText(group) {
    if (!group || !group.rules || group.rules.length === 0) return "";

    var parts = [];
    for (var i = 0; i < group.rules.length; i++) {
      var r = group.rules[i];
      if (r.type === "group") {
        var nested = buildPreviewText(r);
        if (nested) parts.push("(" + nested + ")");
      } else {
        if (!r.field) continue;
        var dn = displayName(r.field);
        var part = '<span class="aq-pv-field">' + esc(dn) + "</span> ";
        part += '<span class="aq-pv-op">' + esc(r.operator) + "</span> ";

        if (r.operator === "is_empty" || r.operator === "is_not_empty") {
          // no value
        } else if (r.operator === "between") {
          part +=
            '<span class="aq-pv-val">' +
            esc(String(r.value)) +
            "</span>" +
            ' <span class="aq-pv-op">~</span> ' +
            '<span class="aq-pv-val">' +
            esc(String(r.value2)) +
            "</span>";
        } else if (r.operator === "in") {
          part +=
            '(<span class="aq-pv-val">' + esc(String(r.value)) + "</span>)";
        } else {
          part +=
            '<span class="aq-pv-val">' + esc(String(r.value)) + "</span>";
        }
        parts.push(part);
      }
    }

    if (parts.length === 0) return "";
    var logicClass = group.logic === "OR" ? "aq-pv-or" : "aq-pv-and";
    var logicSep =
      ' <span class="' + logicClass + '"> ' + group.logic + " </span> ";
    return parts.join(logicSep);
  }

  // ═══════════════════════════════════════════════
  // Tree Helpers
  // ═══════════════════════════════════════════════
  function removeFromParent(group, targetId) {
    for (var i = 0; i < group.rules.length; i++) {
      if (group.rules[i].id === targetId) {
        group.rules.splice(i, 1);
        return true;
      }
      if (group.rules[i].type === "group") {
        if (removeFromParent(group.rules[i], targetId)) return true;
      }
    }
    return false;
  }

  // ═══════════════════════════════════════════════
  // Query Execution
  // ═══════════════════════════════════════════════
  function execute() {
    state.limit = parseInt(document.getElementById("aqLimit").value, 10) || 5000;
    state.page = 1;
    _doExecute();
  }

  function _doExecute() {
    var conditions = serializeConditions(state.conditions);
    if (
      !conditions ||
      !conditions.rules ||
      conditions.rules.length === 0
    ) {
      alert("กรุณาเพิ่มเงื่อนไขอย่างน้อย 1 รายการ");
      return;
    }

    setLoading(true);

    var body = {
      pwaCode: state.pwaCode,
      pwaCodes: state.pwaCodes,
      collection: state.collection,
      conditions: conditions,
      page: state.page,
      pageSize: state.pageSize,
      limit: state.limit,
    };
    if (state.startDate) body.startDate = state.startDate;
    if (state.endDate) body.endDate = state.endDate;
    if (state.sortBy) {
      body.sortBy = state.sortBy;
      body.sortOrder = state.sortOrder;
    }

    fetch(BASE + "/api/features/advanced-query", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(body),
    })
      .then(function (r) {
        return r.json();
      })
      .then(function (data) {
        setLoading(false);
        if (data.status !== "success") {
          alert("Error: " + (data.error || "Unknown error"));
          return;
        }

        state.results = data.data || [];
        state.total = data.total || 0;
        state.page = data.page || 1;
        state.totalPages = data.total_pages || 1;
        state.columns = data.columns || [];
        state.executed = true;

        document.getElementById("aqBadge").textContent = fmtNum(state.total);
        document.getElementById("aqResultsSection").style.display = "";

        // Delay grid creation to ensure container has reflowed after display change
        setTimeout(function() { updateGrid(); }, 50);
        updateFooter();
      })
      .catch(function (err) {
        setLoading(false);
        alert("เกิดข้อผิดพลาด: " + err.message);
      });
  }

  // ═══════════════════════════════════════════════
  // AG Grid
  // ═══════════════════════════════════════════════
  function updateGrid() {
    var gridDiv = document.getElementById("aqGrid");
    if (!gridDiv) return;

    // Build column definitions from API columns
    var cols = state.columns || [];

    // If columns empty, derive from data keys
    if (cols.length === 0 && state.results.length > 0) {
      var derivedKeys = Object.keys(state.results[0]).sort();
      cols = [];
      for (var dk = 0; dk < derivedKeys.length; dk++) {
        if (derivedKeys[dk] === "_doc_id" || derivedKeys[dk] === "password") continue;
        cols.push({ key: derivedKeys[dk], mongo_key: derivedKeys[dk] });
      }
      state.columns = cols;
    }

    // Destroy existing grid — if AG Grid API exists, destroy properly
    if (state.gridApi && typeof state.gridApi.destroy === "function") {
      try { state.gridApi.destroy(); } catch (ignored) {}
    }
    state.gridApi = null;
    gridDiv.innerHTML = "";

    // Try AG Grid first
    if (typeof agGrid !== "undefined" && typeof agGrid.createGrid === "function") {
      try {
        var colDefs = [];
        // Row number column
        colDefs.push({
          headerName: "#",
          valueGetter: function (params) {
            return (state.page - 1) * state.pageSize + params.node.rowIndex + 1;
          },
          width: 60,
          pinned: "left",
          sortable: false,
          suppressSizeToFit: true,
        });

        for (var i = 0; i < cols.length; i++) {
          if (cols[i].key === "_doc_id" || cols[i].key === "password") continue;
          colDefs.push({
            headerName: cols[i].key,
            field: cols[i].key,
            sortable: true,
            resizable: true,
            filter: false,
            minWidth: 80,
            tooltipField: cols[i].key,
          });
        }

        var gridOptions = {
          columnDefs: colDefs,
          rowData: state.results,
          defaultColDef: {
            sortable: true,
            resizable: true,
            filter: false,
            minWidth: 70,
          },
          animateRows: false,
          suppressPaginationPanel: true,
          domLayout: "autoHeight",
          onSortChanged: function (event) {
            var sortModel = event.api.getColumnState
              ? event.api.getColumnState().filter(function (c) { return c.sort; })
              : [];
            if (sortModel.length > 0) {
              var sortKey = sortModel[0].colId;
              if (state.reverseMapping && state.reverseMapping[sortKey]) {
                state.sortBy = state.reverseMapping[sortKey];
              } else {
                state.sortBy = sortKey;
              }
              state.sortOrder = sortModel[0].sort;
              state.page = 1;
              _doExecute();
            }
          },
          onGridReady: function (params) {
            params.api.sizeColumnsToFit();
            console.log("[AQ] AG Grid ready, rows rendered:", state.results.length);
          },
        };

        state.gridApi = agGrid.createGrid(gridDiv, gridOptions);
        console.log("[AQ] AG Grid created:", state.results.length, "rows");
        return;
      } catch (e) {
        console.warn("[AQ] AG Grid failed, using fallback table:", e);
      }
    } else {
      console.log("[AQ] AG Grid not available, using fallback table");
    }

    // Fallback: HTML table
    renderFallbackTable(gridDiv);
  }

  function renderFallbackTable(container) {
    var cols = state.columns || [];
    var html = '<table class="aq-fallback-table"><thead><tr>';
    html += "<th>#</th>";
    for (var i = 0; i < cols.length; i++) {
      if (cols[i].key === "_doc_id" || cols[i].key === "password") continue;
      html += "<th>" + esc(cols[i].key) + "</th>";
    }
    html += "</tr></thead><tbody>";
    for (var r = 0; r < state.results.length; r++) {
      var row = state.results[r];
      html += "<tr><td>" + ((state.page - 1) * state.pageSize + r + 1) + "</td>";
      for (var j = 0; j < cols.length; j++) {
        if (cols[j].key === "_doc_id" || cols[j].key === "password") continue;
        var val = row[cols[j].key];
        html += "<td>" + (val != null ? esc(String(val)) : "") + "</td>";
      }
      html += "</tr>";
    }
    html += "</tbody></table>";
    container.innerHTML = html;
  }

  // ═══════════════════════════════════════════════
  // Pagination
  // ═══════════════════════════════════════════════
  function updateFooter() {
    var info = document.getElementById("aqPageInfo");
    var pageNum = document.getElementById("aqPageNum");
    var totalInfo = document.getElementById("aqTotalInfo");

    if (info)
      info.textContent =
        "หน้า " +
        state.page +
        " / " +
        state.totalPages +
        "  (" +
        fmtNum(state.total) +
        " รายการ)";
    if (pageNum) pageNum.textContent = state.page;
    if (totalInfo) totalInfo.textContent = fmtNum(state.total) + " รายการ";
  }

  function firstPage() {
    if (state.page <= 1) return;
    state.page = 1;
    _doExecute();
  }
  function prevPage() {
    if (state.page <= 1) return;
    state.page--;
    _doExecute();
  }
  function nextPage() {
    if (state.page >= state.totalPages) return;
    state.page++;
    _doExecute();
  }
  function lastPage() {
    if (state.page >= state.totalPages) return;
    state.page = state.totalPages;
    _doExecute();
  }

  // ═══════════════════════════════════════════════
  // Export
  // ═══════════════════════════════════════════════
  function exportResults(format) {
    var conditions = serializeConditions(state.conditions);
    if (!conditions || !conditions.rules || conditions.rules.length === 0) {
      alert("กรุณาเพิ่มเงื่อนไขก่อน export");
      return;
    }

    setLoading(true);

    var body = {
      pwaCode: state.pwaCode,
      pwaCodes: state.pwaCodes,
      collection: state.collection,
      conditions: conditions,
      limit: state.limit,
      format: format,
    };
    if (state.startDate) body.startDate = state.startDate;
    if (state.endDate) body.endDate = state.endDate;

    fetch(BASE + "/api/features/advanced-query/export", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(body),
    })
      .then(function (r) {
        if (!r.ok) throw new Error("Export failed: " + r.status);
        var cd = r.headers.get("Content-Disposition");
        var filename = "export." + format;
        if (cd) {
          var match = cd.match(/filename[^;=\n]*=((['"]).*?\2|[^;\n]*)/);
          if (match && match[1]) filename = match[1].replace(/['"]/g, "");
        }
        return r.blob().then(function (blob) {
          return { blob: blob, filename: filename };
        });
      })
      .then(function (result) {
        setLoading(false);
        var url = URL.createObjectURL(result.blob);
        var a = document.createElement("a");
        a.href = url;
        a.download = result.filename;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        URL.revokeObjectURL(url);
      })
      .catch(function (err) {
        setLoading(false);
        alert("Export error: " + err.message);
      });
  }

  // ═══════════════════════════════════════════════
  // Templates (localStorage)
  // ═══════════════════════════════════════════════
  function getTemplateKey() {
    return "aq_templates_" + state.collection;
  }

  function loadTemplates() {
    var select = document.getElementById("aqTemplateSelect");
    if (!select) return;

    select.innerHTML = '<option value="">-- Template --</option>';

    try {
      var templates = JSON.parse(localStorage.getItem(getTemplateKey()) || "[]");
      for (var i = 0; i < templates.length; i++) {
        var opt = document.createElement("option");
        opt.value = String(i);
        opt.textContent = templates[i].name;
        select.appendChild(opt);
      }

      // Add "delete all" option if templates exist
      if (templates.length > 0) {
        var delOpt = document.createElement("option");
        delOpt.value = "__delete_all__";
        delOpt.textContent = "--- ลบ Templates ทั้งหมด ---";
        delOpt.style.color = "#ef4444";
        select.appendChild(delOpt);
      }
    } catch (e) {
      // ignore
    }
  }

  function saveTemplate() {
    var name = prompt("ตั้งชื่อ Template:");
    if (!name) return;

    var conditions = serializeConditions(state.conditions);
    var tpl = {
      name: name,
      conditions: conditions,
      createdAt: new Date().toISOString(),
    };

    try {
      var templates = JSON.parse(localStorage.getItem(getTemplateKey()) || "[]");
      templates.push(tpl);
      localStorage.setItem(getTemplateKey(), JSON.stringify(templates));
      loadTemplates();
    } catch (e) {
      alert("ไม่สามารถบันทึก Template ได้");
    }
  }

  function _loadTemplate(idxStr) {
    if (idxStr === "__delete_all__") {
      if (confirm("ลบ Templates ทั้งหมดสำหรับ " + state.collection + "?")) {
        localStorage.removeItem(getTemplateKey());
        loadTemplates();
      }
      return;
    }

    var idx = parseInt(idxStr, 10);
    try {
      var templates = JSON.parse(localStorage.getItem(getTemplateKey()) || "[]");
      if (idx >= 0 && idx < templates.length) {
        var tpl = templates[idx];
        // Rebuild conditions from template
        state.conditions = rebuildConditions(tpl.conditions);
        renderConditions();
        updatePreview();
      }
    } catch (e) {
      // ignore
    }
  }

  // Rebuild condition objects with fresh IDs
  function rebuildConditions(serialized) {
    if (!serialized) return newGroup("AND");

    var group = {
      id: nextId++,
      type: "group",
      logic: serialized.logic || "AND",
      rules: [],
    };

    if (serialized.rules) {
      for (var i = 0; i < serialized.rules.length; i++) {
        var r = serialized.rules[i];
        if (r.logic) {
          // Nested group
          group.rules.push(rebuildConditions(r));
        } else {
          group.rules.push({
            id: nextId++,
            type: "rule",
            field: r.field || "",
            operator: r.operator || "=",
            value: r.value || "",
            value2: r.value2 || "",
          });
        }
      }
    }

    if (group.rules.length === 0) {
      group.rules.push(newRule());
    }

    return group;
  }

  // ═══════════════════════════════════════════════
  // Branch Tags (multi-branch display)
  // ═══════════════════════════════════════════════
  function renderBranchTags() {
    var container = document.getElementById("aqBranchTags");
    if (!container) return;
    var codes = state.pwaCodes || [];
    if (codes.length <= 1) {
      container.innerHTML =
        '<span class="aq-branch-tag">' +
        '<i class="fa-solid fa-store"></i> ' + esc(codes[0] || state.pwaCode) +
        "</span>";
      return;
    }
    var html = "";
    for (var i = 0; i < codes.length; i++) {
      html +=
        '<span class="aq-branch-tag">' +
        '<i class="fa-solid fa-store"></i> ' + esc(codes[i]) +
        "</span>";
    }
    html += '<span class="aq-branch-count">' + codes.length + " สาขา</span>";
    container.innerHTML = html;
  }

  // ═══════════════════════════════════════════════
  // View Toggle (Table / Map)
  // ═══════════════════════════════════════════════
  function setView(mode) {
    state.viewMode = mode;
    var gridEl = document.getElementById("aqGrid");
    var mapEl = document.getElementById("aqMapContainer");
    var tableBtn = document.getElementById("aqViewTable");
    var mapBtn = document.getElementById("aqViewMap");
    var footerEl = gridEl ? gridEl.closest(".aq-results").querySelector(".aq-footer") : null;

    if (mode === "map") {
      if (gridEl) gridEl.style.display = "none";
      if (mapEl) mapEl.style.display = "";
      if (tableBtn) tableBtn.classList.remove("active");
      if (mapBtn) mapBtn.classList.add("active");
      if (footerEl) footerEl.style.display = "none";
      renderMap();
    } else {
      if (gridEl) gridEl.style.display = "";
      if (mapEl) mapEl.style.display = "none";
      if (tableBtn) tableBtn.classList.add("active");
      if (mapBtn) mapBtn.classList.remove("active");
      if (footerEl) footerEl.style.display = "";
    }
  }

  function renderMap() {
    var mapEl = document.getElementById("aqMapContainer");
    if (!mapEl || typeof maplibregl === "undefined") {
      mapEl.innerHTML = '<div style="padding:40px;text-align:center;color:var(--text-muted);">MapLibre GL JS not available</div>';
      return;
    }

    // Create or reuse map
    if (!state.mapInstance) {
      mapEl.innerHTML = "";
      state.mapInstance = new maplibregl.Map({
        container: mapEl,
        style: {
          version: 8,
          sources: {
            osm: {
              type: "raster",
              tiles: ["https://tile.openstreetmap.org/{z}/{x}/{y}.png"],
              tileSize: 256,
            },
          },
          layers: [
            { id: "osm", type: "raster", source: "osm", paint: { "raster-opacity": 0.55 } },
          ],
        },
        center: [100.5, 13.7],
        zoom: 7,
        attributionControl: false,
      });
      state.mapInstance.addControl(new maplibregl.NavigationControl(), "top-right");
    }

    // Fetch GeoJSON for map
    setLoading(true);
    var conditions = serializeConditions(state.conditions);
    var body = {
      pwaCode: state.pwaCode,
      pwaCodes: state.pwaCodes,
      collection: state.collection,
      conditions: conditions,
      limit: state.limit,
      format: "geojson",
    };
    if (state.startDate) body.startDate = state.startDate;
    if (state.endDate) body.endDate = state.endDate;

    fetch(BASE + "/api/features/advanced-query/export", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      credentials: "same-origin",
      body: JSON.stringify(body),
    })
      .then(function (r) { return r.json(); })
      .then(function (geojson) {
        setLoading(false);
        if (!geojson || !geojson.features) return;

        var map = state.mapInstance;

        // Remove old layer/source
        if (map.getLayer("aq-features-layer")) map.removeLayer("aq-features-layer");
        if (map.getLayer("aq-features-points")) map.removeLayer("aq-features-points");
        if (map.getSource("aq-features")) map.removeSource("aq-features");

        // Add source
        map.addSource("aq-features", { type: "geojson", data: geojson });

        // Determine geometry type from first feature
        var geomType = "";
        if (geojson.features.length > 0 && geojson.features[0].geometry) {
          geomType = geojson.features[0].geometry.type || "";
        }

        if (geomType.indexOf("Polygon") >= 0) {
          map.addLayer({
            id: "aq-features-layer", type: "fill", source: "aq-features",
            paint: { "fill-color": "#3498DB", "fill-opacity": 0.4, "fill-outline-color": "#2980B9" },
          });
        } else if (geomType.indexOf("Line") >= 0) {
          map.addLayer({
            id: "aq-features-layer", type: "line", source: "aq-features",
            paint: { "line-color": "#E74C3C", "line-width": 2 },
          });
        } else {
          map.addLayer({
            id: "aq-features-points", type: "circle", source: "aq-features",
            paint: { "circle-radius": 5, "circle-color": "#3498DB", "circle-stroke-width": 1, "circle-stroke-color": "#fff" },
          });
        }

        // Fit bounds
        if (geojson.features.length > 0) {
          var bounds = new maplibregl.LngLatBounds();
          geojson.features.forEach(function (f) {
            if (!f.geometry || !f.geometry.coordinates) return;
            addCoordsToBounds(bounds, f.geometry.coordinates);
          });
          if (!bounds.isEmpty()) {
            map.fitBounds(bounds, { padding: 40, maxZoom: 16 });
          }
        }

        // Popup on click
        var clickLayer = map.getLayer("aq-features-layer") ? "aq-features-layer" : "aq-features-points";
        map.on("click", clickLayer, function (e) {
          if (!e.features || !e.features[0]) return;
          var props = e.features[0].properties || {};
          var html = '<div style="max-height:200px;overflow-y:auto;font-size:12px;font-family:\'Noto Sans Thai\',sans-serif;">';
          for (var k in props) {
            if (k === "_id" || k === "password") continue;
            html += "<div><strong>" + esc(k) + ":</strong> " + esc(String(props[k])) + "</div>";
          }
          html += "</div>";
          new maplibregl.Popup({ maxWidth: "320px" })
            .setLngLat(e.lngLat)
            .setHTML(html)
            .addTo(map);
        });
        map.on("mouseenter", clickLayer, function () { map.getCanvas().style.cursor = "pointer"; });
        map.on("mouseleave", clickLayer, function () { map.getCanvas().style.cursor = ""; });
      })
      .catch(function (err) {
        setLoading(false);
        console.error("[AQ] Map load error:", err);
      });
  }

  function addCoordsToBounds(bounds, coords) {
    if (typeof coords[0] === "number") {
      bounds.extend(coords);
    } else {
      for (var i = 0; i < coords.length; i++) {
        addCoordsToBounds(bounds, coords[i]);
      }
    }
  }

  // ═══════════════════════════════════════════════
  // Helpers
  // ═══════════════════════════════════════════════
  function setLoading(on) {
    state.loading = on;
    var el = document.getElementById("aqLoading");
    if (el) el.style.display = on ? "" : "none";
  }

  function esc(s) {
    var d = document.createElement("div");
    d.appendChild(document.createTextNode(s));
    return d.innerHTML;
  }

  function enc(s) {
    return encodeURIComponent(s);
  }

  function fmtNum(n) {
    return Number(n).toLocaleString("th-TH");
  }

  // ═══════════════════════════════════════════════
  // Public Interface
  // ═══════════════════════════════════════════════
  return {
    open: open,
    close: close,
    _execute: execute,
    _export: exportResults,
    _saveTemplate: saveTemplate,
    _firstPage: firstPage,
    _prevPage: prevPage,
    _nextPage: nextPage,
    _lastPage: lastPage,
    _setView: setView,
  };
})();
