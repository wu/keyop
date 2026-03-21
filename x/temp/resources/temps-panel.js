let panelBody = null;
const tempReadings = {}; // Map of serviceName -> tempF
let metricConfigs = {};  // Map of serviceName -> {displayName, color}

export async function init(el) {
    panelBody = el.querySelector('.panel-body') || el;
    if (!panelBody) return;

    panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">Temps loading...</div>';

    try {
        const [tempsRes, configRes] = await Promise.all([
            fetch('/api/tabs/temps/action/fetch-temps', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({}),
            }),
            fetch('/api/tabs/temps/action/get-metric-configs', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({}),
            }),
        ]);

        if (configRes.ok) {
            const cfg = await configRes.json();
            if (cfg.configs) metricConfigs = cfg.configs;
        }

        if (tempsRes.ok) {
            const data = await tempsRes.json();
            if (data.readings && Array.isArray(data.readings)) {
                for (const reading of data.readings) {
                    tempReadings[reading.serviceName] = reading.tempF;
                }
                updatePanel();
            }
        }
    } catch (e) {
        console.error('Failed to fetch temps:', e);
    }
}

export function onMessage(msg) {
    if (!panelBody) return;

    // Listen for temp-related messages
    if (msg.dataType === 'core.temp.v1' || msg.event === 'temp') {
        const data = msg.data || {};
        const serviceName = msg.serviceName || 'Unknown';

        // Store the temperature (convert if needed)
        let tempF = data.temp_f !== undefined ? data.temp_f : (data.tempF !== undefined ? data.tempF : null);
        if (tempF !== null) {
            tempReadings[serviceName] = tempF;
            updatePanel();
        }
    }
}

function updatePanel() {
    if (!panelBody) return;

    const entries = Object.entries(tempReadings);
    if (entries.length === 0) {
        panelBody.innerHTML = '<div style="text-align: center; color: var(--text); opacity: 0.7;">No temps</div>';
        return;
    }

    // Sort by temperature descending (highest first)
    entries.sort((a, b) => b[1] - a[1]);

    let html = '<table style="width: 100%; font-size: 0.85rem; border-collapse: collapse;">';
    for (const [serviceName, tempF] of entries) {
        const cfg = metricConfigs[serviceName] || {};
        const label = cfg.displayName || serviceName;
        const color = cfg.color || 'var(--accent-orange, #ff8a3d)';
        html += `<tr style="border-bottom: 1px solid var(--border-color, rgba(255,255,255,0.1));">`;
        html += `<td style="padding: 4px 6px; text-align: left; color: ${color};">${label}</td>`;
        html += `<td style="padding: 4px 6px; text-align: right; font-weight: bold; color: ${color};">${tempF.toFixed(1)}°F</td>`;
        html += `</tr>`;
    }
    html += '</table>';
    panelBody.innerHTML = html;
}
