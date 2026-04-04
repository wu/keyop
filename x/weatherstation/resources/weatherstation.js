let container = null;
let lastData = null;

// DOM element refs
let elWindSpeed, elWindGauge, elRainValue, elRainRate, elOutTemp, elOutHumidity, elInTemp, elInHumidity;
let elWindDir, elPressure, elUV, elUVLevel, elLight, elTime, elGust;
let elForecastIcon, elPressureGauge;
let windGaugeCanvas = null;
let windGaugeCtx = null;
let pressureGaugeCanvas = null;
let pressureGaugeCtx = null;

function degreesToCompass(deg) {
    const dirs = ['N', 'NNE', 'NE', 'ENE', 'E', 'ESE', 'SE', 'SSE', 'S', 'SSW', 'SW', 'WSW', 'W', 'WNW', 'NW', 'NNW'];
    return dirs[Math.round(deg / 22.5) % 16];
}

function getUVLevel(uv) {
    if (uv === null || uv === undefined) return '—';
    if (uv === 0) return 'Dark';
    if (uv < 3) return 'Low';
    if (uv < 6) return 'Moderate';
    if (uv < 8) return 'High';
    if (uv < 11) return 'Very High';
    return 'Extreme';
}

function drawWindGauge(speed) {
    if (!windGaugeCtx) return;

    const canvas = windGaugeCanvas;
    const ctx = windGaugeCtx;
    const w = canvas.width;
    const h = canvas.height;
    const centerX = w / 2;
    const centerY = h / 2;
    const radius = Math.min(w, h) / 2 - 5;

    // Clear canvas
    ctx.fillStyle = 'transparent';
    ctx.fillRect(0, 0, w, h);

    // Draw gauge circle
    ctx.strokeStyle = '#7cfc00';
    ctx.lineWidth = 2;
    ctx.beginPath();
    ctx.arc(centerX, centerY, radius, 0, 2 * Math.PI);
    ctx.stroke();

    // Draw gauge background
    ctx.fillStyle = 'rgba(124, 252, 0, 0.05)';
    ctx.beginPath();
    ctx.arc(centerX, centerY, radius - 3, 0, 2 * Math.PI);
    ctx.fill();

    // Draw cardinal directions
    ctx.fillStyle = '#7cfc00';
    ctx.font = 'bold 18px Arial';
    ctx.textAlign = 'center';
    ctx.textBaseline = 'middle';

    const dirs = ['N', 'E', 'S', 'W'];
    const angles = [0, Math.PI / 2, Math.PI, 3 * Math.PI / 2];

    for (let i = 0; i < dirs.length; i++) {
        const x = centerX + Math.cos(angles[i] - Math.PI / 2) * (radius - 12);
        const y = centerY + Math.sin(angles[i] - Math.PI / 2) * (radius - 12);
        ctx.fillText(dirs[i], x, y);
    }

    // Draw needle pointing north (up) and move based on gust value proportionally
    const maxGust = 50; // Max value for full rotation
    const angle = (Math.min(speed, maxGust) / maxGust) * (Math.PI * 2) - Math.PI / 2;

    // Draw needle
    ctx.strokeStyle = '#7cfc00';
    ctx.lineWidth = 3;
    ctx.beginPath();
    ctx.moveTo(centerX, centerY);
    const needleX = centerX + Math.cos(angle) * (radius - 15);
    const needleY = centerY + Math.sin(angle) * (radius - 15);
    ctx.lineTo(needleX, needleY);
    ctx.stroke();

    // Draw center circle
    ctx.fillStyle = '#7cfc00';
    ctx.beginPath();
    ctx.arc(centerX, centerY, 4, 0, 2 * Math.PI);
    ctx.fill();
}

function drawPressureGauge(pressure) {
    if (!pressureGaugeCtx) return;

    const canvas = pressureGaugeCanvas;
    const ctx = pressureGaugeCtx;
    const w = canvas.width;
    const h = canvas.height;

    // Clear canvas
    ctx.clearRect(0, 0, w, h);

    const padding = 8;
    const gaugeWidth = 20;
    const gaugeHeight = h - (padding * 2);
    const gaugeX = (w - gaugeWidth) / 2;
    const gaugeY = padding;

    const minPressure = 28;
    const maxPressure = 31;
    const range = maxPressure - minPressure;

    // Helper to convert pressure to Y coordinate
    const getY = (p) => {
        const normalized = (p - minPressure) / range;
        return gaugeY + gaugeHeight - (normalized * gaugeHeight);
    };

    // Draw gauge background box
    ctx.strokeStyle = '#cccccc';
    ctx.lineWidth = 1;
    ctx.strokeRect(gaugeX, gaugeY, gaugeWidth, gaugeHeight);

    // Draw pressure indicator bar if we have valid pressure
    if (pressure !== null && pressure !== undefined) {
        const barY = getY(pressure);
        ctx.fillStyle = '#ffff66';
        ctx.fillRect(gaugeX, barY - 2, gaugeWidth, 4);
    }
}


