let statusmonContainer = null;
let currentStatuses = {}; // Cache of current status items keyed by name
let lastRefreshTime = 0; // Prevent too-frequent refreshes

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

        const oldStatus = currentStatuses[statusData.name];

        // Always refresh if we haven't recently to ensure acknowledged state is correct
        const now = Date.now();
        const timeSinceLastRefresh = now - lastRefreshTime;

        // If status/level hasn't changed AND we've refreshed recently, preserve the acknowledged state
        // Otherwise, refresh from server to get the correct acknowledged state
        if ((oldStatus?.status === statusData.status &&
                oldStatus?.level === statusData.level) &&
            timeSinceLastRefresh < 2000) {
            // Status unchanged and recent refresh - preserve acknowledged state
            currentStatuses[statusData.name] = {
                name: statusData.name,
                status: statusData.status,
                details: statusData.details,
                level: statusData.level,
                hostname: statusData.hostname,
                acknowledged: oldStatus.acknowledged || false,
                lastSeen: new Date().toISOString()
            };
            renderStatusList();
        } else {
            // Status changed or refresh is old - get the correct state from server
            refreshStatus();
        }
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

        lastRefreshTime = Date.now();
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

    // Calculate alert counts (excluding acknowledged problems)
    const unackedStatuses = statuses.filter(s => !s.acknowledged);
    const criticalCount = unackedStatuses.filter(s => (s.level || 'ok').toLowerCase() === 'critical').length;
    const warningCount = unackedStatuses.filter(s => (s.level || 'ok').toLowerCase() === 'warning').length;

    // Parse name to extract hostname and service
    // Format: "hostname:service" or just "service"
    const rows = statuses.map(status => {
        const hostname = status.hostname || '';
        const level = (status.level || 'ok').toLowerCase();
        const color = LEVEL_COLORS[level] || LEVEL_COLORS['info'];
        const statusUpper = (status.status || '').toUpperCase();
        const lastSeenHtml = formatElapsedTime(status.lastSeen);

        // Show ack button for warning/critical problems
        const isProblem = level === 'warning' || level === 'critical';
        let ackButton = '';
        if (isProblem) {
            if (status.acknowledged) {
                ackButton = `<button class="ack-button unacked-btn" data-name="${status.name}" title="Un-acknowledge">✓ Acked</button>`;
            } else {
                ackButton = `<button class="ack-button ack-btn" data-name="${status.name}" title="Acknowledge">Ack</button>`;
            }
        }

        // Add dimmed class if acknowledged
        const rowClass = status.acknowledged ? 'status-row acknowledged' : 'status-row';

        return `
            <tr class="${rowClass}" data-name="${status.name}">
                <td class="status-hostname">${hostname}</td>
                <td class="status-service">${status.name}</td>
                <td class="status-status">
                    <span class="status-badge" style="color: ${color}">${statusUpper}</span>
                </td>
                <td class="status-text">${status.details || ''}</td>
                <td class="status-lastseen">${lastSeenHtml}</td>
                <td class="status-ack">${ackButton}</td>
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
                    <th>Action</th>
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

    // Attach click handlers to ack buttons
    attachAckHandlers();
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

function attachAckHandlers() {
    const ackButtons = document.querySelectorAll('.ack-button');
    ackButtons.forEach(btn => {
        btn.addEventListener('click', handleAckClick);
    });
}

async function handleAckClick(event) {
    event.stopPropagation();
    const btn = event.target;
    const statusName = btn.dataset.name;
    const isCurrentlyAcked = btn.classList.contains('unacked-btn');

    console.log('[statusmon] Ack click:', {
        statusName,
        isCurrentlyAcked,
        action: isCurrentlyAcked ? 'unacknowledge' : 'acknowledge'
    });

    try {
        const action = isCurrentlyAcked ? 'unacknowledge-status' : 'acknowledge-status';
        const payload = {statusName};
        const url = '/api/tabs/statusmon/action/' + action;
        console.log('[statusmon] Sending request:', {action, url, payload});

        const response = await fetch(url, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(payload)
        });

        console.log('[statusmon] Response status:', response.status, response.ok);
        console.log('[statusmon] Response headers:', response.headers.get('content-type'));

        if (response.ok) {
            const result = await response.json();
            console.log('[statusmon] Response result:', result);
            // Refresh from server to ensure we have the correct state
            await refreshStatus();
        } else {
            const text = await response.text();
            console.error('[statusmon] API error:', response.status, text);
        }
    } catch (err) {
        console.error('Failed to toggle acknowledgement:', err);
    }
}
