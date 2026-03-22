let container = null;
let body = null;

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

function formatRemaining(ms) {
    if (ms == null || isNaN(ms)) return '—';
    ms = Math.max(0, ms);
    const totalSec = Math.floor(ms / 1000);
    const days = Math.floor(totalSec / 86400);
    const h = Math.floor((totalSec % 86400) / 3600);
    const m = Math.floor((totalSec % 3600) / 60);
    const parts = [];
    if (days > 0) parts.push(`${days}d`);
    if (h > 0) parts.push(`${h}h`);
    parts.push(`${m}m`);
    return parts.join(' ');
}

// Parse a UTC date string like "Mar 18" and a UTC hour to get a Date object
function parseUtcDate(dateStr, utcHour) {
    const parts = dateStr.trim().split(/\s+/);
    const month = parts[0];
    const day = parseInt(parts[1]);
    const monthMap = {
        'Jan': '01', 'Feb': '02', 'Mar': '03', 'Apr': '04', 'May': '05', 'Jun': '06',
        'Jul': '07', 'Aug': '08', 'Sep': '09', 'Oct': '10', 'Nov': '11', 'Dec': '12'
    };
    const monthNum = monthMap[month] || '03';
    const year = new Date().getFullYear();
    const utcDateStr = `${year}-${monthNum}-${String(day).padStart(2, '0')}T${String(utcHour).padStart(2, '0')}:00:00Z`;
    return new Date(utcDateStr);
}

// Parse UTC hours from period string (e.g., "00-03UT" -> { start: 0, end: 3 })
function parseUtcHours(periodStr) {
    const match = periodStr.match(/(\d{2})-(\d{2})UT/);
    if (!match) return {start: 0, end: 0};
    return {
        start: parseInt(match[1]),
        end: parseInt(match[2])
    };
}

// Extract KP values from forecast data, optionally filtered to future only
function extractKpValues(forecast, futureOnly = true) {
    return extractKpEntries(forecast, futureOnly).map(e => e.kp);
}

// Extract KP entries with start/end dates from forecast data
function extractKpEntries(forecast, futureOnly = true) {
    if (!forecast || !forecast.data || !forecast.data.table) {
        return [];
    }

    const pf = forecast.data;
    const entries = [];
    const now = new Date();

    for (let dayIndex = 0; dayIndex < pf.days.length; dayIndex++) {
        const dayStr = pf.days[dayIndex];

        for (const period of pf.periods) {
            const periodEntries = pf.table[period] || [];
            const entry = periodEntries[dayIndex];

            if (!entry || (entry.kp === null && entry.kp === undefined)) {
                continue;
            }

            const {start: startHour, end: endHour} = parseUtcHours(period);
            const startDate = parseUtcDate(dayStr, startHour);
            const endDate = parseUtcDate(dayStr, endHour);
            if (endHour < startHour) endDate.setDate(endDate.getDate() + 1);

            if (futureOnly && endDate <= now) continue;

            if (entry.kp !== null && entry.kp !== undefined) {
                entries.push({kp: entry.kp, startDate, endDate});
            }
        }
    }

    return entries;
}

