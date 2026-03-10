let container = null;

export function init(el) {
    container = el;
    // Wire up date inputs and refresh button, defaulting to today -> today+7
    const startInput = container.querySelector('#idle-start-date');
    const endInput = container.querySelector('#idle-end-date');
    const refreshBtn = container.querySelector('#idle-refresh-btn');

    const today = new Date();
    const toISODate = d => d.toISOString().slice(0, 10);
    const startDate = new Date(Date.UTC(today.getFullYear(), today.getMonth(), today.getDate()));
    const endDate = new Date(startDate);
    endDate.setDate(endDate.getDate() + 7);

    if (startInput) startInput.value = toISODate(startDate);
    if (endInput) endInput.value = toISODate(endDate);

    function triggerRefresh(startIso, endIso) {
        const body = {
            start: new Date(startIso + 'T00:00:00').toISOString(),
            end: new Date(endIso + 'T00:00:00').toISOString(),
        };
        fetch('/api/tabs/idle/action/refresh-report', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(body)
        })
            .then(response => response.json())
            .then(data => {
                if (data && data.report) {
                    renderReport(data.report);
                }
            })
            .catch(err => console.error('Failed to trigger report refresh:', err));
    }

    if (refreshBtn) {
        refreshBtn.addEventListener('click', () => {
            const s = startInput ? startInput.value : toISODate(startDate);
            const e = endInput ? endInput.value : toISODate(endDate);
            triggerRefresh(s, e);
        });
    }

    // Trigger initial refresh with defaults
    triggerRefresh(startInput ? startInput.value : toISODate(startDate), endInput ? endInput.value : toISODate(endDate));
}

function renderReport(html) {
    if (!container) return;
    const reportEl = container.querySelector('#idle-report-content');
    if (reportEl) {
        reportEl.innerHTML = `<div class="report-markdown">${html || ''}</div>`;
    }
}

export function onMessage(msg) {
    if (!container) return;

    // Check if it's an idle status event
    if (msg.event === 'idle_status' || msg.channelName === 'idle' || msg.serviceType === 'idleMacos') {
        const statusEl = container.querySelector('#idle-status');
        if (statusEl) {
            statusEl.textContent = `Status: ${msg.status || 'unknown'}`;
            statusEl.style.color = msg.status === 'idle' ? '#bb86fc' : '#03dac6';
        }

        const historyEl = container.querySelector('#idle-history');
        if (historyEl) {
            const item = document.createElement('div');
            item.className = 'event-item';
            item.textContent = `[${new Date().toLocaleTimeString()}] ${msg.text || msg.event}`;
            historyEl.prepend(item);

            // Limit history
            if (historyEl.children.length > 20) {
                historyEl.removeChild(historyEl.lastChild);
            }
        }

        // When an idle_status message arrives, refresh the report for the selected range
        const startInput = container.querySelector('#idle-start-date');
        const endInput = container.querySelector('#idle-end-date');
        const s = startInput ? startInput.value : null;
        const e = endInput ? endInput.value : null;
        const body = {};
        if (s) body.start = new Date(s + 'T00:00:00').toISOString();
        if (e) body.end = new Date(e + 'T00:00:00').toISOString();
        fetch('/api/tabs/idle/action/refresh-report', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(body)
        })
            .then(response => response.json())
            .then(data => {
                if (data && data.report) {
                    renderReport(data.report);
                }
            })
            .catch(err => console.error('Failed to trigger report refresh:', err));
    }
}
