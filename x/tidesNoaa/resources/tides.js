let container = null;

export function init(el) {
    container = el;
    // Wire up date inputs and refresh button, defaulting to today -> today+7
    const startInput = container.querySelector('#tide-start-date');
    const endInput = container.querySelector('#tide-end-date');
    const refreshBtn = container.querySelector('#tide-refresh-btn');

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
        fetch('/api/tabs/tides/action/refresh-report', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(body)
        })
            .then(response => response.json())
            .then(data => {
                if (data && data.report) {
                    const reportEl = container.querySelector('#tide-report-content');
                    if (reportEl) {
                        reportEl.innerHTML = `<div class="report-markdown">${data.report}</div>`;
                    }
                }
            })
            .catch(err => console.error('Failed to trigger tide report refresh:', err));
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

function renderReportFromData(data) {
    if (!container) return;
    const reportEl = container.querySelector('#tide-report-content');
    if (!reportEl) return;
    if (!data || !data.periods) {
        reportEl.innerHTML = '<div class="report-markdown">No daylight low tides</div>';
        return;
    }
    let html = '<div class="report-markdown">';
    html += '<h4>Daylight low tide periods</h4>';
    html += '<table><thead><tr><th>Date</th><th>Start</th><th>End</th><th>Min</th></tr></thead><tbody>';
    data.periods.forEach(p => {
        html += `<tr><td>${p.date}</td><td>${p.start}</td><td>${p.end}</td><td>${p.minValue.toFixed(2)} ft</td></tr>`;
    });
    html += '</tbody></table>';
    html += '</div>';
    reportEl.innerHTML = html;
}

export function onMessage(msg) {
    if (!container) return;

    // Check if it's a tide status event
    if (msg.event === 'tide' || msg.serviceType === 'tidesNoaa') {
        const statusEl = container.querySelector('#tide-status');
        if (statusEl) {
            statusEl.textContent = msg.text || `Tide: ${msg.metric} ft`;
            statusEl.style.color = '#bb86fc';
        }

        const historyEl = container.querySelector('#tide-history');
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

        // If the tide message includes report-like periods data, trigger a
        // refresh so the server-side formatted report (Markdown->HTML) is shown.
        if (msg.data && msg.data.periods) {
            const startInput = container.querySelector('#tide-start-date');
            const endInput = container.querySelector('#tide-end-date');
            const s = startInput ? startInput.value : null;
            const e = endInput ? endInput.value : null;
            const body = {};
            if (s) body.start = new Date(s + 'T00:00:00').toISOString();
            if (e) body.end = new Date(e + 'T00:00:00').toISOString();
            fetch('/api/tabs/tides/action/refresh-report', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(body)
            })
                .then(response => response.json())
                .then(data => {
                    if (data && data.report) {
                        const reportEl = container.querySelector('#tide-report-content');
                        if (reportEl) {
                            reportEl.innerHTML = `<div class="report-markdown">${data.report}</div>`;
                        }
                    }
                })
                .catch(err => console.error('Failed to trigger tide report refresh:', err));
        }
    }
}