// Render a sparkline showing KP values
function renderSparkline(forecast, width = 200, solarDays = null) {
    const kpEntries = extractKpEntries(forecast, true);
    if (kpEntries.length === 0) {
        return '';
    }

    const kpValues = kpEntries.map(e => e.kp);
    const height = 40;
    const padding = 3;
    const graphWidth = width - (padding * 2);
    const graphHeight = height - (padding * 2);

    const minKp = Math.min(...kpValues);
    const maxKp = Math.max(...kpValues);
    const range = maxKp - minKp || 1;

    const points = kpEntries.map(({kp}, i) => {
        const x = padding + (i / (kpEntries.length - 1 || 1)) * graphWidth;
        const normalizedY = (kp - minKp) / range;
        const y = padding + graphHeight - (normalizedY * graphHeight);
        return {x, y};
    });

    let pathData = `M ${points[0].x} ${points[0].y}`;
    for (let i = 1; i < points.length; i++) {
        pathData += ` L ${points[i].x} ${points[i].y}`;
    }

    let svg = `<svg width="${width}" height="${height}" style="margin-top: 8px;">`;

    // Draw daylight/night background rects using server-provided solar day segments
    if (solarDays && solarDays.length > 0 && kpEntries.length > 1) {
        const totalMs = kpEntries[kpEntries.length - 1].endDate - kpEntries[0].startDate;
        const startMs = kpEntries[0].startDate.getTime();
        for (const {startDate, endDate} of kpEntries) {
            const lightType = getLightType(startDate, endDate, solarDays);
            const color = lightType === 'day'
                ? 'rgba(255,255,255,0.10)'
                : lightType === 'night'
                    ? 'rgba(0,0,0,0.28)'
                    : 'rgba(255,255,255,0.03)';
            const rx = padding + ((startDate - startMs) / totalMs) * graphWidth;
            const rw = ((endDate - startDate) / totalMs) * graphWidth;
            svg += `<rect x="${rx}" y="${padding}" width="${rw}" height="${graphHeight}" fill="${color}"/>`;
        }
    }

    svg += `<path d="${pathData}" stroke="var(--accent-pink)" stroke-width="1.5" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`;
    for (const point of points) {
        svg += `<circle cx="${point.x}" cy="${point.y}" r="1.5" fill="var(--accent-pink)"/>`;
    }
    svg += `</svg>`;

    return svg;
}

// Extract G-level events from forecast and find next/highest
function analyzeGEvents(forecast) {
    if (!forecast || !forecast.data || !forecast.data.table) {
        return {hasGEvents: false, nextEvent: null, highestEvent: null};
    }

    const pf = forecast.data;
    const now = new Date();
    const gEvents = [];

    // Iterate through periods and days to find all G events
    for (let dayIndex = 0; dayIndex < pf.days.length; dayIndex++) {
        const dayStr = pf.days[dayIndex];

        for (const period of pf.periods) {
            const entries = pf.table[period] || [];
            const entry = entries[dayIndex];

            if (!entry || !entry.g_scale) continue;

            // Parse the time period
            const match = period.match(/(\d{2})-(\d{2})UT/);
            if (!match) continue;

            const startHour = parseInt(match[1]);
            const endHour = parseInt(match[2]);

            // Create UTC date for start time
            const startDate = parseUtcDate(dayStr, startHour);

            // Handle day rollover
            let endDate = parseUtcDate(dayStr, endHour);
            if (endHour < startHour) {
                endDate.setDate(endDate.getDate() + 1);
            }

            gEvents.push({
                gScale: entry.g_scale,
                gValue: extractGNumber(entry.g_scale),
                startDate: startDate,
                endDate: endDate,
                period: period,
                dayStr: dayStr
            });
        }
    }

    if (gEvents.length === 0) {
        return {hasGEvents: false, nextEvent: null, highestEvent: null};
    }

    // Find next event (closest to now, in the future)
    let nextEvent = null;
    let minDiff = Infinity;
    for (const evt of gEvents) {
        const diff = evt.startDate - now;
        if (diff >= 0 && diff < minDiff) {
            minDiff = diff;
            nextEvent = evt;
        }
    }

    // Find highest G-level event
    let highestEvent = gEvents[0];
    for (const evt of gEvents) {
        if (evt.gValue > highestEvent.gValue) {
            highestEvent = evt;
        }
    }

    return {hasGEvents: true, nextEvent: nextEvent || gEvents[0], highestEvent};
}

// Extract numeric value from G-scale string (e.g., "G1" -> 1)
function extractGNumber(gScale) {
    const m = String(gScale).match(/G(\d+)/);
    return m ? parseInt(m[1]) : 0;
}

