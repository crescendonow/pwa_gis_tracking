(() => {
  "use strict";

  const BASE = "/pwa_gis_tracking";
  const OSM_STYLE = "https://tiles.openfreemap.org/styles/bright";
  const OSM_SOURCE = "pwa-osm-buildings";
  const SOURCE_LAYER = "pwa_gis";
  const layerDefinitions = [
    { id: "pwa_waterworks", label: "สำนักงาน กปภ.", minzoom: 5, color: "#225ea8", type: "point" },
    { id: "dma_boundary", label: "ขอบเขต DMA", minzoom: 5, color: "#2f80ed", type: "dma" },
    { id: "pipe", label: "ท่อประปา", minzoom: 10, color: "#2878c7", type: "line" },
    { id: "pipe_serv", label: "ท่อบริการ", minzoom: 10, color: "#58a6d8", type: "line-thin" },
    { id: "bldg", label: "อาคาร", minzoom: 14, color: "#d3a35c", type: "fill" },
    { id: "struct", label: "โครงสร้าง", minzoom: 14, color: "#7f8c8d", type: "fill" },
    { id: "valve", label: "ประตูน้ำ", minzoom: 14, color: "#7546b8", type: "point" },
    { id: "firehydrant", label: "หัวดับเพลิง", minzoom: 14, color: "#e24d42", type: "point" },
    { id: "leakpoint", label: "จุดแตกรั่ว", minzoom: 14, color: "#f27a38", type: "point" },
    { id: "step_test", label: "จุดทดสอบ", minzoom: 14, color: "#16a085", type: "point" },
    { id: "meter", label: "มาตรวัดน้ำ", minzoom: 14, color: "#16a34a", type: "point" },
    { id: "flow_meter", label: "มาตรวัดอัตราการไหล", minzoom: 14, color: "#d59b15", type: "point" }
  ];

  const state = { map: null, config: {}, offices: [], session: {}, mapScope: "branch", originalLayers: [], marker: null };
  const byId = (id) => document.getElementById(id);

  async function requestJSON(path, options) {
    const response = await fetch(BASE + path, { credentials: "same-origin", ...options });
    if (response.status === 401) { window.location.href = BASE + "/login"; throw new Error("session expired"); }
    if (!response.ok) throw new Error(`request failed (${response.status})`);
    return response.json();
  }

  async function init() {
    bindUI();
    try {
      const [config, session, zones, offices, summary] = await Promise.all([
        requestJSON("/api/map/config"), requestJSON("/api/session/info"), requestJSON("/api/zones"),
        requestJSON("/api/offices/geom"), requestJSON("/api/map/summary").catch(() => ({ layers: [] }))
      ]);
      state.config = config;
      state.session = session;
      state.mapScope = config.map_scope || summary.scope || session.permission_leak || "branch";
      state.offices = Array.isArray(offices.data) ? offices.data : [];
      if (config.google_enabled) byId("satelliteName").textContent = "Google Satellite + ชื่อสถานที่ OSM";
      renderScope();
      renderFilters(Array.isArray(zones.data) ? zones.data : []);
      renderLayers(summary.layers || []);
      createMap();
    } catch (error) {
      setStatus("ไม่สามารถเริ่มต้นแผนที่ได้", "error");
      console.error(error);
    }
  }

  function bindUI() {
    byId("sidebarClose").addEventListener("click", () => toggleSidebar(true));
    byId("sidebarOpen").addEventListener("click", () => toggleSidebar(false));
    byId("quickSearchButton").addEventListener("click", quickSearch);
    byId("quickSearch").addEventListener("keydown", (event) => { if (event.key === "Enter") quickSearch(); });
    byId("mapZone").addEventListener("change", updateBranchOptions);
    byId("applyFilters").addEventListener("click", applyFilters);
    byId("clearFilters").addEventListener("click", clearFilters);
    byId("terrainToggle").addEventListener("change", toggleTerrain);
    byId("buildingToggle").addEventListener("change", toggleBuildings);
    document.querySelectorAll("input[name=basemap]").forEach((input) => input.addEventListener("change", switchBasemap));
    byId("aiForm").addEventListener("submit", askAssistant);
  }

  function renderScope() {
    const labels = { all: "สิทธิ์ดูข้อมูลทุกเขต", reg: `สิทธิ์เขต ${state.session.area || ""}`, branch: `สิทธิ์สาขา ${state.session.pwa_code || ""}` };
    byId("mapScopeLabel").textContent = labels[state.mapScope] || "สิทธิ์ระดับสาขา";
  }

  function allowedOffices() {
    const scope = state.mapScope;
    if (scope === "all") return state.offices;
    if (scope === "reg") return state.offices.filter((office) => String(office.zone) === String(state.session.area));
    return state.offices.filter((office) => String(office.pwa_code) === String(state.session.pwa_code));
  }

  function renderFilters(zones) {
    const zoneSelect = byId("mapZone");
    const scope = state.mapScope;
    zones.filter((item) => scope === "all" || String(item.zone) === String(state.session.area)).forEach((item) => {
      zoneSelect.add(new Option(`เขต ${item.zone} (${item.branch_count} สาขา)`, item.zone));
    });
    if (scope === "reg" || scope === "branch") {
      zoneSelect.value = state.session.area || "";
      zoneSelect.disabled = true;
    }
    updateBranchOptions();
    if (scope === "branch") {
      byId("mapBranch").value = state.session.pwa_code || "";
      byId("mapBranch").disabled = true;
    }
  }

  function updateBranchOptions() {
    const select = byId("mapBranch");
    const selected = select.value;
    const zone = byId("mapZone").value;
    select.replaceChildren(new Option("ทุกสาขาที่ได้รับสิทธิ์", ""));
    allowedOffices().filter((office) => !zone || String(office.zone) === zone).forEach((office) => {
      select.add(new Option(`${office.pwa_code} — ${office.name}`, office.pwa_code));
    });
    if ([...select.options].some((option) => option.value === selected)) select.value = selected;
  }

  function renderLayers(summaries) {
    const counts = new Map(summaries.map((item) => [item.layer, Number(item.feature_count || 0)]));
    const container = byId("projectLayers");
    let total = 0;
    layerDefinitions.forEach((definition) => {
      const count = counts.get(definition.id) || 0;
      total += count;
      const label = document.createElement("label");
      label.className = "choice";
      const input = document.createElement("input");
      input.type = "checkbox"; input.checked = true; input.dataset.layer = definition.id;
      input.addEventListener("change", () => setProjectLayerVisibility(definition.id, input.checked));
      const swatch = document.createElement("span"); swatch.className = "layer-swatch"; swatch.style.background = definition.color;
      const text = document.createElement("span"); text.textContent = `${definition.label}${count ? ` (${count.toLocaleString("th-TH")})` : ""}`;
      label.append(input, swatch, text); container.append(label);
    });
    byId("mapFeatureTotal").textContent = total ? total.toLocaleString("th-TH") : "–";
  }

  function createMap() {
    state.map = new maplibregl.Map({
      container: "overviewMap", style: OSM_STYLE, center: [100.55, 13.25], zoom: 5.7,
      attributionControl: false, antialias: true, maxZoom: 22
    });
    state.map.addControl(new maplibregl.NavigationControl({ visualizePitch: true }), "top-right");
    state.map.addControl(new maplibregl.ScaleControl({ unit: "metric" }), "bottom-left");
    state.map.on("load", onMapLoad);
    state.map.on("zoom", () => { byId("zoomReadout").textContent = `Zoom ${state.map.getZoom().toFixed(1)}`; });
    state.map.on("click", showFeaturePopup);
    state.map.on("error", (event) => console.warn("MapLibre", event.error || event));
  }

  function onMapLoad() {
    state.originalLayers = state.map.getStyle().layers.map((layer) => ({ id: layer.id, type: layer.type, visibility: layer.layout && layer.layout.visibility }));
    const firstLabel = state.originalLayers.find((layer) => layer.type === "symbol");
    state.map.addSource("esri-world-imagery", { type: "raster", tileSize: 256, maxzoom: 19, tiles: ["https://server.arcgisonline.com/ArcGIS/rest/services/World_Imagery/MapServer/tile/{z}/{y}/{x}"] });
    state.map.addLayer({ id: "esri-world-imagery", source: "esri-world-imagery", type: "raster", layout: { visibility: "none" }, paint: { "raster-fade-duration": 150 } }, firstLabel && firstLabel.id);
    if (state.config.google_enabled && state.config.google_tile_proxy_template) {
      state.map.addSource("google-satellite", { type: "raster", tileSize: 256, maxzoom: 20, tiles: [window.location.origin + state.config.google_tile_proxy_template] });
      state.map.addLayer({ id: "google-satellite", source: "google-satellite", type: "raster", layout: { visibility: "none" }, paint: { "raster-fade-duration": 150 } }, firstLabel && firstLabel.id);
    }
    addProjectLayers();
    addOSMBuildings();
    setStatus("แผนที่พร้อมใช้งาน", "ready");
    byId("zoomReadout").textContent = `Zoom ${state.map.getZoom().toFixed(1)}`;
  }

  function tileURL(layer) {
    const params = new URLSearchParams();
    const zone = byId("mapZone").value;
    const branch = byId("mapBranch").value;
    if (zone) params.set("zone", zone);
    if (branch) params.set("pwa_code", branch);
    if (byId("mapStartDate").value) params.set("start_date", byId("mapStartDate").value);
    if (byId("mapEndDate").value) params.set("end_date", byId("mapEndDate").value);
    return `${window.location.origin}${BASE}/api/map/tiles/${layer}/{z}/{x}/{y}?${params}`;
  }

  function addProjectLayers() {
    layerDefinitions.forEach((definition) => {
      state.map.addSource(`pwa-${definition.id}`, { type: "vector", tiles: [tileURL(definition.id)], minzoom: definition.minzoom, maxzoom: 22, promoteId: "_fid" });
      styleLayers(definition).forEach((layer) => state.map.addLayer(layer));
    });
  }

  function styleLayers(definition) {
    const common = { source: `pwa-${definition.id}`, "source-layer": SOURCE_LAYER, minzoom: definition.minzoom };
    const labelLayer = {
      ...common, id: `pwa-${definition.id}-label`, type: "symbol", minzoom: 17,
      layout: { "text-field": ["coalesce", ["get", "label"], ["get", "_fid"]], "text-size": 10, "text-offset": [0, 1.1], "text-anchor": "top", "text-optional": true },
      paint: { "text-color": "#263746", "text-halo-color": "#ffffff", "text-halo-width": 1.2 }
    };
    if (definition.type === "dma") return [
      { ...common, id: "pwa-dma_boundary-fill", type: "fill", paint: { "fill-color": ["coalesce", ["get", "sld_color_fill"], definition.color], "fill-opacity": .22 } },
      { ...common, id: "pwa-dma_boundary-line", type: "line", paint: { "line-color": ["coalesce", ["get", "sld_color_stroke"], "#1f4f8a"], "line-width": ["interpolate", ["linear"], ["zoom"], 5, 1, 14, 2.4] } },
      labelLayer
    ];
    if (definition.type === "line" || definition.type === "line-thin") {
      const pipeColor = definition.id === "pipe" ? ["match", ["to-number", ["get", "functionId"]], 1, "#2878c7", 2, "#16a085", 4, "#d59b15", 5, "#7546b8", 6, "#e24d42", 8, "#e24d42", 9, "#16a085", definition.color] : definition.color;
      return [{ ...common, id: `pwa-${definition.id}-line`, type: "line", paint: { "line-color": pipeColor, "line-width": ["interpolate", ["linear"], ["zoom"], definition.minzoom, definition.type === "line" ? 1.2 : .8, 18, definition.type === "line" ? 4.2 : 2.2], "line-opacity": .9 } }, labelLayer];
    }
    if (definition.type === "fill") return [{ ...common, id: `pwa-${definition.id}-fill`, type: "fill", paint: { "fill-color": definition.color, "fill-outline-color": "#56616b", "fill-opacity": .55 } }, labelLayer];
    const pointLayer = { ...common, id: `pwa-${definition.id}-point`, type: "circle", paint: { "circle-radius": ["interpolate", ["linear"], ["zoom"], definition.minzoom, 3, 18, 7], "circle-color": definition.color, "circle-stroke-color": "#ffffff", "circle-stroke-width": 1.2, "circle-opacity": .92 } };
    if (definition.id === "meter") pointLayer.filter = ["!", ["in", ["to-string", ["get", "custStat"]], ["literal", ["5", "6"]]]];
    return [pointLayer, labelLayer];
  }

  function addOSMBuildings() {
    if (!state.map.getSource(OSM_SOURCE)) state.map.addSource(OSM_SOURCE, { type: "vector", url: "https://tiles.openfreemap.org/planet" });
    const firstLabel = state.map.getStyle().layers.find((layer) => layer.type === "symbol");
    state.map.addLayer({
      id: "osm-3d-buildings", type: "fill-extrusion", source: OSM_SOURCE, "source-layer": "building", minzoom: 14,
      filter: ["!=", ["get", "hide_3d"], true],
      paint: {
        "fill-extrusion-color": "#d8d2c8", "fill-extrusion-opacity": .72,
        "fill-extrusion-height": ["interpolate", ["linear"], ["zoom"], 14, 0, 15, ["coalesce", ["to-number", ["get", "render_height"]], ["*", ["coalesce", ["to-number", ["get", "building:levels"]], ["to-number", ["get", "levels"]], 3], 3]]],
        "fill-extrusion-base": ["coalesce", ["to-number", ["get", "render_min_height"]], 0]
      }
    }, firstLabel && firstLabel.id);
  }

  function applyFilters() {
    const start = byId("mapStartDate").value, end = byId("mapEndDate").value;
    if (start && end && start > end) { setInlineMessage("วันที่เริ่มต้องไม่เกินวันที่สิ้นสุด"); return; }
    layerDefinitions.forEach((definition) => {
      const source = state.map && state.map.getSource(`pwa-${definition.id}`);
      if (source && typeof source.setTiles === "function") source.setTiles([tileURL(definition.id)]);
    });
    const branch = allowedOffices().find((office) => office.pwa_code === byId("mapBranch").value);
    if (branch && branch.lng != null && branch.lat != null) state.map.flyTo({ center: [branch.lng, branch.lat], zoom: 14 });
    setInlineMessage("ปรับตัวกรองแล้ว");
  }

  function clearFilters() {
    if (!byId("mapZone").disabled) byId("mapZone").value = "";
    updateBranchOptions();
    if (!byId("mapBranch").disabled) byId("mapBranch").value = "";
    byId("mapStartDate").value = ""; byId("mapEndDate").value = "";
    applyFilters();
  }

  function quickSearch() {
    const query = byId("quickSearch").value.trim();
    if (!query) { setInlineMessage("กรุณาระบุคำค้นหา"); return; }
    const coordinate = parseCoordinate(query);
    if (coordinate) { locate(coordinate, "พิกัดที่ค้นหา"); return; }
    const lower = query.toLocaleLowerCase("th");
    const office = allowedOffices().find((item) => String(item.pwa_code).toLowerCase().includes(lower) || String(item.name).toLocaleLowerCase("th").includes(lower));
    if (!office || office.lng == null || office.lat == null) { setInlineMessage("ไม่พบสาขาหรือพิกัดที่ค้นหา"); return; }
    byId("mapZone").value = office.zone; updateBranchOptions(); byId("mapBranch").value = office.pwa_code;
    locate([Number(office.lng), Number(office.lat)], `${office.pwa_code} — ${office.name}`);
  }

  function parseCoordinate(value) {
    const match = value.match(/^\s*(-?\d+(?:\.\d+)?)\s*[, ]\s*(-?\d+(?:\.\d+)?)\s*$/);
    if (!match) return null;
    const first = Number(match[1]), second = Number(match[2]);
    if (first >= 5 && first <= 22 && second >= 96 && second <= 107) return [second, first];
    if (first >= 96 && first <= 107 && second >= 5 && second <= 22) return [first, second];
    return null;
  }

  function locate(lngLat, label) {
    if (state.marker) state.marker.remove();
    state.marker = new maplibregl.Marker({ color: "#2f68b2" }).setLngLat(lngLat).setPopup(new maplibregl.Popup().setText(label)).addTo(state.map);
    state.marker.togglePopup(); state.map.flyTo({ center: lngLat, zoom: 14 }); setInlineMessage(label);
  }

  function switchBasemap(event) {
    if (!state.map || !state.map.isStyleLoaded()) return;
    const satellite = event.target.value === "satellite";
    state.originalLayers.forEach((layer) => {
      if (!state.map.getLayer(layer.id) || layer.type === "symbol") return;
      state.map.setLayoutProperty(layer.id, "visibility", satellite ? "none" : (layer.visibility || "visible"));
    });
    const satelliteLayer = state.config.google_enabled && state.map.getLayer("google-satellite") ? "google-satellite" : "esri-world-imagery";
    state.map.setLayoutProperty("esri-world-imagery", "visibility", satellite && satelliteLayer === "esri-world-imagery" ? "visible" : "none");
    if (state.map.getLayer("google-satellite")) state.map.setLayoutProperty("google-satellite", "visibility", satellite && satelliteLayer === "google-satellite" ? "visible" : "none");
  }

  function toggleTerrain(event) {
    if (!state.map || !state.map.isStyleLoaded()) return;
    if (event.target.checked) {
      if (!state.map.getSource("pwa-dem")) state.map.addSource("pwa-dem", { type: "raster-dem", url: state.config.dem_tilejson_url, tileSize: 256 });
      state.map.setTerrain({ source: "pwa-dem", exaggeration: 1.15 }); state.map.easeTo({ pitch: 55 });
    } else { state.map.setTerrain(null); state.map.easeTo({ pitch: 0, bearing: 0 }); }
  }

  function toggleBuildings(event) {
    if (state.map && state.map.getLayer("osm-3d-buildings")) state.map.setLayoutProperty("osm-3d-buildings", "visibility", event.target.checked ? "visible" : "none");
  }

  function setProjectLayerVisibility(layer, visible) {
    if (!state.map) return;
    state.map.getStyle().layers.filter((item) => item.id.startsWith(`pwa-${layer}-`)).forEach((item) => state.map.setLayoutProperty(item.id, "visibility", visible ? "visible" : "none"));
  }

  function showFeaturePopup(event) {
    const ids = state.map.getStyle().layers.filter((layer) => layer.id.startsWith("pwa-")).map((layer) => layer.id);
    const feature = state.map.queryRenderedFeatures(event.point, { layers: ids })[0];
    if (!feature) return;
    const table = document.createElement("table"); table.className = "feature-popup";
    const labels = { _layerName: "ชั้นข้อมูล", _pwaCode: "รหัสสาขา", _fid: "รหัสรายการ", dma_id: "DMA", diameter: "ขนาดท่อ", sizeId: "รหัสขนาด" };
    Object.entries(labels).forEach(([key, label]) => {
      if (feature.properties[key] == null || feature.properties[key] === "") return;
      const row = table.insertRow(); row.insertCell().textContent = label; row.insertCell().textContent = String(feature.properties[key]);
    });
    new maplibregl.Popup({ maxWidth: "320px" }).setLngLat(event.lngLat).setDOMContent(table).addTo(state.map);
  }

  async function askAssistant(event) {
    event.preventDefault();
    const prompt = byId("aiPrompt").value.trim(); if (!prompt) return;
    appendAI(`คุณ: ${prompt}`); byId("aiPrompt").value = "";
    try {
      const payload = await requestJSON("/api/chatbot/query", { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ prompt, pwa_code: byId("mapBranch").value }) });
      appendAI(`น้องหนึ่งน้ำ: ${formatAIResponse(payload)}`);
      if (payload.response_type === "geojson") renderAIGeoJSON(payload.result);
      else renderAIGeoJSON(payload.geojson || (payload.data && payload.data.geojson));
    } catch (error) { appendAI("น้องหนึ่งน้ำ: ยังเชื่อมต่อบริการ AI ไม่ได้ กรุณาลองใหม่ค่ะ"); }
  }

  function formatAIResponse(payload) {
    if (payload.text_response) return payload.text_response;
    if (payload.response_type === "numeric" && payload.result) {
      const value = payload.result.value == null ? "–" : Number(payload.result.value).toLocaleString("th-TH");
      return `${payload.result.label || "ผลลัพธ์"}: ${value}${payload.result.unit ? ` ${payload.result.unit}` : ""}`;
    }
    if (payload.response_type === "table" && payload.result) {
      const rows = Array.isArray(payload.result.rows) ? payload.result.rows : [];
      return rows.length ? rows.slice(0, 5).map((row) => Object.values(row).join(" · ")).join("\n") : "ไม่พบข้อมูลค่ะ";
    }
    if (payload.response_type === "geojson" && payload.result) {
      const count = Array.isArray(payload.result.features) ? payload.result.features.length : 0;
      return `แสดงผลบนแผนที่ ${count.toLocaleString("th-TH")} รายการแล้วค่ะ`;
    }
    return payload.message || payload.answer || payload.summary || "แสดงผลลัพธ์แล้วค่ะ";
  }

  function appendAI(message) { const p = document.createElement("p"); p.textContent = message; byId("aiMessages").append(p); byId("aiMessages").scrollTop = byId("aiMessages").scrollHeight; }
  function renderAIGeoJSON(data) {
    if (!data || !state.map) return;
    if (state.map.getLayer("ai-result-line")) state.map.removeLayer("ai-result-line");
    if (state.map.getLayer("ai-result-point")) state.map.removeLayer("ai-result-point");
    if (state.map.getSource("ai-result")) state.map.removeSource("ai-result");
    state.map.addSource("ai-result", { type: "geojson", data });
    state.map.addLayer({ id: "ai-result-line", source: "ai-result", type: "line", filter: ["in", ["geometry-type"], ["literal", ["LineString", "Polygon"]]], paint: { "line-color": "#e44768", "line-width": 4 } });
    state.map.addLayer({ id: "ai-result-point", source: "ai-result", type: "circle", filter: ["==", ["geometry-type"], "Point"], paint: { "circle-color": "#e44768", "circle-radius": 7, "circle-stroke-color": "#fff", "circle-stroke-width": 2 } });
  }

  function toggleSidebar(collapsed) { byId("mapSidebar").classList.toggle("collapsed", collapsed); setTimeout(() => state.map && state.map.resize(), 220); }
  function setInlineMessage(message) { byId("searchMessage").textContent = message; }
  function setStatus(message, stateName) { const node = byId("mapStatus"); node.textContent = message; node.className = `status-dot ${stateName || ""}`; }

  document.addEventListener("DOMContentLoaded", init);
})();
