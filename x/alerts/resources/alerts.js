import {ServiceFilterNav} from '/js/service-filter-nav.js';
import {formatElapsedTime, startElapsedTimeUpdates} from '/js/time-formatter.js';

let alertsContainer = null;
let unreadAlertCount = 0;
let highestSeverity = 'info';
let navController = null;
let serviceCounts = {}; // Track alert counts per service

// Severity levels in order of priority
const SEVERITY_LEVELS = {info: 0, warning: 1, error: 2, critical: 3};
const SEVERITY_COLORS = {
    info: '#8a3fd3',      // theme purple
    warning: '#fbbf24',   // yellow
    error: '#ef4444',     // red
    critical: '#dc2626'   // darker red
};

function updateHighestSeverity(severity) {
    const currentLevel = SEVERITY_LEVELS[highestSeverity] || 0;
    const newLevel = SEVERITY_LEVELS[severity?.toLowerCase()] || 0;
    if (newLevel > currentLevel) {
        highestSeverity = severity?.toLowerCase() || 'info';
    }
}

function recalculateHighestSeverity() {
    highestSeverity = 'info';
    const alertItems = document.querySelectorAll('.alert-item');
    alertItems.forEach(item => {
        const severity = item.dataset.severity?.toLowerCase() || 'info';
        updateHighestSeverity(severity);
    });
}

export async function init(container) {
    alertsContainer = container;
    await refreshAlerts();
    setupNavigation();
    setupMarkAllSeenButton();
    startElapsedTimeUpdates();
}

export function focusItems() {
    // Focus on the items list in the alerts
    if (navController) {
        navController.focusOnItems();
    }
}

export function canReturnToTabs() {
    // Return to tabs only if we're at the top of the items
    return navController && navController.canReturnFocus();
}

export function updateBubble() {
    // Find the tab link for alerts
    const tabLink = document.querySelector('[data-tab-id="alerts"]');
    if (!tabLink) return;

    // Remove existing badge if any
    const existingBadge = tabLink.querySelector('.tab-badge');
    if (existingBadge) {
        existingBadge.remove();
    }

    // Add new badge if there are unread alerts
    if (unreadAlertCount > 0) {
        const badge = document.createElement('span');
        badge.className = 'tab-badge';
        badge.textContent = unreadAlertCount;

        // Colorize badge based on highest severity
        const color = SEVERITY_COLORS[highestSeverity] || SEVERITY_COLORS.info;
        badge.style.backgroundColor = color;
        
        tabLink.appendChild(badge);
    }
}

export function onMessage(msg) {
    if (!alertsContainer) return;

    // Filter for alert-related messages: check channelName, event, or dataType
    if (msg.channelName !== 'alerts' && !msg.event?.includes('alert') && msg.dataType !== 'core.alert.v1' && msg.dataType !== 'alert') {
        return;
    }

    // Check if the alerts tab content is visible
    const tabContent = alertsContainer.closest('.tab-content');
    const isTabActive = tabContent && tabContent.classList.contains('active');

    // When a new alert message arrives, process it
    if (msg.text || msg.summary || msg.data) {
        // Always add the alert to the list (whether tab is active or not)
        // This ensures the alert is present when the user switches to the alerts tab
        addAlertToList(msg);

        // Update unread count only if tab isn't active
        if (!isTabActive) {
            unreadAlertCount++;
            updateBubble();
        }
    }
}

function escapeHtml(text) {
    if (text === null || text === undefined) return '';
    return String(text)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}

