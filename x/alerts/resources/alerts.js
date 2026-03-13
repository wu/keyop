let alertsContainer = null;

export async function init(container) {
    alertsContainer = container;
    await refreshAlerts();
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
    // Get or create alerts list
    let listDiv = alertsContainer.querySelector('#alerts-list');
    if (!listDiv) {
        alertsContainer.innerHTML = '<div id="alerts-list"></div>';
        listDiv = alertsContainer.querySelector('#alerts-list');
    }

    // Remove "no alerts" message if present
    const noAlertsDiv = alertsContainer.querySelector('.no-alerts');
    if (noAlertsDiv) {
        noAlertsDiv.remove();
    }

    // Extract alert data from message
    const alertData = msg.data;
    const severity = alertData.level || 'info';
    const timestamp = msg.timestamp ? new Date(msg.timestamp).toLocaleString() : new Date().toLocaleString();
    const summary = alertData.summary || msg.event || 'No summary';
    const text = alertData.text || '';
    const serviceName = msg.serviceName || 'Unknown';
    const serviceType = msg.serviceType || 'Unknown';

    // Create alert element (note: we don't have a server ID for new alerts, so we'll use a temporary one)
    const alertHTML = `
        <div class="alert-item" data-alert-id="temp-${Date.now()}">
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
                <div class="alert-summary">${summary}</div>
                ${text ? `<div class="alert-text">${text}</div>` : ''}
            </div>
        </div>
    `;

    // Add to top of list
    listDiv.insertAdjacentHTML('afterbegin', alertHTML);

    // Attach checkbox handler to the new alert
    const newCheckbox = listDiv.querySelector('.alert-item:first-child .alert-checkbox-input');
    if (newCheckbox) {
        newCheckbox.addEventListener('change', async (e) => {
            if (e.target.checked) {
                // Remove from UI
                const alertItem = e.target.closest('.alert-item');
                if (alertItem) {
                    alertItem.remove();
                }
                // If no more alerts, show "No active alerts"
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
            alertsContainer.innerHTML = `<div class="error">Error loading alerts: ${response.statusText}</div>`;
            return;
        }

        const result = await response.json();
        const alerts = result.alerts || [];

        if (alerts.length === 0) {
            alertsContainer.innerHTML = '<div class="no-alerts">No active alerts</div>';
            return;
        }

        const html = alerts.map(alert => `
            <div class="alert-item" data-alert-id="${alert.id}">
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
                    <div class="alert-summary">${alert.summary || alert.event || 'No summary'}</div>
                    ${alert.text ? `<div class="alert-text">${alert.text}</div>` : ''}
                </div>
            </div>
        `).join('');

        alertsContainer.innerHTML = `<div id="alerts-list">${html}</div>`;

        // Attach checkbox handlers
        alertsContainer.querySelectorAll('.alert-checkbox-input').forEach(checkbox => {
            checkbox.addEventListener('change', async (e) => {
                if (e.target.checked) {
                    const alertID = parseInt(e.target.dataset.alertId, 10);
                    await markAlertSeen(alertID);
                    // Remove from UI
                    const alertItem = document.querySelector(`[data-alert-id="${alertID}"]`);
                    if (alertItem) {
                        alertItem.remove();
                    }
                    // If no more alerts, show "No active alerts"
                    const listDiv = alertsContainer.querySelector('#alerts-list');
                    if (listDiv && listDiv.children.length === 0) {
                        alertsContainer.innerHTML = '<div class="no-alerts">No active alerts</div>';
                    }
                }
            });
        });
    } catch (err) {
        console.error('Failed to refresh alerts:', err);
        alertsContainer.innerHTML = `<div class="error">Error loading alerts: ${err.message}</div>`;
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
        }
    } catch (err) {
        console.error('Failed to mark alert as seen:', err);
    }
}
