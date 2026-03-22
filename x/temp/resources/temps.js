let tempsContainer = null;
let canvas = null;
let ctx = null;
const history = [];

// Color palette for different sensors
const PALETTE = [
    '#ff6b6b', '#4ecdc4', '#45b7d1', '#96ceb4',
    '#ffeaa7', '#dfe6e9', '#a29bfe', '#fd79a8',
    '#6c5ce7', '#00b894', '#fd79a8', '#fdcb6e'
];
const serviceColors = {};
let colorIndex = 0;
let metricConfigs = {}; // loaded from backend; key = serviceName

function getServiceColor(serviceName) {
    if (metricConfigs[serviceName]?.color) return metricConfigs[serviceName].color;
    if (!serviceColors[serviceName]) {
        serviceColors[serviceName] = PALETTE[colorIndex % PALETTE.length];
        colorIndex++;
    }
    return serviceColors[serviceName];
}

function getServiceLabel(serviceName) {
    return metricConfigs[serviceName]?.displayName || serviceName;
}

export async function init(container) {
    tempsContainer = container;
    canvas = container.querySelector('#temps-chart');
    if (canvas) {
        ctx = canvas.getContext('2d');
    }

    // ResizeObserver for responsive canvas
    const ro = new ResizeObserver(() => resizeCanvas());
    ro.observe(tempsContainer);

    // Load saved configs before rendering
    try {
        const res = await fetch('/api/tabs/temps/action/get-metric-configs', {method: 'POST'});
        if (res.ok) {
            const data = await res.json();
            metricConfigs = data.configs || {};
        }
    } catch (e) { /* use defaults */
    }

    await refreshTemps();
}

function resizeCanvas() {
    if (!canvas || !tempsContainer) return;
    const dpr = window.devicePixelRatio || 1;
    const cssW = tempsContainer.offsetWidth || 800;
    const cssH = 400;
    canvas.style.width = cssW + 'px';
    canvas.style.height = cssH + 'px';
    canvas.width = Math.round(cssW * dpr);
    canvas.height = Math.round(cssH * dpr);
    render();
}

export function onMessage(msg) {
    if (!tempsContainer) return;

    // Only process temp-type messages; JSON field is "data-type" (hyphenated)
    if (msg['data-type'] !== 'core.temp.v1') return;

    // Check if the temps tab content is visible
    const tabContent = tempsContainer.closest('.tab-content');
    if (!tabContent || !tabContent.classList.contains('active')) {
        return;
    }

    // When a new temp message arrives and tab is active, add it to the chart
    if (msg.data && msg.data.tempF !== undefined) {
        history.push({
            timestamp: new Date(msg.timestamp || new Date()),
            tempF: msg.data.tempF,
            tempC: msg.data.tempC,
            serviceName: msg.serviceName
        });

        // Redraw the chart with updated data (resizeCanvas ensures canvas is ready and calls render)
        resizeCanvas();
    }
}

async function refreshTemps() {
    if (!tempsContainer) return;

    try {
        const response = await fetch('/api/tabs/temps/action/fetch-temps', {
            method: 'POST',
        });

        if (!response.ok) {
            tempsContainer.innerHTML = `<div class="error">Error loading temps: ${response.statusText}</div>`;
            return;
        }

        const result = await response.json();
        const readings = result.readings || [];

        if (readings.length === 0) {
            tempsContainer.innerHTML = '<div class="no-temps">No temperature data available</div>';
            return;
        }

        // Ensure canvas is set up
        if (!canvas) {
            canvas = tempsContainer.querySelector('#temps-chart');
            if (canvas) ctx = canvas.getContext('2d');
        }

        // Store readings as history for rendering
        history.length = 0;
        readings.forEach(r => {
            history.push({
                timestamp: new Date(r.timestamp),
                tempF: parseFloat(r.tempF),
                tempC: parseFloat(r.tempC),
                serviceName: r.serviceName
            });
        });

        ensureSettingsPanel();
        resizeCanvas();
    } catch (err) {
        console.error('Failed to refresh temps:', err);
        tempsContainer.innerHTML = `<div class="error">Error loading temps: ${err.message}</div>`;
    }
}

// --- Settings panel ---