function addAlertToList(msg) {
    let listDiv = alertsContainer.querySelector('#alerts-list');
    if (!listDiv) {
        alertsContainer.innerHTML = '<div class="alerts-layout"><div class="filter-sidebar"><div class="filter-title">Services</div><div class="service-list"><div class="service-item tag-item active" data-service="all"><span class="tag-label">all</span><span class="service-count">0</span></div></div></div><div class="alerts-content"><div id="alerts-list"></div></div></div>';
        listDiv = alertsContainer.querySelector('#alerts-list');
    }

    const noAlertsDiv = alertsContainer.querySelector('.no-alerts');
    if (noAlertsDiv) {
        noAlertsDiv.remove();
    }

    const alertData = msg.data || {};
    const severity = (alertData.level || 'info').toLowerCase();
    let timeClass = '';
    const timestampHtml = formatElapsedTime(msg.timestamp);
    if (msg.timestamp) {
        const ts = new Date(msg.timestamp);
        timeClass = (Date.now() - ts.getTime() >= 0) ? 'past' : 'future';
    }
    const text = alertData.text || msg.summary || 'No details';
    const serviceName = msg.serviceName || 'Unknown';
    const serviceType = msg.serviceType || '';

    updateHighestSeverity(severity);
    unreadAlertCount++;
    updateBubble();

    // Build tag labels (avoid duplicate if serviceName === serviceType)
    const safeService = escapeHtml(serviceName);
    const safeType = escapeHtml(serviceType);
    const tagHtml = (serviceType && serviceType === serviceName) ?
        `<span class="tag-label">${safeService}</span>` :
        `<span class="tag-label">${safeService}</span>${serviceType ? `<span class="tag-label">${safeType}</span>` : ''}`;

    const alertHTML = `
        <div class="alert-item task-item" data-alert-id="temp-${Date.now()}" data-service-name="${escapeHtml(serviceName)}" data-severity="${escapeHtml(severity)}">
            <div class="task-checkbox" data-alert-id="temp-${Date.now()}"></div>
            <div class="task-content">
                <div class="task-title-row">
                    <div class="task-title"><span class="task-title-text">${escapeHtml(text)}</span></div>
                </div>
                <div class="task-metadata alert-meta">
                    <div class="task-metadata-primary">
                        <span class="task-time ${timeClass}">${timestampHtml}</span>
                    </div>
                    <div class="task-tags">
                        ${tagHtml}
                    </div>
                </div>
            </div>
        </div>
    `;

    listDiv.insertAdjacentHTML('afterbegin', alertHTML);

    // Attach handler to the checkbox area of the newly added temp alert (no server id)
    const newItem = listDiv.querySelector('.alert-item:first-child');
    if (newItem) {
        const cb = newItem.querySelector('.task-checkbox');
        if (cb) {
            cb.addEventListener('click', (e) => {
                const alertItem = e.target.closest('.alert-item');
                if (alertItem) {
                    alertItem.remove();
                    unreadAlertCount = Math.max(0, unreadAlertCount - 1);
                    recalculateHighestSeverity();
                    updateBubble();
                }
                if (listDiv.children.length === 0) {
                    listDiv.innerHTML = '<div class="no-alerts">No active alerts</div>';
                    rebuildServiceList();
                }
            });
        }
    }

    // Update service list if a new service has appeared
    rebuildServiceList();
}

