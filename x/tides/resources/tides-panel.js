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
                        event: data.event,
                        sparklineRecords: data.sparklineRecords || [],
                        currentLevel: data.currentLevel,
                        peakLevel: data.peakLevel,
                        state: data.state,
                        lat: data.lat,
                        lon: data.lon,
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
    const lat = lastTideData.lat != null ? lastTideData.lat : null;
    const lon = lastTideData.lon != null ? lastTideData.lon : null;

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
        html += renderSparkline(sparklineRecords, lat, lon);
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

    // Next daylight low tide period
    const periods = event.periods || [];
    if (periods.length > 0) {
        const p = periods[0];
        const start = new Date(p.start);
        const now2 = new Date();
        const diffMs = start - now2;

        let relStr;
        if (diffMs <= 0) {
            // Period is ongoing
            const endT = new Date(p.end);
            const remMs = endT - now2;
            if (remMs > 0) {
                relStr = 'now · ' + fmtDuration(remMs) + ' left';
            } else {
                relStr = 'today';
            }
        } else {
            relStr = 'in ' + fmtDuration(diffMs);
        }

        const durStr = fmtDuration(p.duration / 1e6); // Go duration in ns → ms
        const minFt = (p.minValue || 0).toFixed(1);

        // Day label: "Today", "Tomorrow", or day of week
        const startDay = new Date(start.getFullYear(), start.getMonth(), start.getDate());
        const todayDay = new Date(now2.getFullYear(), now2.getMonth(), now2.getDate());
        const dayDiff = Math.round((startDay - todayDay) / 86400000);
        const dayLabel = dayDiff === 0 ? 'Today' : dayDiff === 1 ? 'Tomorrow' : p.dayOfWeek || '';

        const timeStr = start.toLocaleTimeString([], {hour: 'numeric', minute: '2-digit'});

        html += `<div style="width: 100%; text-align: center; margin-top: 6px;">`;
        html += `<div class="sun-label" style="margin-bottom: 2px;">Daylight Low</div>`;
        html += `<div class="sun-value" style="font-size: 0.85rem;">${relStr}</div>`;
        html += `<div class="sun-label" style="font-size: 0.7rem;">${dayLabel} ${timeStr} · ${durStr}</div>`;
        html += `</div>`;
    }

    html += `</div>`;

    panelBody.innerHTML = html;
}

function fmtDuration(ms) {
    const totalMin = Math.round(ms / 60000);
    const h = Math.floor(totalMin / 60);
    const m = totalMin % 60;
    if (h > 0 && m > 0) return `${h}h ${m}m`;
    if (h > 0) return `${h}h`;
    return `${m}m`;
}

// Compute civil dawn and dusk for a given date at lat/lon (NOAA algorithm).
// Returns {dawn: Date, dusk: Date} as UTC Date objects.
function computeCivilDawnDusk(date, lat, lon) {
    const rad = Math.PI / 180;
    const deg = 180 / Math.PI;
    const jd = (Date.UTC(date.getFullYear(), date.getMonth(), date.getDate()) / 86400000) + 2440587.5;
    const n = jd - 2451545.0;
    const L = (280.460 + 0.9856474 * n) % 360;
    const g = (357.528 + 0.9856003 * n) % 360;
    const lambda = L + 1.915 * Math.sin(g * rad) + 0.020 * Math.sin(2 * g * rad);
    const epsilon = 23.439 - 0.0000004 * n;
    const sinDec = Math.sin(epsilon * rad) * Math.sin(lambda * rad);
    const dec = Math.asin(sinDec) * deg;
    const cosH = (Math.cos(96 * rad) - Math.sin(lat * rad) * Math.sin(dec * rad))
        / (Math.cos(lat * rad) * Math.cos(dec * rad));
    const baseMs = Date.UTC(date.getFullYear(), date.getMonth(), date.getDate());
    if (cosH < -1) return {dawn: new Date(baseMs), dusk: new Date(baseMs + 86399000)};
    if (cosH > 1) return {dawn: new Date(baseMs + 43200000), dusk: new Date(baseMs + 43200000)};
    const H = Math.acos(cosH) * deg;
    const eqTime = 4 * (L - lambda);
    const solarNoonUTC = 720 - 4 * lon - eqTime;
    return {
        dawn: new Date(baseMs + (solarNoonUTC - H * 4) * 60000),
        dusk: new Date(baseMs + (solarNoonUTC + H * 4) * 60000),
    };
}

function renderSparkline(records, lat = null, lon = null) {
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

    // Draw daylight/night background if lat/lon available
    if (lat != null && lon != null && timeRange > 0) {
        // Sample background in ~30-min slices across the time range
        const slices = 48;
        const sliceMs = timeRange / slices;
        for (let i = 0; i < slices; i++) {
            const sliceStart = new Date(minTime + i * sliceMs);
            const sliceEnd = new Date(minTime + (i + 1) * sliceMs);
            const {dawn, dusk} = computeCivilDawnDusk(sliceStart, lat, lon);
            const s = sliceStart.getTime(), e = sliceEnd.getTime();
            const d = dawn.getTime(), k = dusk.getTime();
            let lightType;
            if (s >= d && e <= k) lightType = 'day';
            else if (e <= d || s >= k) lightType = 'night';
            else lightType = 'mixed';
            const color = lightType === 'day'
                ? 'rgba(255,255,255,0.10)'
                : lightType === 'night'
                    ? 'rgba(0,0,0,0.28)'
                    : 'rgba(255,255,255,0.03)';
            const rx = padding + (i / slices) * graphWidth;
            const rw = graphWidth / slices + 0.5; // slight overlap to avoid gaps
            svg += `<rect x="${rx}" y="${padding}" width="${rw}" height="${graphHeight}" fill="${color}"/>`;
        }
    }

    // Draw the main line
    svg += `<path d="${pathData}" stroke="var(--accent-blue, #9b5af0)" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`;

    // Draw all points
    for (let i = 0; i < points.length; i++) {
        if (i === currentIndex) continue; // draw last
        svg += `<circle cx="${points[i].x}" cy="${points[i].y}" r="1.5" fill="var(--accent-blue, #9b5af0)"/>`;
    }
    // Draw current point last so it sits on top of everything
    const cp = points[currentIndex];
    svg += `<circle cx="${cp.x}" cy="${cp.y}" r="5" fill="none" stroke="#c49cf8" stroke-width="1.5"/>`;
    svg += `<circle cx="${cp.x}" cy="${cp.y}" r="3" fill="#c49cf8"/>`;
    svg += `</svg>`;

    return svg;
}