function ensureSettingsPanel() {
    if (tempsContainer.querySelector('#temps-settings')) return;

    const panel = document.createElement('div');
    panel.id = 'temps-settings';
    panel.style.cssText = 'padding: 6px 0; font-size: 0.8rem;';
    panel.innerHTML = `
        <button id="temps-settings-toggle" style="background:none;border:none;color:var(--text);opacity:0.6;cursor:pointer;font-size:0.8rem;padding:2px 6px;">⚙ Sensors</button>
        <div id="temps-settings-panel" style="display:none;margin-top:8px;"></div>
    `;
    tempsContainer.appendChild(panel);

    panel.querySelector('#temps-settings-toggle').addEventListener('click', () => {
        const p = panel.querySelector('#temps-settings-panel');
        if (p.style.display === 'none') {
            renderSettingsPanel(p);
            p.style.display = 'block';
        } else {
            p.style.display = 'none';
        }
    });
}

function renderSettingsPanel(container) {
    const services = [...new Set(history.map(h => h.serviceName))];

    let html = `<table style="border-collapse:collapse;width:100%;max-width:480px;">
        <thead><tr>
            <th style="text-align:left;padding:4px 8px;opacity:0.6;font-weight:normal;">Sensor</th>
            <th style="text-align:left;padding:4px 8px;opacity:0.6;font-weight:normal;">Display Name</th>
            <th style="text-align:left;padding:4px 8px;opacity:0.6;font-weight:normal;">Color</th>
        </tr></thead><tbody>`;

    services.forEach(svc => {
        const cfg = metricConfigs[svc] || {};
        const color = cfg.color || getServiceColor(svc);
        const name = cfg.displayName || '';
        html += `<tr data-service="${svc}">
            <td style="padding:4px 8px;opacity:0.7;">${svc}</td>
            <td style="padding:4px 8px;"><input type="text" class="mc-name" placeholder="${svc}" value="${name}"
                style="background:var(--surface,#2a2a3e);border:1px solid var(--border,#444);color:var(--text);padding:2px 6px;border-radius:4px;width:140px;font-size:0.8rem;"></td>
            <td style="padding:4px 8px;"><input type="color" class="mc-color" value="${color}"
                style="width:36px;height:24px;border:none;cursor:pointer;background:none;"></td>
        </tr>`;
    });

    html += `</tbody></table>
        <button id="temps-settings-save" style="margin-top:8px;padding:4px 14px;background:var(--accent-blue,#9b5af0);border:none;border-radius:4px;color:#fff;cursor:pointer;font-size:0.8rem;">Save</button>
        <span id="temps-settings-status" style="margin-left:8px;opacity:0.6;font-size:0.75rem;"></span>`;

    container.innerHTML = html;

    container.querySelector('#temps-settings-save').addEventListener('click', async () => {
        const rows = container.querySelectorAll('tbody tr[data-service]');
        const configs = {};
        rows.forEach(row => {
            const svc = row.dataset.service;
            configs[svc] = {
                displayName: row.querySelector('.mc-name').value.trim(),
                color: row.querySelector('.mc-color').value,
            };
        });

        const status = container.querySelector('#temps-settings-status');
        try {
            const res = await fetch('/api/tabs/temps/action/save-metric-configs', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({configs}),
            });
            if (res.ok) {
                metricConfigs = configs;
                status.textContent = 'Saved ✓';
                setTimeout(() => {
                    status.textContent = '';
                }, 2000);
                resizeCanvas(); // redraw with new colors/names
            } else {
                status.textContent = 'Error saving';
            }
        } catch (e) {
            status.textContent = 'Error saving';
        }
    });
}

