let container = null;
let historyMode = false;

export function init(el) {
    container = el;
    historyMode = false;
    loadData();
}

export function onMessage(msg) {
    if (!container) return;
    if (msg.event === 'gps' || msg.event === 'region_enter' || msg.event === 'region_exit') {
        if (!historyMode) loadData();
    }
}

function loadData() {
    fetch('/api/tabs/gps/action/get-current', {method: 'POST'})
        .then(r => r.json())
        .then(data => {
            render(data);
            if (data.location) loadMap();
        })
        .catch(err => console.error('GPS: failed to load data', err));
}

function loadMap() {
    fetch('/api/tabs/gps/action/get-map', {method: 'POST'})
        .then(r => r.json())
        .then(data => {
            if (!data.map) return;
            const img = container.querySelector('#gps-map');
            if (img) img.src = 'data:image/png;base64,' + data.map;
        })
        .catch(err => console.error('GPS: failed to load map', err));
}

function loadHistoryMap() {
    const placeholder = container.querySelector('#gps-history-map');
    if (placeholder) placeholder.style.opacity = '0.3';
    fetch('/api/tabs/gps/action/get-history-map', {method: 'POST'})
        .then(r => r.json())
        .then(data => {
            const img = container.querySelector('#gps-history-map');
            if (!img) return;
            if (data.map) {
                img.src = 'data:image/png;base64,' + data.map;
                img.style.opacity = '1';
            } else {
                img.style.display = 'none';
                const p = container.querySelector('#gps-history-empty');
                if (p) p.style.display = '';
            }
        })
        .catch(err => console.error('GPS: failed to load history map', err));
}

function toggleHistory() {
    historyMode = !historyMode;
    if (historyMode) {
        fetch('/api/tabs/gps/action/get-current', {method: 'POST'})
            .then(r => r.json())
            .then(data => renderHistory(data))
            .catch(err => console.error('GPS: failed to load data for history', err));
    } else {
        loadData();
    }
}

function formatTs(isoStr) {
    if (!isoStr) return '—';
    const d = new Date(isoStr);
    return d.toLocaleString('en-US', {
        month: 'short', day: 'numeric',
        hour: '2-digit', minute: '2-digit', second: '2-digit',
        hour12: true
    });
}

function formatCoords(lat, lon) {
    if (lat == null || lon == null) return '—';
    const latStr = `${Math.abs(lat).toFixed(5)}° ${lat >= 0 ? 'N' : 'S'}`;
    const lonStr = `${Math.abs(lon).toFixed(5)}° ${lon >= 0 ? 'E' : 'W'}`;
    return `${latStr}, ${lonStr}`;
}

function mapsLink(lat, lon) {
    if (lat == null || lon == null) return '';
    const url = `maps://maps.apple.com/?q=${lat},${lon}`;
    return `<a href="${url}" style="color: var(--accent-blue); font-size: 0.85em; margin-left: 8px;">Open in Maps ↗</a>`;
}

