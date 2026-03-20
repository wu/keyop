let body = null;

// DOM element refs
let elOutTemp, elInTemp, elOutHumidity, elInHumidity;
let elWindSpeed, elWindDir, elWindGust;
let elPressure, elRainRate, elUV, elSolar;
let elUpdated;

export function init(el) {
    body = el.querySelector('.panel-body') || el;
    if (!body) return;

    body.innerHTML = `
        <div class="sun-wrapper" style="gap: 0.5rem; padding: 0.5rem 0; justify-content: flex-start;">
            <div class="ws-grid">
                <div class="ws-metric">
                    <div class="sun-label">Outdoor</div>
                    <div class="sun-value ws-out-temp">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">Indoor</div>
                    <div class="sun-value ws-in-temp">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">Out Humidity</div>
                    <div class="sun-value ws-out-humidity">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">In Humidity</div>
                    <div class="sun-value ws-in-humidity">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">Wind</div>
                    <div class="sun-value ws-wind-speed">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">Gust</div>
                    <div class="sun-value ws-wind-gust">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">Direction</div>
                    <div class="sun-value ws-wind-dir">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">Pressure</div>
                    <div class="sun-value ws-pressure">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">Rain Rate</div>
                    <div class="sun-value ws-rain-rate">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">UV Index</div>
                    <div class="sun-value ws-uv">—</div>
                </div>
                <div class="ws-metric">
                    <div class="sun-label">Solar</div>
                    <div class="sun-value ws-solar">—</div>
                </div>
            </div>
            <div class="ws-updated"></div>
        </div>
    `;

    elOutTemp = body.querySelector('.ws-out-temp');
    elInTemp = body.querySelector('.ws-in-temp');
    elOutHumidity = body.querySelector('.ws-out-humidity');
    elInHumidity = body.querySelector('.ws-in-humidity');
    elWindSpeed = body.querySelector('.ws-wind-speed');
    elWindGust = body.querySelector('.ws-wind-gust');
    elWindDir = body.querySelector('.ws-wind-dir');
    elPressure = body.querySelector('.ws-pressure');
    elRainRate = body.querySelector('.ws-rain-rate');
    elUV = body.querySelector('.ws-uv');
    elSolar = body.querySelector('.ws-solar');
    elUpdated = body.querySelector('.ws-updated');
}

export function onMessage(msg) {
    if (!body) return;
    if (msg.serviceType !== 'weatherstation' || msg.event !== 'weatherstation') return;

    const d = msg.data || {};

    if (elOutTemp) elOutTemp.textContent = d.outTemp != null ? `${d.outTemp.toFixed(1)}°F` : '—';
    if (elInTemp) elInTemp.textContent = d.inTemp != null ? `${d.inTemp.toFixed(1)}°F` : '—';
    if (elOutHumidity) elOutHumidity.textContent = d.outHumidity != null ? `${d.outHumidity}%` : '—';
    if (elInHumidity) elInHumidity.textContent = d.inHumidity != null ? `${d.inHumidity}%` : '—';
    if (elWindSpeed) elWindSpeed.textContent = d.windSpeed != null ? `${d.windSpeed.toFixed(1)} mph` : '—';
    if (elWindGust) elWindGust.textContent = d.windGust != null ? `${d.windGust.toFixed(1)} mph` : '—';
    if (elWindDir) elWindDir.textContent = d.windDir != null ? degreesToCompass(d.windDir) : '—';
    if (elPressure) elPressure.textContent = d.barometerRel != null ? `${d.barometerRel.toFixed(2)} inHg` : '—';
    if (elRainRate) elRainRate.textContent = d.rainRate != null ? `${d.rainRate.toFixed(2)} in/hr` : '—';
    if (elUV) elUV.textContent = d.uV != null ? String(d.uV) : '—';
    if (elSolar) elSolar.textContent = d.solarRadiation != null ? `${Math.round(d.solarRadiation)} W/m²` : '—';

    if (elUpdated) {
        const now = new Date();
        elUpdated.textContent = `Updated ${now.toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'})}`;
    }
}

function degreesToCompass(deg) {
    const dirs = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW'];
    return dirs[Math.round(deg / 22.5) % 16];
}
