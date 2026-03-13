let errorsContainer = null;

export async function init(container) {
    errorsContainer = container;
    await refreshErrors();
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
    // Get or create errors list
    let listDiv = errorsContainer.querySelector('#errors-list');
    if (!listDiv) {
        errorsContainer.innerHTML = '<div id="errors-list"></div>';
        listDiv = errorsContainer.querySelector('#errors-list');
    }

    // Remove "no errors" message if present
    const noErrorsDiv = errorsContainer.querySelector('.no-errors');
    if (noErrorsDiv) {
        noErrorsDiv.remove();
    }

    // Extract error data from message
    const errorData = msg.data;
    const severity = errorData.level || 'info';
    const timestamp = msg.timestamp ? new Date(msg.timestamp).toLocaleString() : new Date().toLocaleString();
    const summary = errorData.summary || msg.event || 'No summary';
    const text = errorData.text || '';
    const serviceName = msg.serviceName || 'Unknown';
    const serviceType = msg.serviceType || 'Unknown';

    // Create error element
    const errorHTML = `
        <div class="error-item" data-error-id="temp-${Date.now()}">
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
                <div class="error-summary">${summary}</div>
                ${text ? `<div class="error-text">${text}</div>` : ''}
            </div>
        </div>
    `;

    // Add to top of list
    listDiv.insertAdjacentHTML('afterbegin', errorHTML);

    // Attach checkbox handler to the new error
    const newCheckbox = listDiv.querySelector('.error-item:first-child .error-checkbox-input');
    if (newCheckbox) {
        newCheckbox.addEventListener('change', async (e) => {
            if (e.target.checked) {
                // Remove from UI
                const errorItem = e.target.closest('.error-item');
                if (errorItem) {
                    errorItem.remove();
                }
                // If no more errors, show "No active errors"
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
            errorsContainer.innerHTML = `<div class="error">Error loading errors: ${response.statusText}</div>`;
            return;
        }

        const result = await response.json();
        const errors = result.errors || [];

        if (errors.length === 0) {
            errorsContainer.innerHTML = '<div class="no-errors">No active errors</div>';
            return;
        }

        const html = errors.map(error => `
            <div class="error-item" data-error-id="${error.id}">
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
                    <div class="error-summary">${error.summary || error.event || 'No summary'}</div>
                    ${error.text ? `<div class="error-text">${error.text}</div>` : ''}
                </div>
            </div>
        `).join('');

        errorsContainer.innerHTML = `<div id="errors-list">${html}</div>`;

        // Attach checkbox handlers
        errorsContainer.querySelectorAll('.error-checkbox-input').forEach(checkbox => {
            checkbox.addEventListener('change', async (e) => {
                if (e.target.checked) {
                    const errorID = parseInt(e.target.dataset.errorId, 10);
                    await markErrorSeen(errorID);
                    // Remove from UI
                    const errorItem = document.querySelector(`[data-error-id="${errorID}"]`);
                    if (errorItem) {
                        errorItem.remove();
                    }
                    // If no more errors, show "No active errors"
                    const listDiv = errorsContainer.querySelector('#errors-list');
                    if (listDiv && listDiv.children.length === 0) {
                        errorsContainer.innerHTML = '<div class="no-errors">No active errors</div>';
                    }
                }
            });
        });
    } catch (err) {
        console.error('Failed to refresh errors:', err);
        errorsContainer.innerHTML = `<div class="error">Error loading errors: ${err.message}</div>`;
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
        }
    } catch (err) {
        console.error('Failed to mark error as seen:', err);
    }
}
