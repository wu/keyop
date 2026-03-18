let container = null;

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

function parseUtcHours(periodStr) {
    const match = periodStr.match(/(\d{2})-(\d{2})UT/);
    if (!match) return {start: 0, end: 0};
    return {
        start: parseInt(match[1]),
        end: parseInt(match[2])
    };
}

function convertUtcToLocal(utcHour, dateStr) {
    // Parse the date string (e.g., "Mar 18")
    const parts = dateStr.trim().split(/\s+/);
    const month = parts[0];
    const day = parseInt(parts[1]);

    // Create a UTC date at the specified hour
    const monthMap = {
        'Jan': '01', 'Feb': '02', 'Mar': '03', 'Apr': '04', 'May': '05', 'Jun': '06',
        'Jul': '07', 'Aug': '08', 'Sep': '09', 'Oct': '10', 'Nov': '11', 'Dec': '12'
    };
    const monthNum = monthMap[month] || '03';
    const year = new Date().getFullYear();
    const utcDateStr = `${year}-${monthNum}-${String(day).padStart(2, '0')}T${String(utcHour).padStart(2, '0')}:00:00Z`;

    const utcDate = new Date(utcDateStr);
    return utcDate;
}

function renderForecastTable(forecast) {
    if (!forecast || !forecast.data) return '';
    const pf = forecast.data;
    if (!pf.days || !pf.periods || !pf.table) return '';

    let html = '<table style="border-collapse: collapse; width: 100%;"><thead><tr>';
    html += '<th style="border: 1px solid var(--task-row-border); padding: 8px;">Date</th>';
    html += '<th style="border: 1px solid var(--task-row-border); padding: 8px;">Start Time</th>';
    html += '<th style="border: 1px solid var(--task-row-border); padding: 8px;">End Time</th>';
    html += '<th style="border: 1px solid var(--task-row-border); padding: 8px;">Time Until</th>';
    html += '<th style="border: 1px solid var(--task-row-border); padding: 8px;">Aurora Level</th>';
    html += '<th style="border: 1px solid var(--task-row-border); padding: 8px;">G Event</th>';
    html += '</tr></thead><tbody>';

    const now = new Date();

    // For each day and each period, create a row (sorted chronologically)
    for (let dayIndex = 0; dayIndex < pf.days.length; dayIndex++) {
        const dayStr = pf.days[dayIndex];

        for (const period of pf.periods) {
            const entries = pf.table[period] || [];
            const entry = entries[dayIndex];
            const {start: startHour, end: endHour} = parseUtcHours(period);

            // Skip empty entries (marked with no kp value and empty raw)
            if (!entry || (entry.kp === null && entry.kp === undefined && (!entry.raw || entry.raw === ''))) {
                continue;
            }

            // Convert UTC times to local
            const startUtcDate = convertUtcToLocal(startHour, dayStr);
            const endUtcDate = convertUtcToLocal(endHour, dayStr);

            // Handle day rollover (e.g., 21-00UT becomes next day at 00:00)
            if (endHour < startHour) {
                endUtcDate.setDate(endUtcDate.getDate() + 1);
            }

            // Skip rows in the past
            if (endUtcDate < now) {
                continue;
            }

            // Format date as "tue may 17" (weekday, month, day)
            const dateStr = startUtcDate.toLocaleDateString('en-US', {
                weekday: 'short',
                month: 'short',
                day: 'numeric'
            });
            const startTimeStr = startUtcDate.toLocaleTimeString('en-US', {
                hour: '2-digit',
                minute: '2-digit',
                hour12: true
            });
            const endTimeStr = endUtcDate.toLocaleTimeString('en-US', {
                hour: '2-digit',
                minute: '2-digit',
                hour12: true
            });

            // Calculate time until start
            const timeUntilStart = startUtcDate - now;
            let timeUntilStartStr = formatRemaining(timeUntilStart);
            // Display "now" if the time until is 0m
            if (timeUntilStartStr === '0m') {
                timeUntilStartStr = 'now';
            }

            // Format aurora level (KP value only)
            let levelText = '';
            if (entry.kp !== null && entry.kp !== undefined) {
                levelText = entry.kp.toFixed(2);
            }

            // Format G-level event if present
            let gEventText = '';
            if (entry.g_scale) {
                gEventText = entry.g_scale;
            }

            html += `<tr>`;
            html += `<td style="border: 1px solid var(--task-row-border); padding: 8px;">${dateStr}</td>`;
            html += `<td style="border: 1px solid var(--task-row-border); padding: 8px;">${startTimeStr}</td>`;
            html += `<td style="border: 1px solid var(--task-row-border); padding: 8px;">${endTimeStr}</td>`;
            html += `<td style="border: 1px solid var(--task-row-border); padding: 8px;">${timeUntilStartStr}</td>`;
            html += `<td style="border: 1px solid var(--task-row-border); padding: 8px;">${levelText}</td>`;
            if (gEventText) {
                html += `<td style="border: 1px solid var(--task-row-border); padding: 8px; color: var(--accent-pink); font-weight: bold;">${gEventText}</td>`;
            } else {
                html += `<td style="border: 1px solid var(--task-row-border); padding: 8px;"></td>`;
            }
            html += `</tr>`;
        }
    }
    html += '</tbody></table>';
    return html;
}

