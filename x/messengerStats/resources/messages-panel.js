let panelBody = null;
const channelCounts = {}; // Map of channel -> count

export function init(el) {
    panelBody = el.querySelector('.panel-body') || el;
    if (!panelBody) return;

    panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Loading...</div>';
}

export function onMessage(msg) {
    if (!panelBody) return;

    // Listen for messenger stats messages
    if (msg.event === 'stats' && msg.serviceType === 'messengerStats') {
        const data = msg.data || {};
        if (data.channelMessageCounts) {
            // Update the channel counts from the stats
            Object.assign(channelCounts, data.channelMessageCounts);
            updatePanel();
        }
    }
}

function updatePanel() {
    if (!panelBody) return;

    const entries = Object.entries(channelCounts);
    if (entries.length === 0) {
        panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">No data</div>';
        return;
    }

    // Sort by count descending (most to least)
    entries.sort((a, b) => b[1] - a[1]);

    // Limit to top 10 entries
    const topEntries = entries.slice(0, 10);

    let html = '<table style="width: 100%; font-size: 0.85rem; border-collapse: collapse;">';
    for (const [channel, count] of topEntries) {
        html += `<tr style="border-bottom: 1px solid var(--border-color, rgba(255,255,255,0.1));">`;
        html += `<td style="padding: 4px 6px; text-align: left; color: var(--text);">${channel}</td>`;
        html += `<td style="padding: 4px 6px; text-align: right; font-weight: bold; color: var(--accent-green, #52c41a);">${count}</td>`;
        html += `</tr>`;
    }
    html += '</table>';
    panelBody.innerHTML = html;
}
