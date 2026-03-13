// idle-report.js - Canvas-based idle report visualization with incremental updates
let container = null;
let reportData = null; // Current report data
let canvas = null;
let canvasCtx = null;
let activePeriodTable = null;

const COLORS = {
    active: '#c77dff',      // light purple
    idle: '#6b21a8',        // dark purple
    unknown: '#333333',     // dark gray (slightly lighter than background)
    border: '#444444',      // cell borders
    text: '#e0e0e0',        // light text for dark mode
};

const HOUR_HEIGHT = 25;
const MINUTE_WIDTH = 10;
const PADDING_LEFT = 100;
const PADDING_TOP = 30;
const PADDING_RIGHT = 80;
const PADDING_BOTTOM = 20;

export function init(el) {
    container = el;

    canvas = container.querySelector('#idle-report-canvas');
    activePeriodTable = container.querySelector('#idle-periods-body');

    function triggerRefresh() {
        const now = new Date();
        const start = new Date(now.getTime() - 24 * 60 * 60 * 1000); // 24 hours ago

        const body = {
            start: start.toISOString(),
            end: now.toISOString(),
        };

        fetch('/api/tabs/idle/action/fetch-idle-report', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(body),
        })
            .then(response => response.json())
            .then(data => {
                if (data && data.data) {
                    reportData = data.data;
                    render();
                } else {
                    console.warn('No data in idle report response');
                }
            })
            .catch(err => console.error('Failed to fetch idle report:', err));
    }

    // Trigger initial refresh if tab is active
    function isActive() {
        return container && container.classList && container.classList.contains('active');
    }

    if (isActive()) {
        triggerRefresh();
    } else {
        try {
            const obs = new MutationObserver((mutations, observer) => {
                if (isActive()) {
                    triggerRefresh();
                    observer.disconnect();
                }
            });
            obs.observe(container, {attributes: true, attributeFilter: ['class']});
        } catch (e) {
            setTimeout(() => {
                if (isActive()) triggerRefresh();
            }, 500);
        }
    }
}

function resizeCanvas() {
    if (!canvas) return;
    const rect = canvas.getBoundingClientRect();
    const dpr = window.devicePixelRatio || 1;
    canvas.width = rect.width * dpr;
    canvas.height = rect.height * dpr;
    canvasCtx = canvas.getContext('2d');
    canvasCtx.scale(dpr, dpr);
    doRender();
}

function render() {
    if (!reportData || !canvas) return;

    // Initialize canvas dimensions on first render
    if (!canvas.style.width) {
        const width = 60 * MINUTE_WIDTH + PADDING_LEFT + PADDING_RIGHT;
        const height = reportData.hourlyData.length * HOUR_HEIGHT + PADDING_TOP + PADDING_BOTTOM;
        canvas.style.width = width + 'px';
        canvas.style.height = height + 'px';
        resizeCanvas();
        return;
    }

    doRender();
}

function doRender() {
    if (!reportData || !canvas) return;

    if (!canvasCtx) {
        canvasCtx = canvas.getContext('2d');
    }

    const dpr = window.devicePixelRatio || 1;
    canvasCtx.clearRect(0, 0, canvas.width / dpr, canvas.height / dpr);

    // Draw hours in reverse order (newest at top)
    for (let i = reportData.hourlyData.length - 1; i >= 0; i--) {
        const hourData = reportData.hourlyData[i];
        const y = PADDING_TOP + (reportData.hourlyData.length - 1 - i) * HOUR_HEIGHT;
        drawHourRow(hourData, y, i);
    }

    // Draw summary
    updateSummary();
    updateActivePeriodTable();

    // Set up ResizeObserver for responsive sizing
    if (!canvas._resizeObserver) {
        canvas._resizeObserver = new ResizeObserver(() => {
            resizeCanvas();
        });
        canvas._resizeObserver.observe(canvas);
    }
}