async function refreshAlerts() {
    if (!alertsContainer) return;

    try {
        const response = await fetch('/api/tabs/alerts/action/fetch-alerts', {
            method: 'POST',
        });

        if (!response.ok) {
            const list = alertsContainer.querySelector('#alerts-list');
            if (list) {
                list.innerHTML = `<div class="error">Error loading alerts: ${response.statusText}</div>`;
            }
            return;
        }

        const result = await response.json();
        const alerts = result.alerts || [];
        serviceCounts = result.serviceCounts || {}; // Store service counts

        let list = alertsContainer.querySelector('#alerts-list');

        // If the layout doesn't exist yet, create it
        if (!list) {
            alertsContainer.innerHTML = '<div class="alerts-layout"><div class="filter-sidebar"><div class="filter-title">Services</div><div class="service-list"><div class="service-item tag-item active" data-service="all"><span class="tag-label">all</span><span class="service-count">0</span></div></div></div><div class="alerts-content"><div class="alerts-header"><button id="mark-all-seen-btn" class="mark-all-seen-btn">Mark All Seen</button></div><div id="alerts-list"></div></div></div>';
            list = alertsContainer.querySelector('#alerts-list');
            setupMarkAllSeenButton(); // Re-setup button when layout is created
        }

        if (alerts.length === 0) {
            list.innerHTML = '<div class="no-alerts">No active alerts</div>';

            // Also ensure service list is visible with only "all"
            const sidebar = alertsContainer.querySelector('.filter-sidebar');
            if (sidebar) {
                sidebar.innerHTML = `
                    <div class="filter-title">Services</div>
                    <div class="service-list">
                        <div class="service-item tag-item active" data-service="all"><span class="tag-label">all</span><span class="service-count">0</span></div>
                    </div>
                `;
            }
            
            unreadAlertCount = 0;
            updateBubble();
            return;
        }

        unreadAlertCount = alerts.length;
        updateBubble();

        // Get unique service names
        const serviceNames = new Set(alerts.map(alert => alert.serviceName));
        const sortedServices = Array.from(serviceNames).sort();

        // Get or create service filter sidebar
        let sidebar = alertsContainer.querySelector('.filter-sidebar');
        if (!sidebar) {
            sidebar = document.createElement('div');
            sidebar.className = 'filter-sidebar';
            alertsContainer.insertBefore(sidebar, list.parentElement);
        }

        const serviceFilterHTML = `
            <div class="filter-title">Services</div>
            <div class="service-list">
                <div class="service-item tag-item active" data-service="all"><span class="tag-label">all</span><span class="service-count">${alerts.length}</span></div>
                ${sortedServices.map(service => `
                    <div class="service-item tag-item" data-service="${service}"><span class="tag-label">${service}</span><span class="service-count">${serviceCounts[service] || 0}</span></div>
                `).join('')}
            </div>
        `;
        sidebar.innerHTML = serviceFilterHTML;

        // Build alerts list
        const html = alerts.map(alert => {
            const text = alert.text ? alert.text : (alert.summary || 'No details');
            const sName = alert.serviceName || 'Unknown';
            const sType = alert.serviceType || '';
            const tagHtml = (sType && sType === sName) ?
                `<span class="tag-label">${escapeHtml(sName)}</span>` :
                `<span class="tag-label">${escapeHtml(sName)}</span>${sType ? `<span class="tag-label">${escapeHtml(sType)}</span>` : ''}`;

            return `
            <div class="alert-item task-item" data-alert-id="${alert.id}" data-service-name="${escapeHtml(sName)}" data-severity="${escapeHtml((alert.severity || 'info').toLowerCase())}">
                <div class="task-checkbox" data-alert-id="${alert.id}"></div>
                <div class="task-content">
                    <div class="task-title-row">
                        <div class="task-title"><span class="task-title-text">${escapeHtml(text)}</span></div>
                    </div>
                    <div class="task-metadata alert-meta">
                        <div class="task-metadata-primary">
                            <span class="task-time ${alert.timestamp && (Date.now() - new Date(alert.timestamp).getTime() >= 0) ? 'past' : 'future'}">${formatElapsedTime(alert.timestamp)}</span>
                        </div>
                        <div class="task-tags">
                            ${tagHtml}
                        </div>
                    </div>
                </div>
            </div>
        `;
        }).join('');

        list.innerHTML = html;

        // Wrap in layout if needed
        if (!list.parentElement.classList.contains('alerts-layout')) {
            const layout = document.createElement('div');
            layout.className = 'alerts-layout';
            const content = document.createElement('div');
            content.className = 'alerts-content';

            const parent = list.parentElement;
            parent.insertBefore(layout, list);
            layout.appendChild(sidebar || document.createElement('div'));
            layout.appendChild(content);
            content.appendChild(list);
        }

        // Attach checkbox handlers
        alertsContainer.querySelectorAll('.task-checkbox').forEach(cb => {
            cb.addEventListener('click', async (e) => {
                const id = parseInt(cb.dataset.alertId, 10);
                if (!Number.isFinite(id)) return;
                await markAlertSeen(id);
                const alertItem = document.querySelector(`[data-alert-id="${id}"]`);
                if (alertItem) alertItem.remove();
                const listDiv = alertsContainer.querySelector('#alerts-list');
                if (listDiv && listDiv.children.length === 0) {
                    listDiv.innerHTML = '<div class="no-alerts">No active alerts</div>';
                    // Rebuild service list to only show "all"
                    rebuildServiceList();
                    recalculateHighestSeverity();
                    updateBubble();
                }
            });
        });

        recalculateHighestSeverity();
        updateBubble();

        // Rebuild service list to include counts
        rebuildServiceList();
    } catch (err) {
        console.error('Failed to refresh alerts:', err);
        const list = alertsContainer.querySelector('#alerts-list');
        if (list) {
            list.innerHTML = `<div class="error">Error loading alerts: ${err.message}</div>`;
        }
    }
}