function render() {
    if (!ctx || !canvas || history.length === 0) return;

    const dpr = window.devicePixelRatio || 1;
    const W = canvas.offsetWidth || canvas.width / dpr;
    const H = canvas.offsetHeight || canvas.height / dpr;
    const PAD = {top: 20, right: 160, bottom: 40, left: 60};
    const chartW = W - PAD.left - PAD.right;
    const chartH = H - PAD.top - PAD.bottom;

    ctx.clearRect(0, 0, canvas.width, canvas.height);
    ctx.save();
    ctx.scale(dpr, dpr);

    // Background
    ctx.fillStyle = '#1e1e2e';
    ctx.fillRect(0, 0, W, H);

    if (history.length < 1) {
        ctx.fillStyle = '#888';
        ctx.font = '14px monospace';
        ctx.textAlign = 'center';
        ctx.fillText('Waiting for data…', W / 2, H / 2);
        ctx.restore();
        return;
    }

    // Collect unique services
    const services = new Set();
    history.forEach(pt => services.add(pt.serviceName));

    // Find temperature range
    let minTemp = Infinity;
    let maxTemp = -Infinity;
    history.forEach(pt => {
        if (pt.tempF < minTemp) minTemp = pt.tempF;
        if (pt.tempF > maxTemp) maxTemp = pt.tempF;
    });

    // Add 5% padding to the range
    const range = maxTemp - minTemp || 1;
    const padding = range * 0.05;
    minTemp -= padding;
    maxTemp += padding;

    // Grid lines
    const gridLines = 5;
    ctx.strokeStyle = '#333';
    ctx.lineWidth = 1;
    for (let i = 0; i <= gridLines; i++) {
        const y = PAD.top + chartH - (i / gridLines) * chartH;
        ctx.beginPath();
        ctx.moveTo(PAD.left, y);
        ctx.lineTo(PAD.left + chartW, y);
        ctx.stroke();

        const label = Math.round(minTemp + (i / gridLines) * (maxTemp - minTemp)) + '°F';
        ctx.fillStyle = '#888';
        ctx.font = '11px monospace';
        ctx.textAlign = 'right';
        ctx.fillText(label, PAD.left - 6, y + 4);
    }

    // X-axis time labels and grid lines
    const firstTime = history[0].timestamp;
    const lastTime = history[history.length - 1].timestamp;
    const timeRange = lastTime - firstTime || 1;

    // Draw vertical grid lines at hours and half-hours
    const msPerHour = 60 * 60 * 1000;
    const startHour = new Date(firstTime);
    startHour.setMinutes(0, 0, 0);

    for (let t = new Date(startHour); t <= lastTime; t = new Date(t.getTime() + 30 * 60 * 1000)) {
        if (t < firstTime) continue;
        const x = PAD.left + ((t - firstTime) / timeRange) * chartW;
        const isHour = t.getMinutes() === 0;

        if (isHour) {
            ctx.strokeStyle = '#666';
            ctx.lineWidth = 1.5;
        } else {
            ctx.strokeStyle = '#555';
            ctx.lineWidth = 1;
        }

        ctx.beginPath();
        ctx.moveTo(x, PAD.top);
        ctx.lineTo(x, PAD.top + chartH);
        ctx.stroke();
    }

    // Hour labels on x-axis
    ctx.fillStyle = '#aaa';
    ctx.font = '11px monospace';
    ctx.textAlign = 'center';

    for (let t = new Date(startHour); t <= lastTime; t = new Date(t.getTime() + msPerHour)) {
        if (t < firstTime) continue;
        const x = PAD.left + ((t - firstTime) / timeRange) * chartW;
        ctx.fillText(t.toLocaleTimeString(), x, H - PAD.bottom + 16);
    }

    // Y-axis label
    ctx.save();
    ctx.translate(14, PAD.top + chartH / 2);
    ctx.rotate(-Math.PI / 2);
    ctx.fillStyle = '#aaa';
    ctx.font = '12px monospace';
    ctx.textAlign = 'center';
    ctx.fillText('Temperature (°F)', 0, 0);
    ctx.restore();

    // Plot each service's temperature line
    Array.from(services).forEach(serviceName => {
        const color = getServiceColor(serviceName);
        const servicePoints = history.filter(pt => pt.serviceName === serviceName);

        ctx.strokeStyle = color;
        ctx.lineWidth = 1.0;
        ctx.beginPath();

        servicePoints.forEach((pt, i) => {
            const x = PAD.left + ((pt.timestamp - firstTime) / timeRange) * chartW;
            const y = PAD.top + chartH - ((pt.tempF - minTemp) / (maxTemp - minTemp)) * chartH;
            if (i === 0) ctx.moveTo(x, y);
            else ctx.lineTo(x, y);
        });

        ctx.stroke();
    });

    // Legend
    const legendX = PAD.left + chartW + 10;
    let legendY = PAD.top;
    ctx.font = '11px monospace';
    ctx.textAlign = 'left';
    Array.from(services).forEach(serviceName => {
        const color = getServiceColor(serviceName);
        ctx.fillStyle = color;
        ctx.fillRect(legendX, legendY, 12, 12);
        ctx.fillStyle = '#ccc';
        const label = getServiceLabel(serviceName);
        const truncated = label.length > 18 ? label.slice(0, 16) + '…' : label;
        ctx.fillText(truncated, legendX + 16, legendY + 10);
        legendY += 18;
    });

    ctx.restore();
}

