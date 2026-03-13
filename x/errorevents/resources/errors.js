import {ServiceFilterNav} from '/js/service-filter-nav.js';

let errorsContainer = null;
let unreadErrorCount = 0;
let navController = null;

export async function init(container) {
    errorsContainer = container;
    await refreshErrors();
    setupNavigation();
}

export function focusItems() {
    // Focus on the items list in the errors
    if (navController) {
        navController.focusOnItems();
    }
}

export function canReturnToTabs() {
    // Return to tabs only if we're at the top of the items
    return navController && navController.canReturnFocus();
}

export function updateBubble() {
    // Find the tab link for errors
    const tabLink = document.querySelector('[data-tab-id="errors"]');
    if (!tabLink) return;

    // Remove existing badge if any
    const existingBadge = tabLink.querySelector('.tab-badge');
    if (existingBadge) {
        existingBadge.remove();
    }

    // Add new badge if there are unread errors
    if (unreadErrorCount > 0) {
        const badge = document.createElement('span');
        badge.className = 'tab-badge';
        badge.textContent = unreadErrorCount;
        tabLink.appendChild(badge);
    }
}

export function onMessage(msg) {
    if (!errorsContainer) return;

    // Only process error-type messages
    if (msg.dataType !== 'core.error.v1') return;

    // Check if the errors tab content is visible
    const tabContent = errorsContainer.closest('.tab-content');
    if (!tabContent || !tabContent.classList.contains('active')) {
        return;
    }

    // When a new error message arrives and tab is active, add it to the list
    if (msg.data && (msg.data.summary || msg.data.text)) {
        addErrorToList(msg);
    }
}

function addErrorToList(msg) {
    let listDiv = errorsContainer.querySelector('#errors-list');
    if (!listDiv) {
        errorsContainer.innerHTML = '<div class="errors-layout"><div class="filter-sidebar"><div class="filter-title">Services</div><div class="service-list"><div class="service-item active" data-service="all">all</div></div></div><div class="errors-content"><div id="errors-list"></div></div></div>';
        listDiv = errorsContainer.querySelector('#errors-list');
    }

    const noErrorsDiv = errorsContainer.querySelector('.no-errors');
    if (noErrorsDiv) {
        noErrorsDiv.remove();
    }

    const errorData = msg.data;
    const severity = errorData.level || 'info';
    const timestamp = msg.timestamp ? new Date(msg.timestamp).toLocaleString() : new Date().toLocaleString();
    const summary = errorData.summary || msg.event || 'No summary';
    const text = errorData.text || '';
    const serviceName = msg.serviceName || 'Unknown';
    const serviceType = msg.serviceType || 'Unknown';

    const errorHTML = `
        <div class="error-item" data-error-id="temp-${Date.now()}" data-service-name="${serviceName}">
            <div class="error-checkbox">
                <input type="checkbox" class="error-checkbox-input" />
            </div>
            <div class="error-content">
                <div class="error-header">
                    <span class="error-severity error-severity-${severity.toLowerCase()}">${severity.toUpperCase()}</span>
                    <span class="error-timestamp">${timestamp}</span>
                </div>
                <div class="error-service">
                    <strong>${serviceName}</strong> (${serviceType})
                </div>
                ${text ? `<div class="error-text">${text}</div>` : ''}
            </div>
        </div>
    `;

    unreadErrorCount++;
    updateBubble();

    listDiv.insertAdjacentHTML('afterbegin', errorHTML);

    const newCheckbox = listDiv.querySelector('.error-item:first-child .error-checkbox-input');
    if (newCheckbox) {
        newCheckbox.addEventListener('change', async (e) => {
            if (e.target.checked) {
                const errorItem = e.target.closest('.error-item');
                if (errorItem) {
                    errorItem.remove();
                }
                if (listDiv.children.length === 0) {
                    errorsContainer.innerHTML = '<div class="no-errors">No active errors</div>';
                }
            }
        });
    }
}