// Extract KP values from forecast data in chronological order, optionally filtered to future only
function extractKpValues(forecast, futureOnly = false) {
    if (!forecast || !forecast.data || !forecast.data.table) {
        return [];
    }

    const pf = forecast.data;
    const kpValues = [];
    const now = new Date();

    // Iterate through days and periods to collect all KP values in order
    for (let dayIndex = 0; dayIndex < pf.days.length; dayIndex++) {
        const dayStr = pf.days[dayIndex];

        for (const period of pf.periods) {
            const entries = pf.table[period] || [];
            const entry = entries[dayIndex];

            if (!entry || (entry.kp === null && entry.kp === undefined)) {
                continue;
            }

            // If filtering for future only, skip past entries
            if (futureOnly) {
                const {start: startHour} = parseUtcHours(period);
                const startDate = convertUtcToLocal(startHour, dayStr);
                if (startDate < now) {
                    continue;
                }
            }

            if (entry.kp !== null && entry.kp !== undefined) {
                kpValues.push(entry.kp);
            }
        }
    }

    return kpValues;
}

// Render a sparkline graph showing KP values over time
function renderSparkline(forecast, width = 300, futureOnly = true) {
    const kpValues = extractKpValues(forecast, futureOnly);
    if (kpValues.length === 0) {
        return '';
    }

    const height = 60;
    const padding = 5;
    const graphWidth = width - (padding * 2);
    const graphHeight = height - (padding * 2);

    // Find min and max for scaling
    const minKp = Math.min(...kpValues);
    const maxKp = Math.max(...kpValues);
    const range = maxKp - minKp || 1; // Avoid division by zero

    // Calculate points for the sparkline
    const points = kpValues.map((kp, i) => {
        const x = padding + (i / (kpValues.length - 1 || 1)) * graphWidth;
        const normalizedY = (kp - minKp) / range;
        const y = padding + graphHeight - (normalizedY * graphHeight);
        return {x, y, kp};
    });

    // Create SVG path for the line
    let pathData = `M ${points[0].x} ${points[0].y}`;
    for (let i = 1; i < points.length; i++) {
        pathData += ` L ${points[i].x} ${points[i].y}`;
    }

    // Create SVG
    let svg = `<svg width="${width}" height="${height}" style="margin-bottom: 12px;">`;

    // Draw grid lines for reference
    svg += `<line x1="${padding}" y1="${padding}" x2="${padding}" y2="${padding + graphHeight}" stroke="var(--task-row-border)" stroke-width="1"/>`;
    svg += `<line x1="${padding}" y1="${padding + graphHeight}" x2="${padding + graphWidth}" y2="${padding + graphHeight}" stroke="var(--task-row-border)" stroke-width="1"/>`;

    // Draw the sparkline
    svg += `<path d="${pathData}" stroke="var(--accent-pink)" stroke-width="2" fill="none" stroke-linecap="round" stroke-linejoin="round"/>`;

    // Draw points on the sparkline
    for (const point of points) {
        svg += `<circle cx="${point.x}" cy="${point.y}" r="2" fill="var(--accent-pink)"/>`;
    }

    // Add labels for min and max
    svg += `<text x="${padding}" y="${height - 2}" font-size="10" fill="var(--text)" text-anchor="start">${minKp.toFixed(1)}</text>`;
    svg += `<text x="${padding + graphWidth}" y="${height - 2}" font-size="10" fill="var(--text)" text-anchor="end">${maxKp.toFixed(1)}</text>`;

    svg += `</svg>`;

    return svg;
}

