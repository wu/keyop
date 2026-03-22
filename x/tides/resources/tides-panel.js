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
            if (data && data.event) {
                lastTideData = data;
                updatePanel();
            } else {
                panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Waiting for tide data...</div>';
            }
        } else {
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
                        solar_days: data.solar_days || [],
                    };
                    updatePanel();
                }
            })
            .catch(err => console.error('Failed to fetch sparkline on message:', err));
    }
}

function updatePanel() {
    if (!panelBody || !lastTideData) {
        return;
    }

    const event = lastTideData.event || lastTideData;
    const sparklineRecords = lastTideData.sparklineRecords || [];
    const state = lastTideData.state || event.state || '';
    const solarDays = lastTideData.solarDays || lastTideData.solar_days || [];

    // Use sun-wrapper layout similar to aurora panel
    let html = `<div class="sun-wrapper">`;

    // Wrap title and sparkline together in sun-event
    html += `<div class="sun-event" style="padding-bottom: 16px;">`;
    html += `<div style="font-weight: bold; margin-bottom: 6px;">Tide Forecast</div>`;
    if (sparklineRecords.length > 0) {
        html += `<div id="tides-sparkline" style="margin-top: 0px; margin-bottom: 12px;">`;
        html += renderSparkline(sparklineRecords, solarDays);
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

// Returns 'day', 'night', or 'mixed' for the period [startDate, endDate) using
// server-provided solar_days (array of {date, dawn, dusk} ISO strings from Go).
function getLightType(startDate, endDate, solarDays) {
    if (!solarDays || solarDays.length === 0) return 'night';
    const s = startDate.getTime(), e = endDate.getTime();
    const dateKey = startDate.toISOString().slice(0, 10);
    const prevKey = new Date(startDate.getTime() - 86400000).toISOString().slice(0, 10);
    const today = solarDays.find(d => d.date === dateKey);
    const prev = solarDays.find(d => d.date === prevKey);
    const dawnMs = today ? new Date(today.dawn).getTime() : null;
    const duskMs = today ? new Date(today.dusk).getTime() : null;
    const prevDuskMs = prev ? new Date(prev.dusk).getTime() : null;
    if (dawnMs !== null && e <= dawnMs) {
        if (prevDuskMs === null) return 'night';
        if (s >= prevDuskMs) return 'night';
        if (e <= prevDuskMs) return 'day';
        return 'mixed';
    }
    if (dawnMs === null || duskMs === null) return 'night';
    if (s >= dawnMs && e <= duskMs) return 'day';
    if (s >= duskMs) return 'night';
    return 'mixed';
}

function renderSparkline(records, solarDays = null) {
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

    // Draw daylight/night background using server-provided solar day segments
    if (solarDays && solarDays.length > 0 && timeRange > 0) {
        // Sample background in ~30-min slices across the time range
        const slices = 48;
        const sliceMs = timeRange / slices;
        for (let i = 0; i < slices; i++) {
            const sliceStart = new Date(minTime + i * sliceMs);
            const sliceEnd = new Date(minTime + (i + 1) * sliceMs);
            const lightType = getLightType(sliceStart, sliceEnd, solarDays);
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

