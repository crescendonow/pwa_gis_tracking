/**
 * Chatbot "น้องหนึ่งน้ำ" — Text-to-Query GIS Assistant
 * Frontend logic for the floating chat panel on detail.html
 */

/* ─── State ──────────────────────────────── */
var chatbotOpen = false;
var chatbotBusy = false;
var chatMsgIdCounter = 0;
var _chatbotPendingGeoJSON = null;

/* ─── Toggle ─────────────────────────────── */
function toggleChatbot() {
    chatbotOpen = !chatbotOpen;
    var panel = document.getElementById('chatbotPanel');
    var fab = document.getElementById('chatbotFab');
    if (chatbotOpen) {
        panel.classList.add('open');
        fab.classList.remove('pulse');
        fab.querySelector('.fab-icon').className = 'fa-solid fa-xmark fab-icon';
        var input = document.getElementById('chatInput');
        if (input) setTimeout(function() { input.focus(); }, 100);
    } else {
        panel.classList.remove('open');
        fab.querySelector('.fab-icon').className = 'fa-solid fa-comments fab-icon';
    }
}

/* ─── Send Message ───────────────────────── */
async function sendChatMessage() {
    if (chatbotBusy) return;
    var input = document.getElementById('chatInput');
    var prompt = (input.value || '').trim();
    if (!prompt) return;
    input.value = '';

    // Show user message
    appendChatMsg('user', escapeHtmlChat(prompt));

    // Determine pwa_code from current page context
    var pwaCode = '';
    if (typeof selectedBranches !== 'undefined' && selectedBranches.length > 0) {
        pwaCode = selectedBranches[0];
    } else if (typeof userSession !== 'undefined' && userSession.pwa_code) {
        pwaCode = userSession.pwa_code;
    }

    // Show typing indicator
    chatbotBusy = true;
    var typingId = showTypingIndicator();
    var sendBtn = document.getElementById('chatSendBtn');
    if (sendBtn) sendBtn.disabled = true;

    try {
        var res = await fetch('/pwa_gis_tracking/api/chatbot/query', {
            method: 'POST',
            headers: { 'Content-Type': 'application/json' },
            body: JSON.stringify({ prompt: prompt, pwa_code: pwaCode })
        });

        removeTypingIndicator(typingId);

        if (!res.ok) {
            var errData = null;
            try { errData = await res.json(); } catch(e) {}
            var errMsg = 'เกิดข้อผิดพลาดค่ะ (HTTP ' + res.status + ')';
            if (errData) {
                if (typeof errData.detail === 'string') errMsg = errData.detail;
                else if (errData.detail && errData.detail.message) errMsg = errData.detail.message;
                else if (errData.message) errMsg = errData.message;
            }
            appendChatMsg('bot', escapeHtmlChat(errMsg));

            // If there's a query display in the error, show it
            if (errData && errData.detail && errData.detail.query_display) {
                appendQueryBlock(
                    errData.detail.query_display.type === 'sql' ? 'SQL Query' : 'MongoDB Pipeline',
                    errData.detail.query_display.code
                );
            }
            return;
        }

        var data = await res.json();
        renderChatbotResponse(data);

    } catch (e) {
        removeTypingIndicator(typingId);
        appendChatMsg('bot', 'เกิดข้อผิดพลาดในการเชื่อมต่อ กรุณาลองใหม่อีกครั้งค่ะ');
    } finally {
        chatbotBusy = false;
        if (sendBtn) sendBtn.disabled = false;
    }
}