async function markAlertSeen(alertID) {
    try {
        const response = await fetch('/api/tabs/alerts/action/mark-seen', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({alertID}),
        });

        if (!response.ok) {
            console.error('Failed to mark alert as seen:', response.statusText);
        } else {
            unreadAlertCount = Math.max(0, unreadAlertCount - 1);
            updateBubble();
        }
    } catch (err) {
        console.error('Failed to mark alert as seen:', err);
    }
}

function rebuildServiceList() {
    const sidebar = alertsContainer.querySelector('.filter-sidebar');
    if (!sidebar || !navController) return;

    // Get all unique services that still have items visible (not filtered out)
    const items = document.querySelectorAll('.alert-item');
    const serviceNames = new Set();
    items.forEach(item => {
        // Only include services that would be visible if filtering was applied
        // (i.e., not hidden by a different service filter)
        const isVisible = item.style.display !== 'none';
        if (isVisible) {
            const service = item.dataset.serviceName;
            if (service) serviceNames.add(service);
        }
    });

    const sortedServices = Array.from(serviceNames).sort();

    // If the currently selected service no longer has any items, fall back to 'all'
    if (navController.selectedService !== 'all' && !sortedServices.includes(navController.selectedService)) {
        navController.selectedService = 'all';
        navController.selectedServiceIndex = 0;
        navController.selectedIndex = -1;
        // Show all items since we're resetting to 'all'
        alertsContainer.querySelectorAll('.alert-item').forEach(item => {
            item.style.display = '';
        });
    }

    // Recalculate counts based on actual visible items in the DOM
    const countsFromDOM = {};
    items.forEach(item => {
        const isVisible = item.style.display !== 'none';
        if (isVisible) {
            const service = item.dataset.serviceName;
            if (service) {
                countsFromDOM[service] = (countsFromDOM[service] || 0) + 1;
            }
        }
    });

    // Calculate total count from visible items
    const totalCount = Object.values(countsFromDOM).reduce((a, b) => a + b, 0);

    // Build new service list with counts
    const serviceListHTML = `
        <div class="filter-title">Services</div>
        <div class="service-list">
            <div class="service-item tag-item ${navController.selectedService === 'all' ? 'active' : ''}" data-service="all"><span class="tag-label">all</span><span class="service-count">${totalCount}</span></div>
            ${sortedServices.map(service => `
                <div class="service-item tag-item ${navController.selectedService === service ? 'active' : ''}" data-service="${service}">
                    <span class="tag-label">${service}</span> <span class="service-count">${countsFromDOM[service] || 0}</span>
                </div>
            `).join('')}
        </div>
    `;

    sidebar.innerHTML = serviceListHTML;

    // Update the service index based on new list
    const services = alertsContainer.querySelectorAll('.service-item');
    navController.selectedServiceIndex = 0;
    services.forEach((item, index) => {
        if (item.dataset.service === navController.selectedService) {
            navController.selectedServiceIndex = index;
        }
    });

    // Re-attach service filter handlers
    alertsContainer.querySelectorAll('.service-item').forEach(item => {
        item.addEventListener('click', () => {
            alertsContainer.querySelectorAll('.service-item').forEach(i => i.classList.remove('active'));
            item.classList.add('active');

            navController.selectedService = item.dataset.service;
            navController.selectedIndex = -1;
            navController.applyServiceFilter();
        });
    });
}