export function init(el) {
    container = el;
    body = el.querySelector('.panel-body') || el;
    if (!body) return;

    // Use similar structure as sun/moon panels
    body.innerHTML = `
        <div class="sun-wrapper">
            <div class="sun-event">
                <div style="font-weight: bold; margin-bottom: 8px;">Aurora Forecast</div>
                <div id="aurora-panel-sparkline"></div>
            </div>
            <div class="sun-meta" style="width: 100%;">
                <div class="sun-length sun-day" style="flex: 1;">
                    <div class="sun-label">Likelihood</div>
                    <div class="sun-value sun-day-value">—</div>
                </div>
                <div class="sun-length sun-night" style="flex: 1;">
                    <div class="sun-label">G Events</div>
                    <div class="sun-value sun-night-value">—</div>
                </div>
            </div>
            <div id="aurora-panel-next" style="margin-top: 12px; display: none;">
                <div style="font-size: 0.9em; color: var(--accent-pink); font-weight: bold;" id="aurora-panel-next-text">—</div>
            </div>
            <div id="aurora-panel-highest" style="margin-top: 8px; display: none;">
                <div style="font-size: 0.9em; color: var(--accent-pink); font-weight: bold;" id="aurora-panel-highest-text">—</div>
            </div>
        </div>
    `;

    // Fetch current data
    fetch('/api/tabs/aurora/action/get-current', {method: 'POST'})
        .then(resp => resp.json())
        .then(data => {
            if (!data) return;
            updatePanel(data);
        })
        .catch(err => console.error('Failed to fetch aurora panel data:', err));
}

function updatePanel(data) {
    if (!body) return;

    const current = data.current || {};
    const forecast = data.forecast || {};

    // Render sparkline
    const sparklineEl = body.querySelector('#aurora-panel-sparkline');
    if (sparklineEl && forecast && forecast.data) {
        const solarDays = data.solar_days || [];
        const sparklineHtml = renderSparkline(forecast, 200, solarDays);
        sparklineEl.innerHTML = sparklineHtml || '';
    }

    // Update likelihood
    const likelihoodEl = body.querySelector('.sun-day-value');
    if (likelihoodEl) {
        const likelihood = current.likelihood !== undefined ? Math.round(current.likelihood) : '—';
        likelihoodEl.textContent = `${likelihood}%`;
    }

    // Analyze G events
    const analysis = analyzeGEvents(forecast);

    // Update G events summary
    const gEventsEl = body.querySelector('.sun-night-value');
    if (gEventsEl) {
        if (analysis.hasGEvents) {
            gEventsEl.textContent = 'Yes';
            gEventsEl.style.color = 'var(--accent-pink)';
        } else {
            gEventsEl.textContent = 'None';
            gEventsEl.style.color = 'var(--text)';
        }
    }

    // Show next and highest events if present
    const nextDiv = body.querySelector('#aurora-panel-next');
    const highestDiv = body.querySelector('#aurora-panel-highest');

    if (analysis.nextEvent) {
        const now = new Date();
        const diff = analysis.nextEvent.startDate - now;
        const timeStr = formatRemaining(diff);
        const nextText = body.querySelector('#aurora-panel-next-text');
        if (nextText) {
            nextText.textContent = `Next ${analysis.nextEvent.gScale} in ${timeStr}`;
        }
        if (nextDiv) nextDiv.style.display = 'block';
    } else {
        if (nextDiv) nextDiv.style.display = 'none';
    }

    // Only show highest if different from next
    if (analysis.highestEvent && analysis.nextEvent &&
        analysis.highestEvent.gValue !== analysis.nextEvent.gValue) {
        const now = new Date();
        const diff = analysis.highestEvent.startDate - now;
        const timeStr = formatRemaining(diff);
        const highestText = body.querySelector('#aurora-panel-highest-text');
        if (highestText) {
            highestText.textContent = `Highest ${analysis.highestEvent.gScale} in ${timeStr}`;
        }
        if (highestDiv) highestDiv.style.display = 'block';
    } else if (analysis.highestEvent && !analysis.nextEvent) {
        // No next event but have highest
        const now = new Date();
        const diff = analysis.highestEvent.startDate - now;
        const timeStr = formatRemaining(diff);
        const highestText = body.querySelector('#aurora-panel-highest-text');
        if (highestText) {
            highestText.textContent = `Highest ${analysis.highestEvent.gScale} in ${timeStr}`;
        }
        if (highestDiv) highestDiv.style.display = 'block';
    } else {
        if (highestDiv) highestDiv.style.display = 'none';
    }
}

export function onMessage(msg) {
    if (!container) return;
    if (msg.serviceType !== 'aurora') return;

    // On aurora check or forecast messages, refresh the panel
    if (msg.event === 'aurora_check' || msg.event === 'aurora_forecast') {
        fetch('/api/tabs/aurora/action/get-current', {method: 'POST'})
            .then(resp => resp.json())
            .then(data => {
                if (data) updatePanel(data);
            })
            .catch(err => console.error('Failed to fetch aurora panel data:', err));
    }
}
