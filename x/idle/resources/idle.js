let container = null;

export function init(el) {
    container = el;
    // Wire up date inputs and refresh button, defaulting to yesterday -> today
    const startInput = container.querySelector('#idle-start-date');
    const endInput = container.querySelector('#idle-end-date');
    const refreshBtn = container.querySelector('#idle-refresh-btn');

    const today = new Date();
    const toISODate = d => d.toISOString().slice(0, 10);
    const endDate = new Date(Date.UTC(today.getFullYear(), today.getMonth(), today.getDate()));
    const startDate = new Date(endDate);
    startDate.setDate(startDate.getDate() - 1); // yesterday

    if (startInput) startInput.value = toISODate(startDate);
    if (endInput) endInput.value = toISODate(endDate);

    function triggerRefresh(startIso, endIso) {
        const body = {};
        if (startIso) body.start = new Date(startIso + 'T00:00:00').toISOString();
        if (endIso) body.end = new Date(endIso + 'T00:00:00').toISOString();
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

    // Trigger initial refresh only if the tab is active. Otherwise wait until it becomes active.
    function isActive() {
        return container && container.classList && container.classList.contains('active');
    }

    if (isActive()) {
        triggerRefresh(startInput ? startInput.value : toISODate(startDate), endInput ? endInput.value : toISODate(endDate));
    } else {
        // Observe the container for class changes to detect when it becomes active
        try {
            const obs = new MutationObserver((mutations, observer) => {
                if (isActive()) {
                    triggerRefresh(startInput ? startInput.value : toISODate(startDate), endInput ? endInput.value : toISODate(endDate));
                    observer.disconnect();
                }
            });
            obs.observe(container, {attributes: true, attributeFilter: ['class']});
        } catch (e) {
            // Fallback: if MutationObserver isn't available, trigger once after a short delay
            setTimeout(() => {
                if (isActive()) triggerRefresh(startInput ? startInput.value : toISODate(startDate), endInput ? endInput.value : toISODate(endDate));
            }, 500);
        }
    }
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

    // Only respond to idle messages when the tab is active
    if (!container.classList || !container.classList.contains('active')) return;

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

        // When an idle_status message arrives, refresh the report using server defaults
        fetch('/api/tabs/idle/action/refresh-report', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
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