/* ─── Render Bot Response ────────────────── */
function renderChatbotResponse(data) {
    if (!data) {
        appendChatMsg('bot', 'ไม่พบข้อมูลค่ะ');
        return;
    }

    // 1. Show text response (Thai explanation from LLM)
    if (data.text_response) {
        appendChatMsg('bot', escapeHtmlChat(data.text_response));
    }

    // 2. Show generated query for verification
    if (data.query_display && data.query_display.code) {
        var label = 'Query';
        if (data.query_display.type === 'sql') label = 'SQL Query';
        else if (data.query_display.type === 'mongodb') label = 'MongoDB Pipeline';
        else if (data.query_display.type === 'sql+mongodb') label = 'SQL + MongoDB';
        appendQueryBlock(label, data.query_display.code);
    }

    // 3. Render based on response_type
    var result = data.result;
    if (!result) {
        appendChatMsg('bot', 'ไม่พบข้อมูลค่ะ');
        return;
    }

    if (data.response_type === 'geojson') {
        // GeoJSON FeatureCollection
        var features = result.features || [];
        if (features.length === 0) {
            appendChatMsg('bot', 'ไม่พบข้อมูลตำแหน่งที่ตรงตามเงื่อนไขค่ะ');
            return;
        }

        // Check if map is available (detail page with layer loaded)
        var mapAvailable = (typeof detailMap !== 'undefined' && detailMap) ||
                           (document.getElementById('detailMapContainer') && typeof ensureMap === 'function');

        if (mapAvailable) {
            appendChatMsg('bot',
                'พบข้อมูล <strong>' + features.length.toLocaleString('th-TH') + '</strong> รายการค่ะ ' +
                '<span class="chat-map-link" onclick="scrollToMap()"><i class="fa-solid fa-map-location-dot"></i> ดูบนแผนที่</span>'
            );
            try {
                renderChatbotGeoJSON(result);
            } catch (mapErr) {
                console.warn('[Chatbot] Map render error:', mapErr);
                _chatbotPendingGeoJSON = result;
                appendChatMsg('bot',
                    '<span style="color:var(--text-muted);font-size:12px;">' +
                    '<i class="fa-solid fa-circle-info" style="margin-right:4px;"></i>' +
                    'ไม่สามารถแสดงแผนที่ได้ในขณะนี้ กรุณากด "ดูบนแผนที่" อีกครั้งค่ะ</span>'
                );
            }
        } else {
            appendChatMsg('bot',
                'พบข้อมูล <strong>' + features.length.toLocaleString('th-TH') + '</strong> รายการค่ะ<br>' +
                '<span style="color:var(--text-muted);font-size:12px;">' +
                '<i class="fa-solid fa-circle-info" style="margin-right:4px;"></i>' +
                'กรุณาเปิดหน้า "รายละเอียด" และเลือก Layer ก่อน เพื่อแสดงตำแหน่งบนแผนที่</span>'
            );
        }

    } else if (data.response_type === 'numeric') {
        renderChatNumeric(result);

    } else if (data.response_type === 'table') {
        var rows = result.rows || [];
        if (rows.length === 0) {
            appendChatMsg('bot', 'ไม่พบข้อมูลที่ตรงตามเงื่อนไขค่ะ');
            return;
        }
        appendChatMsg('bot', 'พบข้อมูล <strong>' + (result.row_count || rows.length).toLocaleString('th-TH') + '</strong> รายการค่ะ');
        renderChatTable(result.columns || [], rows);

    } else {
        appendChatMsg('bot', 'ได้ผลลัพธ์แล้วค่ะ');
    }

    // Show metadata
    if (data.metadata) {
        var meta = data.metadata;
        var metaParts = [];
        if (meta.execution_time_ms) metaParts.push(meta.execution_time_ms + 'ms');
        if (meta.cached) metaParts.push('cached');
        if (meta.model === 'rule-based') metaParts.push('rule-based');
        else if (meta.model) metaParts.push('LLM');
        if (metaParts.length > 0) {
            appendChatMeta(metaParts.join(' · '));
        }
    }
}

/* ─── Render Helpers ─────────────────────── */

function appendChatMsg(role, html) {
    var container = document.getElementById('chatMessages');
    var div = document.createElement('div');
    div.className = 'chat-msg ' + role;
    div.innerHTML = '<div class="chat-bubble">' + html + '</div>';
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
}

function appendChatMeta(text) {
    var container = document.getElementById('chatMessages');
    var div = document.createElement('div');
    div.style.cssText = 'text-align:right;font-size:10px;color:var(--text-muted);margin-top:-4px;padding-right:4px;';
    div.textContent = text;
    container.appendChild(div);
}

function appendQueryBlock(label, code) {
    var container = document.getElementById('chatMessages');
    var div = document.createElement('div');
    div.className = 'chat-msg bot';
    div.innerHTML =
        '<div class="chat-bubble" style="padding:8px 12px;width:100%">' +
            '<div class="chat-query-label">' + escapeHtmlChat(label) + '</div>' +
            '<div class="chat-query-block">' + escapeHtmlChat(code) + '</div>' +
        '</div>';
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
}

function renderChatNumeric(result) {
    var container = document.getElementById('chatMessages');
    var div = document.createElement('div');
    div.className = 'chat-msg bot';
    var val = result.value;
    if (typeof val === 'number') val = val.toLocaleString('th-TH');
    div.innerHTML =
        '<div class="chat-bubble" style="padding:8px 12px">' +
            '<div class="chat-numeric">' +
                '<div class="chat-numeric-value">' + escapeHtmlChat(String(val)) + '</div>' +
                '<div class="chat-numeric-unit">' + escapeHtmlChat(result.unit || '') + '</div>' +
            '</div>' +
            '<div class="chat-numeric-label">' + escapeHtmlChat(result.label || '') + '</div>' +
        '</div>';
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
}

