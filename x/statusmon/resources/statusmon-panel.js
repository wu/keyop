let panelBody = null;

export function init(el) {
    panelBody = el.querySelector('.panel-body') || el;
    if (!panelBody) return;

    // Fetch status data and render panel
    fetch('/api/tabs/statusmon/action/fetch-status', {method: 'POST'})
        .then(resp => resp.json())
        .then(data => {
            if (data && data.statuses) {
                updatePanel(data.statuses);
            }
        })
        .catch(err => console.error('Failed to fetch status for panel:', err));
}

export function onMessage(msg) {
    if (!panelBody) return;

    // Refetch status on any status message
    if (msg.event === 'statusmon' || msg['data-type'] === 'core.status.v1') {
        fetch('/api/tabs/statusmon/action/fetch-status', {method: 'POST'})
            .then(resp => resp.json())
            .then(data => {
                if (data && data.statuses) {
                    updatePanel(data.statuses);
                }
            })
            .catch(err => console.error('Failed to refresh status panel:', err));
    }
}

function updatePanel(statuses) {
    if (!panelBody) return;

    // Filter out acknowledged problems from the counts
    const activeStatuses = statuses.filter(s => !s.acknowledged || (s.level || 'ok').toLowerCase() === 'ok');

    // Calculate status counts (excluding acked problems)
    const totalServices = activeStatuses.length;
    const okServices = activeStatuses.filter(s => (s.level || 'ok').toLowerCase() === 'ok').length;
    const warningServices = activeStatuses.filter(s => (s.level || 'ok').toLowerCase() === 'warning');
    const criticalServices = activeStatuses.filter(s => (s.level || 'ok').toLowerCase() === 'critical');
    const ackedProblems = statuses.filter(s => s.acknowledged && ((s.level || 'ok').toLowerCase() === 'warning' || (s.level || 'ok').toLowerCase() === 'critical'));

    // Determine overall status (based only on active, non-acked problems)
    let overallStatus = 'ok';
    if (criticalServices.length > 0) {
        overallStatus = 'critical';
    } else if (warningServices.length > 0) {
        overallStatus = 'warning';
    }

    // Render panel content with three sections: traffic light, header, and details
    let html = '<div style="display: flex; flex-direction: column; height: 100%; padding: 12px;">';

    // Top section - Traffic light
    html += '<div style="flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center;">';
    html += '<div style="display: flex; justify-content: center; gap: 12px;">';
    // Red light (critical)
    const redOpacity = overallStatus === 'critical' ? '1' : '0.2';
    html += `<div style="width: 16px; height: 16px; border-radius: 50%; background-color: #ef4444; opacity: ${redOpacity};"></div>`;
    // Yellow light (warning)
    const yellowOpacity = overallStatus === 'warning' ? '1' : '0.2';
    html += `<div style="width: 16px; height: 16px; border-radius: 50%; background-color: #f59e0b; opacity: ${yellowOpacity};"></div>`;
    // Green light (ok)
    const greenOpacity = overallStatus === 'ok' ? '1' : '0.2';
    html += `<div style="width: 16px; height: 16px; border-radius: 50%; background-color: #10b981; opacity: ${greenOpacity};"></div>`;
    html += '</div>';
    html += '</div>';

    // Middle section - Header (styled like "Aurora Forecast")
    html += `<div style="font-weight: bold; margin-bottom: 8px; text-align: center; color: var(--accent);">Service Status</div>`;

    // Bottom section - Status details
    html += '<div style="flex: 1; display: flex; flex-direction: column; align-items: center; justify-content: center;">';

    if (okServices === totalServices) {
        // All OK - show label and count like sun-label/sun-value structure
        html += '<div style="font-size: 0.75rem; color: var(--text); opacity: 0.7;">Overall Status</div>';
        html += `<div style="font-size: 1rem; font-weight: 600; color: var(--accent-green, #10b981); margin-top: 4px;">${okServices} OK</div>`;
    } else {
        // Show problems - compact format
        let problemsHtml = '';

        if (criticalServices.length > 0) {
            problemsHtml += `<div style="font-size: 0.85em; color: #ef4444; font-weight: bold;">Critical: ${criticalServices.length}</div>`;
            for (const service of criticalServices) {
                problemsHtml += `<div style="font-size: 0.8em; color: #ef4444;">${service.name}</div>`;
            }
        }

        if (warningServices.length > 0) {
            if (problemsHtml) problemsHtml += '<div style="margin-top: 4px;"></div>';
            problemsHtml += `<div style="font-size: 0.85em; color: #f59e0b; font-weight: bold;">Warnings: ${warningServices.length}</div>`;
            for (const service of warningServices) {
                problemsHtml += `<div style="font-size: 0.8em; color: #f59e0b;">${service.name}</div>`;
            }
        }

        if (ackedProblems.length > 0) {
            if (problemsHtml) problemsHtml += '<div style="margin-top: 4px;"></div>';
            problemsHtml += `<div style="font-size: 0.85em; color: var(--accent-pink); font-weight: bold;">Acked: ${ackedProblems.length}</div>`;
        }

        html += '<div style="font-size: 0.8em;">' + problemsHtml + '</div>';
    }

    html += '</div>';
    html += '</div>';

    panelBody.innerHTML = html;
}
