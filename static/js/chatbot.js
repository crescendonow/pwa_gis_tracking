/**
 * Chatbot "น้องหนึ่งน้ำ" — Text-to-Query GIS Assistant
 * Frontend logic for the floating chat panel on detail.html
 */

/* ─── State ──────────────────────────────── */
var chatbotOpen = false;
var chatbotBusy = false;
var chatMsgIdCounter = 0;
var _chatbotPendingGeoJSON = null;
var _chatbotLastPrompt = '';

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
// promptOverride: ถ้าไม่ระบุ จะอ่านจาก #chatInput เอง (การพิมพ์ถามปกติ)
// forceLLM: true = ข้าม rule parser แล้วให้ LLM ตอบโดยตรง (ปุ่ม "ถามใหม่ด้วย AI")
async function sendChatMessage(promptOverride, forceLLM) {
    if (chatbotBusy) return;
    var input = document.getElementById('chatInput');
    var prompt;
    if (typeof promptOverride === 'string' && promptOverride) {
        prompt = promptOverride;
    } else {
        prompt = (input.value || '').trim();
        if (!prompt) return;
        input.value = '';
        // Show user message
        appendChatMsg('user', escapeHtmlChat(prompt));
    }
    _chatbotLastPrompt = prompt;

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
            body: JSON.stringify({ prompt: prompt, pwa_code: pwaCode, force_llm: !!forceLLM })
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
        // GeoJSON FeatureCollection — แสดงบนแผนที่เสมอ แม้ผู้ใช้ยังไม่เคยกด "ค้นหา"/เปิด Layer มาก่อน
        var features = result.features || [];
        if (features.length === 0) {
            appendChatMsg('bot', 'ไม่พบข้อมูลตำแหน่งที่ตรงตามเงื่อนไขค่ะ');
        } else {
            var totalCount = (typeof result.total_count === 'number') ? result.total_count : features.length;
            var countMsg;
            if (result.truncated) {
                countMsg = 'พบข้อมูล <strong>' + totalCount.toLocaleString('th-TH') + '</strong> รายการ ' +
                    '(แสดง <strong>' + features.length.toLocaleString('th-TH') + '</strong> รายการแรกบนแผนที่) ค่ะ ';
            } else {
                countMsg = 'พบข้อมูล <strong>' + totalCount.toLocaleString('th-TH') + '</strong> รายการค่ะ ';
            }
            appendChatMsg('bot', countMsg +
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

        // คำตอบมาจาก rule parser และน่าจะลองถาม LLM เพิ่มได้ → แสดงปุ่ม "ถามใหม่ด้วย AI"
        if (meta.can_retry_llm) {
            appendRetryLLM();
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

function appendRetryLLM() {
    var container = document.getElementById('chatMessages');
    var div = document.createElement('div');
    div.className = 'chat-msg bot';
    div.innerHTML =
        '<div class="chat-bubble" style="padding:8px 12px">' +
            '<div style="font-size:12px;color:var(--text-muted);margin-bottom:6px">' +
                'คำตอบนี้มาจากกฎอัตโนมัติ ถ้ายังไม่ตรงกับที่ต้องการ ลองให้ AI ช่วยวิเคราะห์อีกครั้งค่ะ' +
            '</div>' +
            '<button class="chat-retry-llm" onclick="retryWithLLM()">' +
                '<i class="fa-solid fa-wand-magic-sparkles"></i> ถามใหม่ด้วย AI' +
            '</button>' +
        '</div>';
    container.appendChild(div);
    container.scrollTop = container.scrollHeight;
}

function retryWithLLM() {
    if (!_chatbotLastPrompt || chatbotBusy) return;
    appendChatMsg('user', escapeHtmlChat('🤖 ' + _chatbotLastPrompt + ' (ถามด้วย AI)'));
    sendChatMessage(_chatbotLastPrompt, true);
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

/**
 * เปิดแผนที่ให้มองเห็นได้เสมอ แม้ผู้ใช้จะยังไม่เคยกด "ค้นหา"/เปิด Layer มาก่อน
 * #detailMapContainer เป็นลูกของ #resultsSection ซึ่งซ่อนอยู่ (display:none) จนกว่าจะค้นหาจริง
 * ฟังก์ชันนี้เปิดทั้งพ่อและลูก แล้ว resize() แผนที่ (ที่อาจถูกสร้างไว้ตั้งแต่ container ยังซ่อนอยู่
 * ทำให้ canvas มีขนาด 0) ให้ตรงกับขนาดจริงก่อนแสดงผล — ถ้ายังไม่เคยค้นหาจริง จะโชว์เฉพาะการ์ดแผนที่
 * (ซ่อนการ์ดสถิติ/กราฟ/ตาราง/export ที่ยังว่างอยู่ ด้วย class 'chatbot-map-only')
 */
function _ensureChatbotMapVisible() {
    var results = document.getElementById('resultsSection');
    var empty = document.getElementById('emptyState');
    var mapBox = document.getElementById('detailMapContainer');
    if (!results || !mapBox) return null;

    var hasRealSearch = (typeof mapLoadedSources !== 'undefined' && mapLoadedSources.length > 0);
    if (!hasRealSearch) results.classList.add('chatbot-map-only');

    results.style.display = 'block';
    if (empty) empty.style.display = 'none';
    mapBox.style.display = 'block';

    var map = (typeof ensureMap === 'function') ? ensureMap() : null;
    if (map) {
        // Resize ทันที (synchronous) — ต้องทำก่อนเพิ่ม layer/fitBounds ไม่งั้นจะคำนวณจาก canvas ขนาด 0
        try { map.resize(); } catch (e) {}
        // เผื่อ layout ยังไม่นิ่ง (เช่น font/CSS transition ของการ์ดที่เพิ่งโผล่) — resize ซ้ำอีกครั้งหลัง 60ms
        setTimeout(function () { try { map.resize(); } catch (e) {} }, 60);
    }
    return map;
}

function renderChatbotGeoJSON(geojson) {
    var map = _ensureChatbotMapVisible();
    if (!map) {
        appendChatMsg('bot',
            '<span style="color:var(--text-muted);font-size:12px;">' +
            '<i class="fa-solid fa-circle-info" style="margin-right:4px;"></i>' +
            'ไม่สามารถเปิดแผนที่ได้ในขณะนี้ค่ะ กรุณาลองใหม่อีกครั้ง</span>'
        );
        return;
    }
    _addChatbotLayer(geojson, map);
}

function _addChatbotLayer(geojson, map) {
    map = map || ((typeof detailMap !== 'undefined') ? detailMap : null);
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
    // เผื่อผู้ใช้กด "ดูบนแผนที่" ก่อนที่แผนที่จะทันเปิด (เช่น renderChatbotGeoJSON ก่อนหน้าล้มเหลว)
    _ensureChatbotMapVisible();
    var mapEl = document.getElementById('detailMapContainer');
    if (mapEl) mapEl.scrollIntoView({ behavior: 'smooth', block: 'center' });

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
