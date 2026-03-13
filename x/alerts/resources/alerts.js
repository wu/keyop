import {ServiceFilterNav} from '/js/service-filter-nav.js';

let alertsContainer = null;
let unreadAlertCount = 0;
let highestSeverity = 'info';
let navController = null;

// Severity levels in order of priority
const SEVERITY_LEVELS = {info: 0, warning: 1, error: 2, critical: 3};
const SEVERITY_COLORS = {
    info: '#3b82f6',      // blue
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
        tabLink.appendChild(badge);
    }
}

export function onMessage(msg) {
    if (!alertsContainer) return;

    // Only process alert-type messages
    if (msg.dataType !== 'core.alert.v1') return;

    // Check if the alerts tab content is visible
    const tabContent = alertsContainer.closest('.tab-content');
    if (!tabContent || !tabContent.classList.contains('active')) {
        return;
    }

    // When a new alert message arrives and tab is active, add it to the list
    if (msg.data && (msg.data.summary || msg.data.text)) {
        addAlertToList(msg);
    }
}

function addAlertToList(msg) {
    let listDiv = alertsContainer.querySelector('#alerts-list');
    if (!listDiv) {
        alertsContainer.innerHTML = '<div class="alerts-layout"><div class="filter-sidebar"><div class="filter-title">Services</div><div class="service-list"><div class="service-item active" data-service="all">all</div></div></div><div class="alerts-content"><div id="alerts-list"></div></div></div>';
        listDiv = alertsContainer.querySelector('#alerts-list');
    }

    const noAlertsDiv = alertsContainer.querySelector('.no-alerts');
    if (noAlertsDiv) {
        noAlertsDiv.remove();
    }

    const alertData = msg.data;
    const severity = alertData.level || 'info';
    const timestamp = msg.timestamp ? new Date(msg.timestamp).toLocaleString() : new Date().toLocaleString();
    const summary = alertData.summary || msg.event || 'No summary';
    const text = alertData.text || '';
    const serviceName = msg.serviceName || 'Unknown';
    const serviceType = msg.serviceType || 'Unknown';

    updateHighestSeverity(severity);
    unreadAlertCount++;
    updateBubble();

    const alertHTML = `
        <div class="alert-item" data-alert-id="temp-${Date.now()}" data-service-name="${serviceName}" data-severity="${severity.toLowerCase()}">
            <div class="alert-checkbox">
                <input type="checkbox" class="alert-checkbox-input" />
            </div>
            <div class="alert-content">
                <div class="alert-header">
                    <span class="alert-severity alert-severity-${severity.toLowerCase()}">${severity.toUpperCase()}</span>
                    <span class="alert-timestamp">${timestamp}</span>
                </div>
                <div class="alert-service">
                    <strong>${serviceName}</strong> (${serviceType})
                </div>
                ${text ? `<div class="alert-text">${text}</div>` : ''}
            </div>
        </div>
    `;

    listDiv.insertAdjacentHTML('afterbegin', alertHTML);

    const newCheckbox = listDiv.querySelector('.alert-item:first-child .alert-checkbox-input');
    if (newCheckbox) {
        newCheckbox.addEventListener('change', async (e) => {
            if (e.target.checked) {
                const alertItem = e.target.closest('.alert-item');
                if (alertItem) {
                    alertItem.remove();
                }
                if (listDiv.children.length === 0) {
                    alertsContainer.innerHTML = '<div class="no-alerts">No active alerts</div>';
                }
            }
        });
    }
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

        const list = alertsContainer.querySelector('#alerts-list');
        if (!list) return;

        if (alerts.length === 0) {
            list.innerHTML = '<div class="no-alerts">No active alerts</div>';

            // Also clear the service list to only show "all"
            const sidebar = alertsContainer.querySelector('.filter-sidebar');
            if (sidebar) {
                sidebar.innerHTML = `
                    <div class="filter-title">Services</div>
                    <div class="service-list">
                        <div class="service-item active" data-service="all">all</div>
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
                <div class="service-item active" data-service="all">all</div>
                ${sortedServices.map(service => `
                    <div class="service-item" data-service="${service}">${service}</div>
                `).join('')}
            </div>
        `;
        sidebar.innerHTML = serviceFilterHTML;

        // Build alerts list
        const html = alerts.map(alert => `
            <div class="alert-item" data-alert-id="${alert.id}" data-service-name="${alert.serviceName}" data-severity="${alert.severity?.toLowerCase() || 'info'}">
                <div class="alert-checkbox">
                    <input type="checkbox" class="alert-checkbox-input" data-alert-id="${alert.id}" />
                </div>
                <div class="alert-content">
                    <div class="alert-header">
                        <span class="alert-severity alert-severity-${alert.severity?.toLowerCase() || 'info'}">${alert.severity || 'INFO'}</span>
                        <span class="alert-timestamp">${new Date(alert.timestamp).toLocaleString()}</span>
                    </div>
                    <div class="alert-service">
                        <strong>${alert.serviceName}</strong> (${alert.serviceType})
                    </div>
                    ${alert.text ? `<div class="alert-text">${alert.text}</div>` : ''}
                </div>
            </div>
        `).join('');

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

        function rebuildServiceList() {
            const sidebar = alertsContainer.querySelector('.filter-sidebar');
            if (!sidebar) return;

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

            // Build new service list
            const serviceListHTML = `
        <div class="filter-title">Services</div>
        <div class="service-list">
            <div class="service-item ${navController.selectedService === 'all' ? 'active' : ''}" data-service="all">all</div>
            ${sortedServices.map(service => `
                <div class="service-item ${navController.selectedService === service ? 'active' : ''}" data-service="${service}">${service}</div>
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

        alertsContainer.querySelectorAll('.service-item').forEach(item => {
            item.addEventListener('click', () => {
                alertsContainer.querySelectorAll('.service-item').forEach(i => i.classList.remove('active'));
                item.classList.add('active');

                navController.selectedService = item.dataset.service;
                navController.selectedIndex = -1;
                navController.applyServiceFilter();
            });
        });

        // Attach checkbox handlers
        alertsContainer.querySelectorAll('.alert-checkbox-input').forEach(checkbox => {
            checkbox.addEventListener('change', async (e) => {
                if (e.target.checked) {
                    const alertID = parseInt(e.target.dataset.alertId, 10);
                    await markAlertSeen(alertID);
                    const alertItem = document.querySelector(`[data-alert-id="${alertID}"]`);
                    if (alertItem) {
                        alertItem.remove();
                    }
                    const listDiv = alertsContainer.querySelector('#alerts-list');
                    if (listDiv && listDiv.children.length === 0) {
                        listDiv.innerHTML = '<div class="no-alerts">No active alerts</div>';
                    }
                }
            });
        });

        recalculateHighestSeverity();
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

function setupNavigation() {
    navController = new ServiceFilterNav({
        container: alertsContainer,
        itemSelector: '.alert-item',
        serviceSelector: '.service-item',
        selectedClass: 'alert-selected',
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
}