export function init(el) {
    container = el;
    const statusEl = container.querySelector('#aurora-status');
    if (statusEl) statusEl.textContent = 'Loading aurora forecast...';

    // Fetch current forecast from the server-side sqlite cache
    fetch('/api/tabs/aurora/action/get-current', {method: 'POST'})
        .then(resp => resp.json())
        .then(data => {
            if (!data) return;
            const cur = data.current;
            const statusEl = container.querySelector('#aurora-status');
            if (cur && statusEl) {
                let text = 'Aurora forecast available';
                if (cur.likelihood !== undefined && cur.likelihood !== null) {
                    text = `Likelihood: ${cur.likelihood}%`;
                }
                if (cur.forecast_time) {
                    try {
                        text = `${text} (${new Date(cur.forecast_time).toLocaleString()})`;
                    } catch (e) {
                        text = `${text} (${cur.forecast_time})`;
                    }
                }
                statusEl.textContent = text;
                statusEl.style.color = '#7c4dff';
            }

            // Render latest forecast as table if present
            let forecastEl = container.querySelector('#aurora-forecast');
            if (!forecastEl) {
                const historyEl = container.querySelector('#aurora-history');
                const pre = document.createElement('div');
                pre.id = 'aurora-forecast';
                if (historyEl && historyEl.parentNode) {
                    historyEl.parentNode.insertBefore(pre, historyEl);
                } else {
                    container.appendChild(pre);
                }
                forecastEl = pre;
            }
            if (data.forecast && data.forecast.data) {
                const sparklineHtml = renderSparkline(data.forecast);
                const tableHtml = renderForecastTable(data.forecast);
                forecastEl.innerHTML = (sparklineHtml || '') + (tableHtml || 'No forecast available');
            }
        })
        .catch(err => console.error('Failed to fetch current aurora forecast:', err));
}

export function onMessage(msg) {
    if (!container) return;

    // Handle aurora_check events (current likelihood and forecast time)
    if (msg.event === 'aurora_check') {
        const statusEl = container.querySelector('#aurora-status');
        const historyEl = container.querySelector('#aurora-history');

        let likelihood = null;
        let forecastTime = null;
        if (msg.data && typeof msg.data === 'object') {
            likelihood = msg.data.likelihood ?? msg.data.Likelihood ?? msg.data.metric ?? null;
            forecastTime = msg.data.forecast_time ?? msg.data.ForecastTime ?? msg.data.forecastTime ?? null;
        }

        let text = msg.text || 'Aurora forecast available';
        if (likelihood !== null) text = `Likelihood: ${likelihood}%`;
        if (forecastTime) {
            try {
                text = `${text} (${new Date(forecastTime).toLocaleString()})`;
            } catch (e) {
                text = `${text} (${forecastTime})`;
            }
        }

        if (statusEl) {
            statusEl.textContent = text;
            statusEl.style.color = '#7c4dff';
        }

        if (historyEl) {
            const item = document.createElement('div');
            item.className = 'event-item';
            item.textContent = `[${new Date().toLocaleTimeString()}] ${statusEl ? statusEl.textContent : (msg.text || msg.event)}`;
            historyEl.prepend(item);
            if (historyEl.children.length > 50) {
                historyEl.removeChild(historyEl.lastChild);
            }
        }
    }

    // Handle aurora_forecast events (3-day forecast table)
    if (msg.event === 'aurora_forecast') {
        let forecastEl = container.querySelector('#aurora-forecast');
        if (!forecastEl) {
            const historyEl = container.querySelector('#aurora-history');
            const div = document.createElement('div');
            div.id = 'aurora-forecast';
            if (historyEl && historyEl.parentNode) {
                historyEl.parentNode.insertBefore(div, historyEl);
            } else {
                container.appendChild(div);
            }
            forecastEl = div;
        }
        let dataObj = null;
        if (msg.data && typeof msg.data === 'object') {
            dataObj = msg.data.data ?? msg.data;
        } else {
            dataObj = msg.data;
        }
        const sparklineHtml = renderSparkline({data: dataObj});
        const tableHtml = renderForecastTable({data: dataObj});
        forecastEl.innerHTML = (sparklineHtml || '') + (tableHtml || 'No forecast available');
    }
}