export async function init(el) {
    container = el;
    const body = el.querySelector('.weatherstation-container') || el;

    // Get element refs
    elWindSpeed = body.querySelector('.ws-speed');
    elGust = body.querySelector('.ws-gust');
    elWindGauge = body.querySelector('#ws-wind-gauge');
    elRainValue = body.querySelector('.ws-rain-value');
    elRainRate = body.querySelector('.ws-rain-rate');
    elOutTemp = body.querySelector('.ws-outdoor .ws-temp-value');
    elOutHumidity = body.querySelector('.ws-outdoor .ws-humidity-value');
    elInTemp = body.querySelector('.ws-indoor .ws-temp-value');
    elInHumidity = body.querySelector('.ws-indoor .ws-humidity-value');
    elPressure = body.querySelector('.ws-pressure-value');
    elUV = body.querySelector('.ws-uvi-value');
    elUVLevel = body.querySelector('.ws-uvi-level');
    elLight = body.querySelector('.ws-light-value');
    elTime = body.querySelector('.ws-time');
    elForecastIcon = body.querySelector('.ws-forecast-icon');
    elPressureGauge = body.querySelector('#ws-pressure-gauge');

    // Setup wind gauge canvas
    if (elWindGauge) {
        windGaugeCanvas = elWindGauge;
        windGaugeCtx = windGaugeCanvas.getContext('2d');
        drawWindGauge(0);
    }

    // Setup pressure gauge canvas
    if (elPressureGauge) {
        pressureGaugeCanvas = elPressureGauge;
        pressureGaugeCtx = pressureGaugeCanvas.getContext('2d');
        drawPressureGauge(null);
    }

    // Fetch initial data
    try {
        const resp = await fetch('/api/tabs/weatherstation/action/get-current', {method: 'POST'});
        if (!resp.ok) throw new Error(resp.statusText);
        const data = await resp.json();
        if (data) updateDisplay(data);
    } catch (err) {
        console.error('Failed to fetch weatherstation data:', err);
    }
}

function updateDisplay(data) {
    if (!data) return;
    lastData = data;

    const d = data;

    // Wind
    if (elWindSpeed && d.windSpeed !== null && d.windSpeed !== undefined) {
        elWindSpeed.textContent = d.windSpeed.toFixed(1);
        if (windGaugeCanvas) drawWindGauge(d.windSpeed);
    }

    // Gust
    if (elGust && d.gustSpeed !== null && d.gustSpeed !== undefined) {
        elGust.textContent = d.gustSpeed.toFixed(1);
    }

    // Rain
    if (elRainValue && d.dailyRain !== null && d.dailyRain !== undefined) {
        elRainValue.textContent = d.dailyRain.toFixed(2);
    }

    // Rain Rate
    if (elRainRate && d.rainRate !== null && d.rainRate !== undefined) {
        elRainRate.textContent = d.rainRate.toFixed(2);
    }

    // Outdoor temp (show max/min if available, otherwise current)
    if (elOutTemp) {
        if (d.outTemp !== null && d.outTemp !== undefined) {
            elOutTemp.textContent = d.outTemp.toFixed(1);
        } else {
            elOutTemp.textContent = '—';
        }
    }

    // Outdoor humidity
    if (elOutHumidity && d.outHumidity !== null && d.outHumidity !== undefined) {
        elOutHumidity.textContent = d.outHumidity;
    }

    // Indoor temp
    if (elInTemp && d.inTemp !== null && d.inTemp !== undefined) {
        elInTemp.textContent = d.inTemp.toFixed(1);
    }

    // Indoor humidity
    if (elInHumidity && d.inHumidity !== null && d.inHumidity !== undefined) {
        elInHumidity.textContent = d.inHumidity;
    }

    // Pressure
    if (elPressure && d.barometerRel !== null && d.barometerRel !== undefined) {
        elPressure.textContent = d.barometerRel.toFixed(2);

        // Redraw gauge
        if (pressureGaugeCanvas) drawPressureGauge(d.barometerRel);
    }

    // UV
    if (elUV) {
        if (d.uV !== null && d.uV !== undefined) {
            elUV.textContent = d.uV;
        } else {
            elUV.textContent = '0';
        }
    }

    // UV Level
    if (elUVLevel) {
        if (d.uV !== null && d.uV !== undefined) {
            elUVLevel.textContent = getUVLevel(d.uV);
        } else {
            elUVLevel.textContent = getUVLevel(0);
        }
    }

    // Light (solar radiation)
    if (elLight && d.solarRadiation !== null && d.solarRadiation !== undefined) {
        elLight.textContent = Math.round(d.solarRadiation);
    }

    // Time
    if (elTime) {
        const now = new Date();
        elTime.textContent = now.toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'});
    }
}

export async function onMessage(msg) {
    if (!container) return;
    if (msg.serviceType !== 'weatherstation' || msg.event !== 'weatherstation') return;

    const data = msg.data || {};

    // Transform message data to match our expected format
    const displayData = {
        windSpeed: data.windSpeed,
        gustSpeed: data.gustSpeed,
        dailyRain: data.dailyRain,
        rainRate: data.rainRate,
        outTemp: data.outTemp,
        outHumidity: data.outHumidity,
        inTemp: data.inTemp,
        inHumidity: data.inHumidity,
        barometerRel: data.barometerRel,
        uV: data.uV,
        solarRadiation: data.solarRadiation,
        wh65Batt: data.wh65Batt,
        windDir: data.windDir,
    };

    updateDisplay(displayData);
}

export function focusItems() {
    // Not used for weather station, but required by interface
}

export function canReturnToTabs() {
    return true;
}