function setupNavigation() {
    navController = new ServiceFilterNav({
        container: alertsContainer,
        itemSelector: '.alert-item',
        serviceSelector: '.service-item',
        selectedClass: 'task-selected',
        markedClass: 'alert-marked',
        markItemCallback: async (item) => {
            const alertID = parseInt(item.dataset.alertId, 10);
            await markAlertSeen(alertID);
            item.remove();
            const listDiv = alertsContainer.querySelector('#alerts-list');

            // Check if there are any visible items left
            const visibleItems = navController.getVisibleItems();

            if (listDiv && listDiv.children.length === 0) {
                listDiv.innerHTML = '<div class="no-alerts">No active alerts</div>';
                navController.selectedIndex = -1;

                // When all alerts are gone, refresh from server to ensure service list is clean
                await refreshAlerts();
            } else if (visibleItems.length === 0) {
                // No visible items (service was filtered and now has no items)
                // Refresh from server to get the current state and rebuild the service list
                await refreshAlerts();

                // Ensure all items are visible before resetting nav state
                alertsContainer.querySelectorAll('.alert-item').forEach(item => {
                    item.style.display = '';
                });

                // Reset navigation state (don't recreate nav controller to avoid duplicate event listeners)
                navController.selectedService = 'all';
                navController.selectedServiceIndex = 0;
                navController.selectedIndex = -1;
                navController.focusOnServices = false;
                navController.applyServiceFilter();

                // Double-check that all items are now visible
                alertsContainer.querySelectorAll('.alert-item').forEach(item => {
                    item.style.display = '';
                });

                // Update service list UI
                alertsContainer.querySelectorAll('.service-item').forEach(s => s.classList.remove('active'));
                const allService = alertsContainer.querySelector('[data-service="all"]');
                if (allService) allService.classList.add('active');

                // Select first item from "all" services
                const newVisibleItems = navController.getVisibleItems();
                if (newVisibleItems.length > 0) {
                    navController.selectItem(0);
                }
                 // Exit early, don't call rebuildServiceList()
            } else {
                // Reselect at same index (which is now the next item)
                if (navController.selectedIndex >= visibleItems.length) {
                    // If we were at the end, move back one
                    navController.selectItem(visibleItems.length - 1);
                } else if (navController.selectedIndex >= 0) {
                    // Reselect at same index
                    navController.selectItem(navController.selectedIndex);
                }
                // Rebuild service list to remove services with no items
                rebuildServiceList();
            }
        },
        onStateChange: (state) => {
            // Can be used for debugging or updating UI
        }
    });
    rebuildServiceList();
}

function setupMarkAllSeenButton() {
    // Use a timeout to ensure the button is fully rendered
    setTimeout(() => {
        const markAllBtn = document.querySelector('#mark-all-seen-btn');
        if (!markAllBtn) {
            console.warn('[alerts] Mark all seen button not found');
            return;
        }

        markAllBtn.addEventListener('click', async () => {
            // Get the currently selected service filter
            const currentService = navController?.selectedService || 'all';

            try {
                const response = await fetch('/api/tabs/alerts/action/mark-all-seen', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({serviceFilter: currentService})
                });

                if (!response.ok) {
                    console.error('[alerts] Mark all seen failed:', response.status, response.statusText);
                    return;
                }

                // Refresh alerts to update the display
                await refreshAlerts();
            } catch (err) {
                console.error('[alerts] Failed to mark all alerts as seen:', err);
            }
        });
    }, 100);
}
