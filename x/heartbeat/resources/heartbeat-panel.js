let panelBody = null;
let heartbeats = {}; // Map of "serviceName:hostname" -> heartbeat info

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

export async function init(el) {
    panelBody = el.querySelector('.panel-body') || el;
    if (!panelBody) return;

    // Initialize with placeholder
    panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Waiting for heartbeat data...</div>';
}

export function onMessage(msg) {
    if (!panelBody) return;

    // Listen for uptime_check events from all services
    if (msg.event === 'uptime_check' && msg.data) {
        const serviceName = msg.serviceName || msg.serviceType || 'unknown';
        const hostname = msg.hostname || 'unknown';
        const data = msg.data;

        // Use composite key to track per service per host
        const key = `${serviceName}:${hostname}`;

        // Update heartbeat info
        heartbeats[key] = {
            serviceName: serviceName,
            hostname: hostname,
            uptime: data.uptime || '—',
            uptimeSeconds: data.uptimeSeconds || 0,
            lastSeen: '0m',
            lastSeenTime: new Date(data.now || msg.timestamp)
        };

        updatePanel();
    }
}

function updatePanel() {
    // Load total known hosts from localStorage
    let totalHosts = Object.keys(heartbeats).length;
    try {
        const saved = localStorage.getItem('heartbeat-state');
        if (saved) {
            const parsed = JSON.parse(saved);
            const allKnown = parsed.heartbeats || {};
            totalHosts = Object.keys(allKnown).length;
        }
    } catch (e) {
        console.error('Failed to load heartbeat state:', e);
    }

    if (!panelBody || Object.keys(heartbeats).length === 0) {
        if (panelBody) {
            panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">No heartbeat data available</div>';
        }
        return;
    }

    // Sort by hostname then service name
    const sorted = Object.values(heartbeats).sort((a, b) => {
        if (a.hostname !== b.hostname) {
            return a.hostname.localeCompare(b.hostname);
        }
        return a.serviceName.localeCompare(b.serviceName);
    });

    // Calculate last seen for each and analyze
    const now = new Date();
    let shortestUptime = null;
    let longestUptime = null;
    let staleHosts = [];

    for (const hb of sorted) {
        if (hb.lastSeenTime) {
            const diff = now - hb.lastSeenTime;
            hb.lastSeen = formatDuration(diff);

            // Check if stale (> 5 minutes)
            if (diff > 5 * 60 * 1000) {
                staleHosts.push(hb.hostname);
            }
        }

        // Track shortest and longest uptime
        if (hb.uptimeSeconds) {
            if (!shortestUptime || hb.uptimeSeconds < shortestUptime.uptimeSeconds) {
                shortestUptime = hb;
            }
            if (!longestUptime || hb.uptimeSeconds > longestUptime.uptimeSeconds) {
                longestUptime = hb;
            }
        }
    }

    // Build summary panel with title
    let html = '<div style="display: flex; flex-direction: column; gap: 16px; font-size: 0.9rem; align-items: center;">';

    // Title
    html += '<div style="font-weight: 700; font-size: 1rem; color: var(--accent);">Heartbeat</div>';

    // Host count - showing "currently reporting / total known"
    const currentlyReporting = Object.keys(heartbeats).length;
    html += '<div style="display: flex; flex-direction: column; gap: 4px; text-align: center;">';
    html += `<span style="opacity: 0.7; font-size: 0.85rem;">Hosts Reporting</span>`;
    html += `<span style="font-weight: 600; color: var(--accent); font-size: 1.1rem;">${currentlyReporting}/${totalHosts}</span>`;
    html += '</div>';

    // Shortest uptime
    if (shortestUptime) {
        html += '<div style="display: flex; flex-direction: column; gap: 4px; text-align: center;">';
        html += `<span style="opacity: 0.7; font-size: 0.85rem;">Shortest Uptime</span>`;
        html += `<span style="color: var(--text); font-size: 1.1rem;">${formatUptime(shortestUptime.uptime)}</span>`;
        html += '</div>';
    }

    // Longest uptime
    if (longestUptime) {
        html += '<div style="display: flex; flex-direction: column; gap: 4px; text-align: center;">';
        html += `<span style="opacity: 0.7; font-size: 0.85rem;">Longest Uptime</span>`;
        html += `<span style="color: var(--text); font-size: 1.1rem;">${formatUptime(longestUptime.uptime)}</span>`;
        html += '</div>';
    }

    // Stale hosts in red
    if (staleHosts.length > 0) {
        html += '<div style="margin-top: 8px; padding-top: 12px; border-top: 1px solid var(--border); width: 100%; text-align: center;">';
        html += `<div style="color: #ff6b6b; font-weight: 600; margin-bottom: 6px;">Stale (>5m):</div>`;
        for (const hostname of staleHosts) {
            html += `<div style="color: #ff6b6b; opacity: 0.9; padding: 2px 0;">${hostname}</div>`;
        }
        html += '</div>';
    }

    html += '</div>';

    panelBody.innerHTML = html;
}