function render(data) {
    if (!container) return;

    const loc = data.location;
    const events = data.events || [];

    let html = '<div style="padding: 16px; max-width: 800px;">';

    // Toolbar
    html += '<div style="display: flex; align-items: center; justify-content: flex-end; margin-bottom: 16px;">';
    html += `<button id="gps-history-btn" onclick="(function(){window._gpsToggleHistory();})()" style="background: var(--accent); color: #fff; border: none; border-radius: 6px; padding: 6px 16px; font-size: 0.88em; cursor: pointer; font-weight: 500;">History</button>`;
    html += '</div>';

    // Current location card
    html += '<div style="margin-bottom: 24px;">';
    html += '<h3 style="margin: 0 0 12px; font-size: 1em; text-transform: uppercase; opacity: 0.6; letter-spacing: 0.05em;">Current Location</h3>';
    if (loc) {
        html += '<div style="background: var(--task-row-bg); border: 1px solid var(--task-row-border); border-radius: 8px; padding: 16px;">';
        html += `<div style="display: flex; align-items: center; flex-wrap: wrap; gap: 8px; margin-bottom: 8px;">`;
        html += `<span style="font-size: 1.1em; font-weight: 600;">${formatCoords(loc.lat, loc.lon)}</span>`;
        html += mapsLink(loc.lat, loc.lon);
        html += `</div>`;
        html += `<div style="display: flex; flex-wrap: wrap; gap: 16px; font-size: 0.88em; opacity: 0.75;">`;
        if (loc.device) html += `<span>Device: <strong>${escHtml(loc.device)}</strong></span>`;
        if (loc.alt) html += `<span>Alt: <strong>${loc.alt.toFixed(0)} m</strong></span>`;
        if (loc.acc) html += `<span>Accuracy: <strong>±${loc.acc.toFixed(0)} m</strong></span>`;
        if (loc.batt) html += `<span>Battery: <strong>${loc.batt.toFixed(0)}%</strong></span>`;
        html += `</div>`;
        html += `<div style="margin-top: 8px; font-size: 0.82em; opacity: 0.5;">Updated ${formatTs(loc.timestamp)}</div>`;
        html += `<div style="margin-top: 12px; border-radius: 6px; overflow: hidden; line-height: 0;">`;
        html += `<img id="gps-map" alt="Map" style="width: 100%; max-width: 768px; border-radius: 6px; opacity: 0.5;" onload="this.style.opacity=1" />`;
        html += `</div>`;
        html += '</div>';
    } else {
        html += '<div style="opacity: 0.5; padding: 12px;">No location data yet.</div>';
    }
    html += '</div>';

    // Region events
    html += '<div>';
    html += '<h3 style="margin: 0 0 12px; font-size: 1em; text-transform: uppercase; opacity: 0.6; letter-spacing: 0.05em;">Recent Region Events</h3>';
    if (events.length === 0) {
        html += '<div style="opacity: 0.5; padding: 12px;">No region events yet.</div>';
    } else {
        html += '<div style="display: flex; flex-direction: column; gap: 6px;">';
        for (const ev of events) {
            const isEnter = ev.event_type === 'enter';
            const icon = isEnter ? '▶' : '◀';
            const color = isEnter ? 'var(--accent-green, #4caf50)' : 'var(--accent-pink, #e91e63)';
            const label = isEnter ? 'Entered' : 'Exited';
            html += `<div style="display: flex; align-items: center; gap: 12px; padding: 10px 14px; background: var(--task-row-bg); border: 1px solid var(--task-row-border); border-radius: 6px; font-size: 0.9em;">`;
            html += `<span style="color: ${color}; font-size: 1.1em; width: 16px; text-align: center;">${icon}</span>`;
            html += `<div style="flex: 1; min-width: 0;">`;
            html += `<span style="color: ${color}; font-weight: 600;">${label}</span> <span style="font-weight: 500;">${escHtml(ev.region)}</span>`;
            if (ev.device) html += ` <span style="opacity: 0.5; font-size: 0.85em;">via ${escHtml(ev.device)}</span>`;
            html += `</div>`;
            html += `<span style="opacity: 0.45; font-size: 0.82em; white-space: nowrap;">${formatTs(ev.timestamp)}</span>`;
            html += `</div>`;
        }
        html += '</div>';
    }
    html += '</div>';

    html += '</div>';
    container.innerHTML = html;

    window._gpsToggleHistory = toggleHistory;
}

function renderHistory(data) {
    if (!container) return;
    const loc = data.location;

    let html = '<div style="padding: 16px; max-width: 800px;">';

    // Toolbar
    html += '<div style="display: flex; align-items: center; gap: 12px; margin-bottom: 16px;">';
    html += '<h3 style="margin: 0; font-size: 1em; text-transform: uppercase; opacity: 0.6; letter-spacing: 0.05em; flex: 1;">GPS History</h3>';
    html += `<button onclick="(function(){window._gpsToggleHistory();})()" style="background: var(--task-row-bg); color: var(--text); border: 1px solid var(--task-row-border); border-radius: 6px; padding: 6px 16px; font-size: 0.88em; cursor: pointer;">← Current</button>`;
    html += '</div>';

    html += '<div style="background: var(--task-row-bg); border: 1px solid var(--task-row-border); border-radius: 8px; padding: 16px;">';

    // Legend
    html += '<div style="display: flex; align-items: center; gap: 16px; font-size: 0.82em; opacity: 0.7; margin-bottom: 12px;">';
    html += '<span style="display:flex;align-items:center;gap:4px;"><span style="width:10px;height:10px;border-radius:50%;background:#22c55e;display:inline-block;"></span> Start</span>';
    html += '<span style="display:flex;align-items:center;gap:4px;"><span style="width:10px;height:10px;border-radius:50%;background:#ef4444;display:inline-block;"></span> End</span>';
    html += '<span style="display:flex;align-items:center;gap:4px;"><span style="width:10px;height:3px;background:#8b5cf6;display:inline-block;border-radius:2px;"></span> Track</span>';
    html += '</div>';

    html += `<div style="border-radius: 6px; overflow: hidden; line-height: 0; position: relative;">`;
    html += `<img id="gps-history-map" alt="History Map" style="width: 100%; max-width: 100%; border-radius: 6px; opacity: 0.3;" onload="this.style.opacity=1" />`;
    html += `<div id="gps-history-empty" style="display:none; padding: 12px; opacity: 0.5;">No GPS history available.</div>`;
    html += `</div>`;

    if (loc) {
        html += `<div style="margin-top: 10px; font-size: 0.82em; opacity: 0.5;">Most recent: ${formatCoords(loc.lat, loc.lon)} · ${formatTs(loc.timestamp)}</div>`;
    }

    html += '</div>';
    html += '</div>';

    container.innerHTML = html;
    window._gpsToggleHistory = toggleHistory;

    // Trigger history map load after render
    loadHistoryMap();
}

function escHtml(str) {
    if (!str) return '';
    return str.replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