async function refreshErrors() {
    if (!errorsContainer) return;

    try {
        const response = await fetch('/api/tabs/errors/action/fetch-errors', {
            method: 'POST',
        });

        if (!response.ok) {
            const list = errorsContainer.querySelector('#errors-list');
            if (list) {
                list.innerHTML = `<div class="error">Error loading errors: ${response.statusText}</div>`;
            }
            return;
        }

        const result = await response.json();
        const errors = result.errors || [];

        const list = errorsContainer.querySelector('#errors-list');
        if (!list) return;

        if (errors.length === 0) {
            list.innerHTML = '<div class="no-errors">No active errors</div>';

            // Also clear the service list to only show "all"
            const sidebar = errorsContainer.querySelector('.filter-sidebar');
            if (sidebar) {
                sidebar.innerHTML = `
                    <div class="filter-title">Services</div>
                    <div class="service-list">
                        <div class="service-item active" data-service="all">all</div>
                    </div>
                `;
            }
            
            unreadErrorCount = 0;
            updateBubble();
            return;
        }

        unreadErrorCount = errors.length;
        updateBubble();

        // Get unique service names
        const serviceNames = new Set(errors.map(error => error.serviceName));
        const sortedServices = Array.from(serviceNames).sort();

        // Get or create service filter sidebar
        let sidebar = errorsContainer.querySelector('.filter-sidebar');
        if (!sidebar) {
            sidebar = document.createElement('div');
            sidebar.className = 'filter-sidebar';
            errorsContainer.insertBefore(sidebar, list.parentElement);
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

        // Build errors list
        const html = errors.map(error => `
            <div class="error-item" data-error-id="${error.id}" data-service-name="${error.serviceName}">
                <div class="error-checkbox">
                    <input type="checkbox" class="error-checkbox-input" data-error-id="${error.id}" />
                </div>
                <div class="error-content">
                    <div class="error-header">
                        <span class="error-severity error-severity-${error.severity?.toLowerCase() || 'info'}">${error.severity || 'INFO'}</span>
                        <span class="error-timestamp">${new Date(error.timestamp).toLocaleString()}</span>
                    </div>
                    <div class="error-service">
                        <strong>${error.serviceName}</strong> (${error.serviceType})
                    </div>
                    ${error.text ? `<div class="error-text">${error.text}</div>` : ''}
                </div>
            </div>
        `).join('');

        list.innerHTML = html;

        // Wrap in layout if needed
        if (!list.parentElement.classList.contains('errors-layout')) {
            const layout = document.createElement('div');
            layout.className = 'errors-layout';
            const content = document.createElement('div');
            content.className = 'errors-content';

            const parent = list.parentElement;
            parent.insertBefore(layout, list);
            layout.appendChild(sidebar || document.createElement('div'));
            layout.appendChild(content);
            content.appendChild(list);
        }

        function rebuildServiceList() {
            const sidebar = errorsContainer.querySelector('.filter-sidebar');
            if (!sidebar) return;

            // Get all unique services that still have items visible (not filtered out)
            const items = document.querySelectorAll('.error-item');
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
            const services = errorsContainer.querySelectorAll('.service-item');
            navController.selectedServiceIndex = 0;
            services.forEach((item, index) => {
                if (item.dataset.service === navController.selectedService) {
                    navController.selectedServiceIndex = index;
                }
            });

            // Re-attach service filter handlers
            errorsContainer.querySelectorAll('.service-item').forEach(item => {
                item.addEventListener('click', () => {
                    errorsContainer.querySelectorAll('.service-item').forEach(i => i.classList.remove('active'));
                    item.classList.add('active');

                    navController.selectedService = item.dataset.service;
                    navController.selectedIndex = -1;
                    navController.applyServiceFilter();
                });
            });
        }

        // Attach service filter handlers for mouse clicks
        errorsContainer.querySelectorAll('.service-item').forEach(item => {
            item.addEventListener('click', () => {
                errorsContainer.querySelectorAll('.service-item').forEach(i => i.classList.remove('active'));
                item.classList.add('active');

                navController.selectedService = item.dataset.service;
                navController.selectedIndex = -1;
                navController.applyServiceFilter();
            });
        });

        // Attach checkbox handlers
        errorsContainer.querySelectorAll('.error-checkbox-input').forEach(checkbox => {
            checkbox.addEventListener('change', async (e) => {
                if (e.target.checked) {
                    const errorID = parseInt(e.target.dataset.errorId, 10);
                    await markErrorSeen(errorID);
                    const errorItem = document.querySelector(`[data-error-id="${errorID}"]`);
                    if (errorItem) {
                        errorItem.remove();
                    }
                    const listDiv = errorsContainer.querySelector('#errors-list');
                    if (listDiv && listDiv.children.length === 0) {
                        listDiv.innerHTML = '<div class="no-errors">No active errors</div>';
                    }
                }
            });
        });
    } catch (err) {
        console.error('Failed to refresh errors:', err);
        const list = errorsContainer.querySelector('#errors-list');
        if (list) {
            list.innerHTML = `<div class="error">Error loading errors: ${err.message}</div>`;
        }
    }
}

async function markErrorSeen(errorID) {
    try {
        const response = await fetch('/api/tabs/errors/action/mark-seen', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify({errorID}),
        });

        if (!response.ok) {
            console.error('Failed to mark error as seen:', response.statusText);
        } else {
            unreadErrorCount = Math.max(0, unreadErrorCount - 1);
            updateBubble();
        }
    } catch (err) {
        console.error('Failed to mark error as seen:', err);
    }
}

function setupNavigation() {
    navController = new ServiceFilterNav({
        container: errorsContainer,
        itemSelector: '.error-item',
        serviceSelector: '.service-item',
        selectedClass: 'error-selected',
        markedClass: 'error-marked',
        markItemCallback: async (item) => {
            const errorID = parseInt(item.dataset.errorId, 10);
            await markErrorSeen(errorID);
            item.remove();
            const listDiv = errorsContainer.querySelector('#errors-list');

            // Check if there are any visible items left
            const visibleItems = navController.getVisibleItems();

            if (listDiv && listDiv.children.length === 0) {
                listDiv.innerHTML = '<div class="no-errors">No active errors</div>';
                navController.selectedIndex = -1;

                // When all errors are gone, refresh from server to ensure service list is clean
                await refreshErrors();
            } else if (visibleItems.length === 0) {
                // No visible items (service was filtered and now has no items)
                // Refresh from server to get the current state and rebuild the service list
                await refreshErrors();

                // Ensure all items are visible before resetting nav state
                errorsContainer.querySelectorAll('.error-item').forEach(item => {
                    item.style.display = '';
                });

                // Reset navigation state (don't recreate nav controller to avoid duplicate event listeners)
                navController.selectedService = 'all';
                navController.selectedServiceIndex = 0;
                navController.selectedIndex = -1;
                navController.focusOnServices = false;
                navController.applyServiceFilter();

                // Double-check that all items are now visible
                errorsContainer.querySelectorAll('.error-item').forEach(item => {
                    item.style.display = '';
                });

                // Update service list UI
                errorsContainer.querySelectorAll('.service-item').forEach(s => s.classList.remove('active'));
                const allService = errorsContainer.querySelector('[data-service="all"]');
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
