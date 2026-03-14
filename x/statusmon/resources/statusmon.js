let statusmonContainer = null;
let currentStatuses = {}; // Cache of current status items keyed by name

import {formatElapsedTime, startElapsedTimeUpdates} from '/js/time-formatter.js';

const LEVEL_COLORS = {
    'ok': '#10b981',         // green
    'warning': '#f59e0b',    // amber
    'critical': '#ef4444',   // red
    'error': '#dc2626',      // dark red
    'info': '#3b82f6'        // blue
};

export async function init(container) {
    statusmonContainer = container;
    await refreshStatus();
    startElapsedTimeUpdates();
}

export function onMessage(msg) {
    if (!statusmonContainer) return;

    // Filter for status-related messages: check DataType for core.status.v1
    // Note: The JSON field is "data-type" (with hyphen), not "dataType"
    if (msg['data-type'] !== 'core.status.v1') {
        return;
    }

    // Extract the status data from the message
    if (msg.data && msg.data.name) {
        const statusData = msg.data;

        // If this is a new service (not in cache), refresh from sqlite to get all current services
        if (!currentStatuses.hasOwnProperty(statusData.name)) {
            refreshStatus();
            return;
        }

        // For existing items, update the cache
        currentStatuses[statusData.name] = {
            name: statusData.name,
            status: statusData.status,
            details: statusData.details,
            level: statusData.level,
            hostname: statusData.hostname,
            lastSeen: new Date().toISOString() // Use current time for last seen
        };

        // Re-render the display with updated data
        renderStatusList();
    }
}

async function refreshStatus() {
    if (!statusmonContainer) return;

    try {
        const response = await fetch('/api/tabs/statusmon/action/fetch-status', {
            method: 'POST',
        });

        if (!response.ok) {
            const list = statusmonContainer.querySelector('#statusmon-list');
            if (list) {
                list.innerHTML = `<div class="error">Error loading status: ${response.statusText}</div>`;
            }
            return;
        }

        const result = await response.json();
        const statuses = result.statuses || [];

        let list = statusmonContainer.querySelector('#statusmon-list');

        // If the layout doesn't exist yet, create it
        if (!list) {
            statusmonContainer.innerHTML = '<div class="statusmon-layout"><div class="statusmon-content"><div id="statusmon-list"></div></div></div>';
            list = statusmonContainer.querySelector('#statusmon-list');
        }

        // Cache the initial status data
        currentStatuses = {};
        statuses.forEach(status => {
            currentStatuses[status.name] = status;
        });

        renderStatusList();
    } catch (err) {
        const list = statusmonContainer.querySelector('#statusmon-list');
        if (list) {
            list.innerHTML = `<div class="error">Error loading status: ${err.message}</div>`;
        }
    }
}

function renderStatusList() {
    if (!statusmonContainer) {
        return;
    }

    let list = statusmonContainer.querySelector('#statusmon-list');
    if (!list) {
        statusmonContainer.innerHTML = '<div class="statusmon-layout"><div class="statusmon-content"><div id="statusmon-list"></div></div></div>';
        list = statusmonContainer.querySelector('#statusmon-list');
    }

    const statuses = Object.values(currentStatuses);

    if (statuses.length === 0) {
        list.innerHTML = '<div class="no-status">No status items</div>';
        return;
    }

    // Sort by name
    statuses.sort((a, b) => a.name.localeCompare(b.name));

    // Calculate alert counts
    const criticalCount = statuses.filter(s => (s.level || 'ok').toLowerCase() === 'critical').length;
    const warningCount = statuses.filter(s => (s.level || 'ok').toLowerCase() === 'warning').length;
    const alertCount = criticalCount + warningCount;

    let alertBubble = '';
    if (alertCount > 0) {
        const bubbleColor = criticalCount > 0 ? '#ef4444' : '#f59e0b'; // red for critical, yellow for warning
        alertBubble = `<div class="status-alert-bubble" style="background-color: ${bubbleColor}">${alertCount}</div>`;
    }

    // Parse name to extract hostname and service
    // Format: "hostname:service" or just "service"
    const rows = statuses.map(status => {
        const hostname = status.hostname || '';
        const level = (status.level || 'ok').toLowerCase();
        const color = LEVEL_COLORS[level] || LEVEL_COLORS['info'];
        const statusUpper = (status.status || '').toUpperCase();
        const lastSeenHtml = formatElapsedTime(status.lastSeen);

        return `
            <tr class="status-row" data-name="${status.name}">
                <td class="status-hostname">${hostname}</td>
                <td class="status-service">${status.name}</td>
                <td class="status-status">
                    <span class="status-badge" style="color: ${color}">${statusUpper}</span>
                </td>
                <td class="status-text">${status.details || ''}</td>
                <td class="status-lastseen">${lastSeenHtml}</td>
            </tr>
        `;
    }).join('');

    const html = `
        <table class="status-table">
            <thead>
                <tr>
                    <th>Hostname</th>
                    <th>Service</th>
                    <th>Status</th>
                    <th>Details</th>
                    <th>Last Seen</th>
                </tr>
            </thead>
            <tbody>
                ${rows}
            </tbody>
        </table>
    `;

    // Update the tab badge with alert count
    updateStatusTabBadge(criticalCount, warningCount);

    list.innerHTML = html;
}

function updateStatusTabBadge(criticalCount, warningCount) {
    const tabLink = document.querySelector('[data-tab-id="statusmon"]');
    if (!tabLink) return;

    // Remove existing badges if any
    const existingBadges = tabLink.querySelectorAll('.tab-badge');
    existingBadges.forEach(badge => badge.remove());

    // Add badges for critical and warning separately
    if (criticalCount > 0) {
        const criticalBadge = document.createElement('span');
        criticalBadge.className = 'tab-badge';
        criticalBadge.textContent = criticalCount;
        criticalBadge.setAttribute('data-badge-style', 'critical');
        tabLink.appendChild(criticalBadge);
    }

    if (warningCount > 0) {
        const warningBadge = document.createElement('span');
        warningBadge.className = 'tab-badge';
        warningBadge.textContent = warningCount;
        warningBadge.setAttribute('data-badge-style', 'warning');
        tabLink.appendChild(warningBadge);
    }
}