function renderChatTable(columns, rows) {
    var container = document.getElementById('chatMessages');
    var div = document.createElement('div');
    div.className = 'chat-msg bot';

    var html = '<div class="chat-bubble" style="padding:8px 10px;width:100%"><div class="chat-table-wrapper"><table class="chat-table"><thead><tr>';
    for (var i = 0; i < columns.length; i++) {
        html += '<th>' + escapeHtmlChat(columns[i]) + '</th>';
    }
    html += '</tr></thead><tbody>';
    var maxRows = Math.min(rows.length, 50);
    for (var r = 0; r < maxRows; r++) {
        html += '<tr>';
        for (var c = 0; c < columns.length; c++) {
            var val = rows[r][columns[c]];
            if (val === null || val === undefined) val = '';
            html += '<td>' + escapeHtmlChat(String(val)) + '</td>';
        }
        html += '</tr>';
    }
    html += '</tbody></table></div>';
    if (rows.length > 50) {
        html += '<div style="font-size:11px;color:var(--text-muted);margin-top:4px;">แสดง 50 จาก ' + rows.length + ' รายการ</div>';
    }
    html += '</div>';

    div.innerHTML = html;
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
}

function showTypingIndicator() {
    var container = document.getElementById('chatMessages');
    var id = 'typing-' + (++chatMsgIdCounter);
    var div = document.createElement('div');
    div.id = id;
    div.className = 'chat-msg bot';
    div.innerHTML = '<div class="chat-bubble"><div class="chat-typing"><span></span><span></span><span></span></div></div>';
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
    return id;
}

function removeTypingIndicator(id) {
    var el = document.getElementById(id);
    if (el) el.remove();
}

function escapeHtmlChat(str) {
    var div = document.createElement('div');
    div.appendChild(document.createTextNode(str));
    return div.innerHTML;
}

/* ─── Map Integration ────────────────────── */

function renderChatbotGeoJSON(geojson) {
    // Ensure map exists and is visible
    var mapContainer = document.getElementById('detailMapContainer');
    if (mapContainer) mapContainer.style.display = 'block';

    // Wait for map if not yet initialized
    if (typeof detailMap === 'undefined' || !detailMap) {
        // Try to use existing ensureMap function from detail.js
        if (typeof ensureMap === 'function') {
            ensureMap();
        }
        // If map still not available, try again after a short delay
        if (!detailMap) {
            setTimeout(function() {
                if (typeof detailMap !== 'undefined' && detailMap) {
                    _addChatbotLayer(geojson);
                } else {
                    appendChatMsg('bot',
                        '<span style="color:var(--text-muted);font-size:12px;">' +
                        '<i class="fa-solid fa-circle-info" style="margin-right:4px;"></i>' +
                        'กรุณาเลือก Layer เพื่อเปิดแผนที่ก่อน จากนั้นลองถามคำถามอีกครั้งค่ะ</span>'
                    );
                }
            }, 800);
            return;
        }
    }

    _addChatbotLayer(geojson);
}

