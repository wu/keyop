// Import the idle-report visualization module
import * as idleReport from './idle-report.js';

let container = null;

export function init(el) {
    container = el;

    // Wire up date inputs and refresh button
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

    // Initialize the report visualization
    idleReport.init(container);

    if (refreshBtn) {
        refreshBtn.addEventListener('click', () => {
            const s = startInput ? startInput.value : toISODate(startDate);
            const e = endInput ? endInput.value : toISODate(endDate);
            // Trigger refresh through custom event that idleReport listens for
            container.dispatchEvent(new CustomEvent('idle-refresh', {
                detail: {start: s, end: e}
            }));
        });
    }
}

export function onMessage(msg) {
    if (!container) return;

    // Delegate to idleReport module
    idleReport.onMessage(msg);

    // Check if it's an idle status event
    if (msg.event === 'idle_status' || msg.channelName === 'idle' || msg.serviceType === 'idleMacos') {
        const statusEl = container.querySelector('#idle-status');
        if (statusEl) {
            statusEl.textContent = `Status: ${msg.status || 'unknown'}`;
            statusEl.style.color = msg.status === 'idle' ? '#6b21a8' : '#c77dff';
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
    }
}
