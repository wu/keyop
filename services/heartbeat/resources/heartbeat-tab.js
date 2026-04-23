let container = null;
let heartbeats = {}; // Map of "hostname" -> heartbeat info
let sortColumn = 'uptime'; // Default sort by uptime
let sortAscending = true; // Sort ascending (shortest first)

function formatDuration(ms) {
    if (ms < 0) ms = 0;
    ms = Math.floor(ms / 1000) * 1000; // Round to seconds

    const days = Math.floor(ms / (24 * 60 * 60 * 1000));
    const hours = Math.floor((ms % (24 * 60 * 60 * 1000)) / (60 * 60 * 1000));
    const minutes = Math.floor((ms % (60 * 60 * 1000)) / (60 * 1000));

    if (days > 0) {
        return `${days}d ${hours}h`;
    }
    if (hours > 0) {
        return `${hours}h ${minutes}m`;
    }
    return `${minutes}m`;
}

function formatUptime(uptimeStr) {
    // Parse uptime string like "121h8m3s" or "1h 2m 3s"
    // Return formatted as "Xd Yh" or "Xh Ym" or just "Xm"

    const dMatch = uptimeStr.match(/(\d+)d/);
    const hMatch = uptimeStr.match(/(\d+)h/);
    const mMatch = uptimeStr.match(/(\d+)m/);

    let days = dMatch ? parseInt(dMatch[1]) : 0;
    let hours = hMatch ? parseInt(hMatch[1]) : 0;
    const minutes = mMatch ? parseInt(mMatch[1]) : 0;

    // Convert excess hours to days
    if (hours >= 24) {
        days += Math.floor(hours / 24);
        hours = hours % 24;
    }

    if (days > 0) {
        return `${days}d ${hours}h`;
    }
    if (hours > 0) {
        return `${hours}h ${minutes}m`;
    }
    return `${minutes}m`;
}

export function init(el) {
    container = el.querySelector('#heartbeat-list') || el;
    if (!container) return;

    // Load persisted state from localStorage
    try {
        const saved = localStorage.getItem('heartbeat-state');
        if (saved) {
            const parsed = JSON.parse(saved);
            heartbeats = parsed.heartbeats || {};
            sortColumn = parsed.sortColumn || 'uptime';
            sortAscending = parsed.sortAscending !== false;

            // Convert lastSeenTime strings back to Date objects
            for (const hostname in heartbeats) {
                if (heartbeats[hostname].lastSeenTime) {
                    heartbeats[hostname].lastSeenTime = new Date(heartbeats[hostname].lastSeenTime);
                }
            }
        }
    } catch (e) {
        console.error('Failed to load heartbeat state:', e);
    }

    container.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Waiting for heartbeat data...</div>';
}

export function onMessage(msg) {
    if (!container) return;

    // Listen for heartbeat events from the new messenger
    if (msg.dataType === 'service.heartbeat.v1' && msg.payload) {
        const hostname = msg.hostname || msg.payload.hostname || 'unknown';
        const data = msg.payload;

        // Update heartbeat info
        heartbeats[hostname] = {
            hostname: hostname,
            uptime: data.uptime || '—',
            uptimeSeconds: data.uptimeSeconds || 0,
            lastSeen: '0m',
            lastSeenTime: new Date(data.now || msg.timestamp)
        };

        // Persist state
        persistState();
        updatePanel();
    }
}

function persistState() {
    try {
        localStorage.setItem('heartbeat-state', JSON.stringify({
            heartbeats: heartbeats,
            sortColumn: sortColumn,
            sortAscending: sortAscending
        }));
    } catch (e) {
        console.error('Failed to persist heartbeat state:', e);
    }
}

