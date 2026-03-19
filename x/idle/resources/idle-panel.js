let panelBody = null;

export function init(el) {
    panelBody = el.querySelector('.panel-body') || el;
    if (!panelBody) return;

    // Fetch dashboard data and render panel
    fetch('/api/tabs/idle/action/fetch-idle-dashboard', {method: 'POST'})
        .then(resp => resp.json())
        .then(data => {
            if (data && data.data) {
                updatePanel(data.data);
            }
        })
        .catch(err => console.error('Failed to fetch idle panel data:', err));
}

export function onMessage(msg) {
    if (!panelBody) return;

    // Refetch on any idle message
    if (msg.event === 'idle' || msg['data-type'] === 'service.idle.v1') {
        fetch('/api/tabs/idle/action/fetch-idle-dashboard', {method: 'POST'})
            .then(resp => resp.json())
            .then(data => {
                if (data && data.data) {
                    updatePanel(data.data);
                }
            })
            .catch(err => console.error('Failed to refresh idle panel:', err));
    }
}

function formatSeconds(seconds) {
    if (isNaN(seconds)) return '—';
    const totalSec = Math.floor(seconds);
    const hours = Math.floor(totalSec / 3600);
    const minutes = Math.floor((totalSec % 3600) / 60);
    const secs = totalSec % 60;

    if (hours > 0) {
        return `${hours}h ${minutes}m`;
    } else if (minutes > 0) {
        return `${minutes}m ${secs}s`;
    } else {
        return `${secs}s`;
    }
}

function getStatusColor(status) {
    switch (status) {
        case 'idle':
            return 'var(--accent-blue, #9b5af0)';
        case 'active':
            return 'var(--accent-green, #10b981)';
        default:
            return 'var(--text)';
    }
}

function updatePanel(data) {
    if (!panelBody) return;

    // Build HTML similar to status panel structure
    let html = '<div style="display: flex; flex-direction: column; height: 100%; padding: 12px;">';

    // Top section - Current State
    html += '<div style="flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center;">';
    const currentColor = getStatusColor(data.currentStatus);
    const capitalCurrent = data.currentStatus.charAt(0).toUpperCase() + data.currentStatus.slice(1);
    html += `<div style="font-size: 1.2rem; font-weight: bold; color: ${currentColor};">${capitalCurrent}</div>`;
    html += `<div style="font-size: 0.8rem; color: var(--text); margin-top: 4px;">${formatSeconds(data.timeSinceChangeSeconds)}</div>`;
    html += '</div>';

    // Middle section - Header
    html += `<div style="font-weight: bold; margin-bottom: 8px; text-align: center; color: var(--accent);">Idle Activity</div>`;

    // Bottom section - 24h Stats
    html += '<div style="flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center; width: 100%;">';
    html += '<div style="font-size: 0.75rem; color: var(--text); opacity: 0.7; margin-bottom: 6px;">Last 24 Hours</div>';

    // Stats in table for alignment
    html += '<table style="border-collapse: collapse; font-size: 0.8rem;">';
    html += `<tr><td style="color: var(--accent-green, #10b981); padding: 2px 8px; text-align: right;">Active:</td><td style="padding: 2px 8px; text-align: right;">${formatSeconds(data.totalActiveSeconds)}</td></tr>`;
    html += `<tr><td style="color: var(--accent-blue, #9b5af0); padding: 2px 8px; text-align: right;">Idle:</td><td style="padding: 2px 8px; text-align: right;">${formatSeconds(data.totalIdleSeconds)}</td></tr>`;
    if (data.totalUnknownSeconds > 300) {
        html += `<tr><td style="color: var(--text); opacity: 0.7; padding: 2px 8px; text-align: right;">Unknown:</td><td style="padding: 2px 8px; text-align: right;">${formatSeconds(data.totalUnknownSeconds)}</td></tr>`;
    }
    html += '</table>';
    html += '</div>';

    html += '</div>';

    panelBody.innerHTML = html;
}