function drawHourRow(hourData, y, hourIndex) {
    const timeStr = new Date(hourData.time).toLocaleTimeString('en-US', {
        hour: '2-digit',
        minute: '2-digit',
        hour12: false
    });
    const dateStr = new Date(hourData.time).toLocaleDateString('en-US', {month: '2-digit', day: '2-digit'});

    // Draw label
    canvasCtx.fillStyle = COLORS.text;
    canvasCtx.font = '12px monospace';
    canvasCtx.textAlign = 'right';
    canvasCtx.fillText(dateStr + ' ' + timeStr, PADDING_LEFT - 10, y + 20);

    // Draw minute cells
    const minuteData = hourData.minuteData;
    for (let i = 0; i < minuteData.length; i++) {
        const x = PADDING_LEFT + i * MINUTE_WIDTH;
        const char = minuteData[i];

        let color = COLORS.unknown;
        if (char === '█') {
            color = COLORS.active;
        } else if (char === '·') {
            color = COLORS.idle;
        }

        canvasCtx.fillStyle = color;
        canvasCtx.fillRect(x, y + 5, MINUTE_WIDTH - 1, HOUR_HEIGHT - 10);

        // Draw cell border
        canvasCtx.strokeStyle = COLORS.border;
        canvasCtx.lineWidth = 0.5;
        canvasCtx.strokeRect(x, y + 5, MINUTE_WIDTH - 1, HOUR_HEIGHT - 10);
    }

    // Draw active minute count
    canvasCtx.fillStyle = COLORS.text;
    canvasCtx.font = 'bold 12px monospace';
    canvasCtx.textAlign = 'left';
    canvasCtx.fillText(hourData.activeMins + 'm', PADDING_LEFT + 60 * MINUTE_WIDTH + 10, y + 20);
}

function updateSummary() {
    const totalsEl = container.querySelector('#idle-totals');
    if (!totalsEl || !reportData) return;

    const formatDuration = (secs) => {
        const m = Math.floor(secs / 60);
        const h = Math.floor(m / 60);
        const mm = m % 60;
        return `${h}h ${mm}m`;
    };

    totalsEl.innerHTML = `
    <div style="display: flex; gap: 20px; margin-bottom: 10px; font-size: 14px;">
      <div><strong>Total active:</strong> ${formatDuration(reportData.totalActiveDurationSecs)}</div>
      <div><strong>Total idle:</strong> ${formatDuration(reportData.totalIdleDurationSecs)}</div>
      <div><strong>Total unknown:</strong> ${formatDuration(reportData.totalUnknownDurationSecs)}</div>
    </div>
  `;
}

function updateActivePeriodTable() {
    if (!activePeriodTable || !reportData) return;

    activePeriodTable.innerHTML = '';

    // Show most recent periods first
    for (let i = reportData.activePeriods.length - 1; i >= 0; i--) {
        const p = reportData.activePeriods[i];
        const row = document.createElement('tr');

        const startTime = new Date(p.start).toLocaleTimeString('en-US', {hour12: true});
        const stopTime = new Date(p.stop).toLocaleTimeString('en-US', {hour12: true});
        const durationSecs = p.durationSeconds;
        const m = Math.floor(durationSecs / 60);
        const h = Math.floor(m / 60);
        const mm = m % 60;
        const duration = `${h}h ${mm}m`;

        row.innerHTML = `
      <td>${p.hostname}</td>
      <td>${startTime}</td>
      <td>${stopTime}</td>
      <td>${duration}</td>
    `;
        activePeriodTable.appendChild(row);
    }
}

export function onMessage(msg) {
    if (!container) return;
    if (!container.classList || !container.classList.contains('active')) return;

    // Filter for idle events - check event type or service type
    if (msg.event !== 'idle_status' && msg.serviceType !== 'idle') return;

    if (!reportData) return; // No report yet, skip

    // Use the same rolling 24-hour window as init()
    const now = new Date();
    const start = new Date(now.getTime() - 24 * 60 * 60 * 1000); // 24 hours ago

    fetch('/api/tabs/idle/action/fetch-idle-report', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({
            start: start.toISOString(),
            end: now.toISOString(),
        }),
    })
        .then(response => response.json())
        .then(data => {
            if (data && data.data) {
                // Replace entire report data with fresh data
                reportData = data.data;
                render();
            }
        })
        .catch(err => console.error('Failed to fetch updated report:', err));
}