function _addChatbotLayer(geojson) {
    var map = (typeof detailMap !== 'undefined') ? detailMap : null;
    if (!map) return;

    function _doAdd() {
        // Remove previous chatbot layers
        var layerIds = ['chatbot-result-point', 'chatbot-result-line', 'chatbot-result-fill', 'chatbot-result-outline'];
        for (var i = 0; i < layerIds.length; i++) {
            if (map.getLayer(layerIds[i])) map.removeLayer(layerIds[i]);
        }
        if (map.getSource('chatbot-result')) map.removeSource('chatbot-result');

        // Add source
        map.addSource('chatbot-result', { type: 'geojson', data: geojson });

        // Point layer
        map.addLayer({
            id: 'chatbot-result-point',
            type: 'circle',
            source: 'chatbot-result',
            filter: ['==', '$type', 'Point'],
            paint: {
                'circle-radius': 6,
                'circle-color': '#FF6B6B',
                'circle-stroke-width': 2,
                'circle-stroke-color': '#ffffff'
            }
        });

        // Line layer
        map.addLayer({
            id: 'chatbot-result-line',
            type: 'line',
            source: 'chatbot-result',
            filter: ['==', '$type', 'LineString'],
            paint: {
                'line-color': '#FF6B6B',
                'line-width': 3
            }
        });

        // Polygon fill
        map.addLayer({
            id: 'chatbot-result-fill',
            type: 'fill',
            source: 'chatbot-result',
            filter: ['==', '$type', 'Polygon'],
            paint: {
                'fill-color': '#FF6B6B',
                'fill-opacity': 0.25
            }
        });

        // Polygon outline
        map.addLayer({
            id: 'chatbot-result-outline',
            type: 'line',
            source: 'chatbot-result',
            filter: ['==', '$type', 'Polygon'],
            paint: {
                'line-color': '#FF6B6B',
                'line-width': 2
            }
        });

        // Add click popup for chatbot results
        map.on('click', 'chatbot-result-point', function(e) { _chatbotPopup(e, map); });
        map.on('click', 'chatbot-result-line', function(e) { _chatbotPopup(e, map); });
        map.on('click', 'chatbot-result-fill', function(e) { _chatbotPopup(e, map); });

        // Change cursor on hover
        ['chatbot-result-point', 'chatbot-result-line', 'chatbot-result-fill'].forEach(function(lid) {
            map.on('mouseenter', lid, function() { map.getCanvas().style.cursor = 'pointer'; });
            map.on('mouseleave', lid, function() { map.getCanvas().style.cursor = ''; });
        });

        // Fit bounds
        _fitChatbotBounds(map, geojson);
    }

    // Wait for map style to load before adding layers
    if (map.isStyleLoaded()) {
        _doAdd();
    } else {
        map.once('load', function() { _doAdd(); });
    }
}

function _chatbotPopup(e, map) {
    if (!e.features || !e.features.length) return;
    var props = e.features[0].properties;
    var html = '<div style="padding:10px;max-height:300px;overflow-y:auto">';
    html += '<div style="font-weight:600;margin-bottom:8px;color:var(--pwa-gold);font-size:13px">ผลลัพธ์ Chatbot</div>';
    for (var key in props) {
        if (props.hasOwnProperty(key)) {
            html += '<div style="display:flex;justify-content:space-between;gap:12px;padding:3px 0;border-bottom:1px solid var(--border)">';
            html += '<span style="color:var(--text-muted);font-size:11px;white-space:nowrap">' + escapeHtmlChat(key) + '</span>';
            html += '<span style="color:var(--text-primary);font-size:11px;text-align:right">' + escapeHtmlChat(String(props[key] || '')) + '</span>';
            html += '</div>';
        }
    }
    html += '</div>';

    var popup = (typeof mapPopup !== 'undefined' && mapPopup) ? mapPopup : new maplibregl.Popup({ maxWidth: '360px' });
    popup.setLngLat(e.lngLat).setHTML(html).addTo(map);
}

function _fitChatbotBounds(map, geojson) {
    if (!geojson.features || geojson.features.length === 0) return;
    var bounds = new maplibregl.LngLatBounds();
    geojson.features.forEach(function(f) {
        if (f.geometry && f.geometry.coordinates) {
            _extendBoundsRecursive(bounds, f.geometry.coordinates);
        }
    });
    if (!bounds.isEmpty()) {
        map.fitBounds(bounds, { padding: 50, maxZoom: 16, duration: 800 });
    }
}

function _extendBoundsRecursive(bounds, coords) {
    if (typeof coords[0] === 'number') {
        bounds.extend(coords);
    } else {
        for (var i = 0; i < coords.length; i++) {
            _extendBoundsRecursive(bounds, coords[i]);
        }
    }
}

function scrollToMap() {
    var mapEl = document.getElementById('detailMapContainer');
    if (mapEl) {
        mapEl.style.display = 'block';
        mapEl.scrollIntoView({ behavior: 'smooth', block: 'center' });
    }
    // Retry pending GeoJSON that failed to render earlier
    if (_chatbotPendingGeoJSON) {
        var pending = _chatbotPendingGeoJSON;
        _chatbotPendingGeoJSON = null;
        try {
            renderChatbotGeoJSON(pending);
        } catch (e) {
            console.warn('[Chatbot] Retry map render failed:', e);
            appendChatMsg('bot',
                '<span style="color:var(--text-muted);font-size:12px;">' +
                '<i class="fa-solid fa-triangle-exclamation" style="margin-right:4px;"></i>' +
                'ไม่สามารถแสดงข้อมูลบนแผนที่ได้ กรุณาลองเปิด Layer ใหม่แล้วถามอีกครั้งค่ะ</span>'
            );
        }
    }
}

/* ─── Suggested Question Click ───────────── */
function askSuggestion(text) {
    var input = document.getElementById('chatInput');
    if (input) {
        input.value = text;
        sendChatMessage();
    }
}
