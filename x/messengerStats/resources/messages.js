let container = null;
let canvas = null;
let ctx = null;

// Rolling window of data points: [{time, counts: {channelName: int64}}]
const MAX_POINTS = 60;
const history = [];

// Stable channel color palette
const PALETTE = [
    '#bb86fc', '#03dac6', '#cf6679', '#ffb74d',
    '#4fc3f7', '#aed581', '#ff8a65', '#ce93d8',
    '#80cbc4', '#f48fb1', '#ffe082', '#a5d6a7',
];
const channelColors = {};
let colorIndex = 0;

function getChannelColor(channel) {
    if (!channelColors[channel]) {
        channelColors[channel] = PALETTE[colorIndex % PALETTE.length];
        colorIndex++;
    }
    return channelColors[channel];
}

export function init(el) {
    container = el;
    canvas = container.querySelector('#messages-chart');
    if (canvas) {
        ctx = canvas.getContext('2d');
    }

    // ResizeObserver fires when the element is actually laid out (including
    // when the tab becomes visible), giving accurate dimensions on HiDPI.
    const ro = new ResizeObserver(() => resizeCanvas());
    ro.observe(container);

    render();
}

function resizeCanvas() {
    if (!canvas || !container) return;
    const dpr = window.devicePixelRatio || 1;
    // Use offsetWidth so we get the real CSS pixel width after layout
    const cssW = canvas.offsetWidth || container.offsetWidth || 800;
    const cssH = 320;
    canvas.style.height = cssH + 'px';
    // Set backing buffer scaled for display density
    canvas.width = Math.round(cssW * dpr);
    canvas.height = Math.round(cssH * dpr);
    render();
}

export function onMessage(msg) {
    if (!container) return;
    if (msg.serviceType !== 'messengerStats' || msg.event !== 'stats') return;

    const data = msg.data;
    if (!data || !data.channelMessageCounts) return;

    history.push({
        time: msg.timestamp ? new Date(msg.timestamp) : new Date(),
        counts: data.channelMessageCounts,
        total: data.totalMessageCount || 0,
        failures: data.totalFailureCount || 0,
    });

    if (history.length > MAX_POINTS) {
        history.shift();
    }

    updateSummary(data);
    render();
}

function updateSummary(data) {
    const totalEl = container.querySelector('#messages-total');
    const failEl = container.querySelector('#messages-failures');
    if (totalEl) totalEl.textContent = data.totalMessageCount?.toLocaleString() ?? '0';
    if (failEl) failEl.textContent = data.totalFailureCount?.toLocaleString() ?? '0';
}

function render() {
    if (!ctx || !canvas) return;

    const dpr = window.devicePixelRatio || 1;
    // Use the CSS display width so coordinates match what the user sees
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

    if (history.length < 2) {
        ctx.fillStyle = '#888';
        ctx.font = '14px monospace';
        ctx.textAlign = 'center';
        ctx.fillText('Waiting for data…', W / 2, H / 2);
        return;
    }

    // Collect all channel names across history
    const channels = new Set();
    history.forEach(pt => Object.keys(pt.counts).forEach(ch => channels.add(ch)));

    // Derive per-channel rates (msgs/min) between consecutive history points.
    // rates[i] corresponds to the interval ending at history[i] (i >= 1).
    const rates = history.map((pt, i) => {
        if (i === 0) return {time: pt.time, r: {}};
        const prev = history[i - 1];
        const deltaMin = (pt.time - prev.time) / 60000;
        const r = {};
        channels.forEach(ch => {
            const delta = (pt.counts[ch] || 0) - (prev.counts[ch] || 0);
            r[ch] = deltaMin > 0 ? Math.max(0, delta / deltaMin) : 0;
        });
        return {time: pt.time, r};
    }).slice(1); // drop the first placeholder

    // Sort channels by most recent rate descending
    const lastRate = rates[rates.length - 1]?.r || {};
    const channelList = Array.from(channels).sort((a, b) => (lastRate[b] || 0) - (lastRate[a] || 0));

    // Find y-axis max across all rate points
    let yMax = 0;
    rates.forEach(pt => {
        channelList.forEach(ch => {
            if ((pt.r[ch] || 0) > yMax) yMax = pt.r[ch];
        });
    });
    if (yMax === 0) yMax = 1;

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

        const label = Math.round((i / gridLines) * yMax).toLocaleString() + '/min';
        ctx.fillStyle = '#888';
        ctx.font = '11px monospace';
        ctx.textAlign = 'right';
        ctx.fillText(label, PAD.left - 6, y + 4);
    }

    // X-axis time labels
    const firstTime = rates[0].time;
    const lastTime = rates[rates.length - 1].time;
    const timeRange = lastTime - firstTime || 1;
    ctx.fillStyle = '#888';
    ctx.font = '11px monospace';
    ctx.textAlign = 'center';
    [0, 0.5, 1].forEach(frac => {
        const t = new Date(firstTime.getTime() + frac * timeRange);
        const x = PAD.left + frac * chartW;
        ctx.fillText(t.toLocaleTimeString(), x, H - PAD.bottom + 16);
    });

    // Axis labels
    ctx.save();
    ctx.translate(14, PAD.top + chartH / 2);
    ctx.rotate(-Math.PI / 2);
    ctx.fillStyle = '#aaa';
    ctx.font = '12px monospace';
    ctx.textAlign = 'center';
    ctx.fillText('msgs / min', 0, 0);
    ctx.restore();

    // Plot each channel's rate
    channelList.forEach(ch => {
        const color = getChannelColor(ch);
        ctx.strokeStyle = color;
        ctx.lineWidth = 2;
        ctx.beginPath();

        rates.forEach((pt, i) => {
            const x = PAD.left + ((pt.time - firstTime) / timeRange) * chartW;
            const v = pt.r[ch] || 0;
            const y = PAD.top + chartH - (v / yMax) * chartH;
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
    channelList.forEach(ch => {
        const color = getChannelColor(ch);
        ctx.fillStyle = color;
        ctx.fillRect(legendX, legendY, 12, 12);
        ctx.fillStyle = '#ccc';
        const label = ch.length > 18 ? ch.slice(0, 16) + '…' : ch;
        ctx.fillText(label, legendX + 16, legendY + 10);
        legendY += 18;
    });

    ctx.restore();
}
