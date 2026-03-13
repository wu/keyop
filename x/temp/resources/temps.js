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

function getServiceColor(serviceName) {
    if (!serviceColors[serviceName]) {
        serviceColors[serviceName] = PALETTE[colorIndex % PALETTE.length];
        colorIndex++;
    }
    return serviceColors[serviceName];
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

    // Only process temp-type messages
    if (msg.dataType !== 'core.temp.v1') return;

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

        resizeCanvas();
    } catch (err) {
        console.error('Failed to refresh temps:', err);
        tempsContainer.innerHTML = `<div class="error">Error loading temps: ${err.message}</div>`;
    }
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

    // X-axis time labels
    const firstTime = history[0].timestamp;
    const lastTime = history[history.length - 1].timestamp;
    const timeRange = lastTime - firstTime || 1;
    ctx.fillStyle = '#888';
    ctx.font = '11px monospace';
    ctx.textAlign = 'center';
    [0, 0.5, 1].forEach(frac => {
        const t = new Date(firstTime.getTime() + frac * timeRange);
        const x = PAD.left + frac * chartW;
        ctx.fillText(t.toLocaleTimeString(), x, H - PAD.bottom + 16);
    });

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
        ctx.lineWidth = 1.5;
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
        const label = serviceName.length > 18 ? serviceName.slice(0, 16) + '…' : serviceName;
        ctx.fillText(label, legendX + 16, legendY + 10);
        legendY += 18;
    });

    ctx.restore();
}
