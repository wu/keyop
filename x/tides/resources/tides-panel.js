let panelBody = null;
let lastTideData = null;

export async function init(el) {
    panelBody = el.querySelector('.panel-body') || el;
    if (!panelBody) return;

    // Initialize with placeholder
    panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Tide data loading...</div>';

    // Fetch initial tides from database
    try {
        const res = await fetch('/api/tabs/tides/action/fetch-tides', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
        });
        if (res.ok) {
            const data = await res.json();
            console.log('=== Tides Panel Fetch ===');
            console.log('Full response:', JSON.stringify(data, null, 2));
            if (data && data.event) {
                lastTideData = data;
                updatePanel();
            } else {
                console.warn('No event in tide response:', data);
                panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Waiting for tide data...</div>';
            }
        } else {
            console.error('Tide fetch failed:', res.status, res.statusText);
            const errorText = await res.text();
            console.error('Error response:', errorText);
            panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Tide data unavailable</div>';
        }
    } catch (e) {
        console.error('Failed to fetch tides:', e);
        panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Error loading tide data</div>';
    }
}

export function onMessage(msg) {
    if (!panelBody) return;

    // Listen for tide event messages
    if (msg.event && msg.event.includes('tide')) {
        console.log('Tides panel received message:', msg);
        lastTideData = msg.data || msg;

        // Fetch fresh sparkline data when tide updates come in
        fetch('/api/tabs/tides/action/fetch-tides', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
        })
            .then(res => res.ok ? res.json() : null)
            .then(data => {
                if (data && data.event) {
                    // Merge the new sparkline data with the message data
                    lastTideData = {
                        event: msg.data || msg,
                        sparklineRecords: data.sparklineRecords || [],
                        currentLevel: data.currentLevel,
                        peakLevel: data.peakLevel,
                        state: data.state
                    };
                    updatePanel();
                }
            })
            .catch(err => console.error('Failed to fetch sparkline on message:', err));
    }
}

function updatePanel() {
    if (!panelBody || !lastTideData) {
        console.log('updatePanel: missing data');
        return;
    }

    const event = lastTideData.event || lastTideData;
    const sparklineRecords = lastTideData.sparklineRecords || [];
    const state = lastTideData.state || event.state || '';

    console.log('=== updatePanel ===');
    console.log('Current level:', lastTideData.currentLevel);
    console.log('Peak level:', lastTideData.peakLevel);
    console.log('State:', state);
    console.log('Sparkline records:', sparklineRecords.length);

    // Use sun-wrapper layout similar to aurora panel
    let html = `<div class="sun-wrapper">`;

    // Wrap title and sparkline together in sun-event
    html += `<div class="sun-event" style="padding-bottom: 16px;">`;
    html += `<div style="font-weight: bold; margin-bottom: 6px;">Tide Forecast</div>`;
    if (sparklineRecords.length > 0) {
        html += `<div id="tides-sparkline" style="margin-top: 0px; margin-bottom: 12px;">`;
        html += renderSparkline(sparklineRecords);
        html += `</div>`;
    }
    html += `</div>`;

    // Current and peak info in two columns
    html += `<div class="sun-meta" style="width: 100%;">`;

    // Current level
    html += `<div class="sun-length sun-day" style="flex: 1; flex-direction: column; align-items: center;">`;
    html += `<div class="sun-label">Current: ${state}</div>`;
    html += `<div class="sun-value sun-day-value">${(lastTideData.currentLevel || 0).toFixed(2)} ft</div>`;
    html += `</div>`;

    // Next peak
    if (event.nextPeak && event.nextPeak.time) {
        const isHigh = event.nextPeak.type === 'high' || event.nextPeak.type === 'HIGH';
        const peakLabel = isHigh ? 'High' : 'Low';
        const peakTime = new Date(event.nextPeak.time);
        const now = new Date();
        const diff = peakTime - now;
        const hours = Math.floor(diff / 3600000);
        const minutes = Math.floor((diff % 3600000) / 60000);
        const timeStr = hours > 0 ? `${hours}h ${minutes}m` : `${minutes}m`;

        html += `<div class="sun-length sun-night" style="flex: 1; flex-direction: column; align-items: center;">`;
        html += `<div class="sun-label">Next Peak: ${timeStr}</div>`;
        html += `<div class="sun-value sun-night-value">${peakLabel} ${(event.nextPeak.value || 0).toFixed(2)} ft</div>`;
        html += `</div>`;
    }

    html += `</div>`;
    html += `</div>`;

    panelBody.innerHTML = html;
}

function renderSparkline(records) {
    const values = records.map(r => parseFloat(r.v || r.value || 0));
    if (values.length === 0) return '';

    const minVal = Math.min(...values);
    const maxVal = Math.max(...values);
    const range = maxVal - minVal || 1;

    // Create SVG sparkline with fixed dimensions
    const width = 200;
    const height = 40;
    const padding = 3;
    const graphWidth = width - (padding * 2);
    const graphHeight = height - (padding * 2);

    // Calculate position of current time based on time range
    const now = new Date();
    const recordTimes = records.map(r => new Date(r.t || r.time));

    const minTime = Math.min(...recordTimes.map(t => t.getTime()));
    const maxTime = Math.max(...recordTimes.map(t => t.getTime()));
    const timeRange = maxTime - minTime;
    const timeSinceStart = now.getTime() - minTime;
    const currentPosition = timeSinceStart / timeRange; // 0 to 1

    // Find current point (closest record to now)
    let currentIndex = 0;
    let minDiff = Infinity;
    for (let i = 0; i < records.length; i++) {
        const recordTime = new Date(records[i].t || records[i].time);
        const diff = Math.abs(recordTime - now);
        if (diff < minDiff) {
            minDiff = diff;
            currentIndex = i;
        }
    }

    // Normalize and create points
    const points = values.map((v, i) => {
        const x = padding + (i / (values.length - 1 || 1)) * graphWidth;
        const normalizedV = (v - minVal) / range;
        const y = padding + graphHeight - (normalizedV * graphHeight);
        return {x, y};
    });

    // Create polyline path
    let pathData = `M ${points[0].x} ${points[0].y}`;
    for (let i = 1; i < points.length; i++) {
        pathData += ` L ${points[i].x} ${points[i].y}`;
    }

    // Create SVG with fixed dimensions (matching aurora panel pattern)
    let svg = `<svg width="${width}" height="${height}" style="margin-top: 8px;">`;

    // Draw vertical line at current time (based on actual time position in range)
    const currentX = padding + (currentPosition * graphWidth);
    svg += `<line x1="${currentX}" y1="${padding}" x2="${currentX}" y2="${height - padding}" stroke="var(--accent-blue, #9b5af0)" stroke-width="2" opacity="0.3"/>`;

    // Draw the main line
    svg += `<path d="${pathData}" stroke="var(--accent-blue, #9b5af0)" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`;

    // Draw all points
    for (let i = 0; i < points.length; i++) {
        const point = points[i];
        if (i === currentIndex) {
            // Current point: larger and more prominent
            svg += `<circle cx="${point.x}" cy="${point.y}" r="2.5" fill="var(--accent-blue, #9b5af0)"/>`;
        } else {
            // Other points: smaller
            svg += `<circle cx="${point.x}" cy="${point.y}" r="1.5" fill="var(--accent-blue, #9b5af0)"/>`;
        }
    }
    svg += `</svg>`;

    return svg;
}