function updatePanel() {
    if (!container || Object.keys(heartbeats).length === 0) {
        if (container) {
            container.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">No heartbeat data available</div>';
        }
        return;
    }

    // Sort heartbeats
    const sorted = Object.values(heartbeats).sort((a, b) => {
        let aVal, bVal;

        if (sortColumn === 'hostname') {
            aVal = a.hostname;
            bVal = b.hostname;
            const cmp = aVal.localeCompare(bVal);
            return sortAscending ? cmp : -cmp;
        } else if (sortColumn === 'uptime') {
            aVal = a.uptimeSeconds || 0;
            bVal = b.uptimeSeconds || 0;
            return sortAscending ? (aVal - bVal) : (bVal - aVal);
        } else if (sortColumn === 'lastSeen') {
            aVal = a.lastSeenTime ? a.lastSeenTime.getTime() : 0;
            bVal = b.lastSeenTime ? b.lastSeenTime.getTime() : 0;
            return sortAscending ? (aVal - bVal) : (bVal - aVal);
        }
        return 0;
    });

    // Calculate last seen for each
    const now = new Date();
    for (const hb of sorted) {
        if (hb.lastSeenTime) {
            const diff = now - hb.lastSeenTime;
            hb.lastSeen = formatDuration(diff);
        }
    }

    // Build table with clickable headers
    let html = '<table style="width: 100%; font-size: 0.85rem; border-collapse: collapse;">';
    html += '<tr style="border-bottom: 1px solid var(--border); cursor: pointer;">';

    // Hostname header
    html += '<th style="text-align: left; padding: 8px; font-weight: 600; color: var(--text); opacity: 0.7; cursor: pointer;" onclick="sortBy(\'hostname\')">';
    html += 'Hostname';
    if (sortColumn === 'hostname') html += sortAscending ? ' ▲' : ' ▼';
    html += '</th>';

    // Uptime header
    html += '<th style="text-align: left; padding: 8px; font-weight: 600; color: var(--text); opacity: 0.7; cursor: pointer;" onclick="sortBy(\'uptime\')">';
    html += 'Uptime';
    if (sortColumn === 'uptime') html += sortAscending ? ' ▲' : ' ▼';
    html += '</th>';

    // Last Seen header
    html += '<th style="text-align: left; padding: 8px; font-weight: 600; color: var(--text); opacity: 0.7; cursor: pointer;" onclick="sortBy(\'lastSeen\')">';
    html += 'Last Seen';
    if (sortColumn === 'lastSeen') html += sortAscending ? ' ▲' : ' ▼';
    html += '</th>';

    html += '<th style="padding: 8px; width: 24px;"></th>';
    html += '</tr>';

    for (const hb of sorted) {
        const diff = hb.lastSeenTime ? (now - hb.lastSeenTime) : 0;
        const isStale = diff > 5 * 60 * 1000; // > 5 minutes
        const rowBg = isStale ? 'background-color: rgba(255, 220, 0, 0.15);' : '';
        const safeHostname = hb.hostname.replace(/'/g, "\\'");

        html += `<tr style="border-bottom: 1px solid var(--border, #333); ${rowBg}">`;
        html += `<td style="padding: 8px; color: var(--text);">${hb.hostname}</td>`;
        html += `<td style="padding: 8px; color: var(--text);">${formatUptime(hb.uptime)}</td>`;
        html += `<td style="padding: 8px; color: var(--text); opacity: 0.7;">${hb.lastSeen}</td>`;
        html += `<td style="padding: 4px 8px; text-align: right;"><button onclick="removeHost('${safeHostname}')" style="background: none; border: none; color: var(--text); opacity: 0.4; cursor: pointer; font-size: 0.9rem; padding: 0 2px; line-height: 1;" title="Remove from list">✕</button></td>`;
        html += '</tr>';
    }

    html += '</table>';
    container.innerHTML = html;
}

// Make sortBy and removeHost available globally for onclick handlers
window.sortBy = function (column) {
    if (sortColumn === column) {
        sortAscending = !sortAscending;
    } else {
        sortColumn = column;
        sortAscending = column === 'uptime'; // Default ascending for uptime
    }
    persistState();
    updatePanel();
};

window.removeHost = function (hostname) {
    delete heartbeats[hostname];
    persistState();
    updatePanel();
};
