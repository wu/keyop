let panelBody = null;
let lastData = null;

// --- Weather Icons font icon mapping ---
// Maps NWS shortForecast text to a wi-* class from the Weather Icons font.

function getIconClass(shortForecast, isDaytime) {
    const f = (shortForecast || '').toLowerCase();
    const day = isDaytime !== false;

    if (f.includes('thunder') || f.includes('t-storm'))
        return day ? 'wi-day-thunderstorm' : 'wi-night-alt-thunderstorm';
    if (f.includes('blizzard') || f.includes('snow') || f.includes('flurr'))
        return day ? 'wi-day-snow' : 'wi-night-alt-snow';
    if (f.includes('wintry') || f.includes('sleet') || f.includes('mix'))
        return day ? 'wi-day-sleet' : 'wi-night-alt-sleet';
    if (f.includes('fog') || f.includes('haze') || f.includes('mist') || f.includes('smoke'))
        return day ? 'wi-day-fog' : 'wi-night-fog';
    if (f.includes('drizzle') || f.includes('sprinkle'))
        return day ? 'wi-day-sprinkle' : 'wi-night-alt-sprinkle';
    if (f.includes('shower') || f.includes('rain'))
        return day ? 'wi-day-rain' : 'wi-night-alt-rain';
    if (f.includes('wind') || f.includes('breezy') || f.includes('blustery'))
        return day ? 'wi-day-windy' : 'wi-windy';
    if (f.includes('mostly cloudy') || f.includes('overcast') || f.includes('cloudy'))
        return day ? 'wi-day-cloudy' : 'wi-night-alt-cloudy';
    if (f.includes('partly') || f.includes('scattered') || f.includes('few clouds') ||
        f.includes('mostly sunny') || f.includes('mostly clear'))
        return day ? 'wi-day-cloudy' : 'wi-night-alt-partly-cloudy';
    if (f.includes('sunny') || f.includes('clear'))
        return day ? 'wi-day-sunny' : 'wi-night-clear';
    return day ? 'wi-day-sunny' : 'wi-night-clear';
}

function getIcon(shortForecast) {
    const f = (shortForecast || '').toLowerCase();
    if (f.includes('thunder') || f.includes('t-storm')) return ICONS.thunderstorm;
    if (f.includes('snow') || f.includes('flurr') || f.includes('blizzard') || f.includes('wintry')) return ICONS.snow;
    if (f.includes('fog') || f.includes('haze') || f.includes('mist')) return ICONS.fog;
    if (f.includes('rain') || f.includes('shower') || f.includes('drizzle') || f.includes('sleet')) {
        const light = f.includes('slight chance') || f.includes('isolated') || f.includes('patchy') || f.includes('chance');
        return light ? ICONS.lightRain : ICONS.rain;
    }
    if (f.includes('partly') || f.includes('few clouds') || f.includes('mostly sunny') || f.includes('mostly clear')) return ICONS.partlyCloudy;
    if (f.includes('cloud') || f.includes('overcast')) return ICONS.cloudy;
    if (f.includes('sunny') || f.includes('clear')) return ICONS.sunny;
    return ICONS.cloudy;
}

// --- Panel Lifecycle ---

export async function init(el) {
    panelBody = el.querySelector('.panel-body') || el;
    if (!panelBody) return;
    panelBody.innerHTML = '<div class="wp-loading">Loading…</div>';
    try {
        const res = await fetch('/api/tabs/weather/action/fetch-forecast', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({}),
        });
        if (res.ok) {
            const data = await res.json();
            if (data && data.periods && data.periods.length > 0) {
                lastData = data;
                renderPanel();
            } else {
                panelBody.innerHTML = '<div class="wp-loading">No forecast</div>';
            }
        }
    } catch (e) {
        console.error('weather-panel: fetch failed', e);
    }
}

export function onMessage(msg) {
    if (!panelBody) return;
    if (msg.event !== 'weather_forecast' && msg['data-type'] !== 'service.weather.v1' && msg['data-type'] !== 'weather_forecast') return;
    fetch('/api/tabs/weather/action/fetch-forecast', {
        method: 'POST',
        headers: {'Content-Type': 'application/json'},
        body: JSON.stringify({}),
    })
        .then(r => (r.ok ? r.json() : null))
        .then(data => {
            if (data && data.periods && data.periods.length > 0) {
                lastData = data;
                renderPanel();
            }
        })
        .catch(e => console.error('weather-panel: update failed', e));
}

// --- Rendering ---

function renderPanel() {
    if (!lastData || !lastData.periods || lastData.periods.length === 0) {
        panelBody.innerHTML = '<div class="wp-loading">No forecast</div>';
        return;
    }

    const periods = lastData.periods;

    // Identify the first daytime and first nighttime period within the next ~24 h.
    let dayPeriod = null;
    let nightPeriod = null;
    for (const p of periods.slice(0, 4)) {
        if (p.isDaytime && !dayPeriod) dayPeriod = p;
        if (!p.isDaytime && !nightPeriod) nightPeriod = p;
    }

    const current = dayPeriod || nightPeriod || periods[0];
    const high = dayPeriod != null ? dayPeriod.temperature : null;
    const low = nightPeriod != null ? nightPeriod.temperature : null;
    const unit = (current.temperatureUnit || 'F').toUpperCase();
    const shortForecast = current.shortForecast || '';
    const precip = current.probabilityOfPrecipitation?.value ?? null;

    const iconClass = getIconClass(current.shortForecast, current.isDaytime);

    let html = `<div class="wp-panel">`;

    html += `<div class="wp-icon-half">`;
    html += `<div class="wp-icon"><i class="wi ${escHtml(iconClass)}"></i></div>`;
    html += `</div>`;

    html += `<div class="wp-text-half">`;
    html += `<div class="wp-temps">`;
    if (high != null) html += `<span class="wp-temp-high">${Math.round(high)}°</span>`;
    if (high != null && low != null) html += `<span class="wp-temp-sep"> | </span>`;
    if (low != null) html += `<span class="wp-temp-low">${Math.round(low)}°${unit}</span>`;
    html += `</div>`;
    if (shortForecast) {
        html += `<div class="wp-short">${escHtml(shortForecast)}</div>`;
    }
    if (precip != null && precip > 0) {
        html += `<div class="wp-precip">💧 ${Math.round(precip)}%</div>`;
    }
    html += `</div>`;

    html += `</div>`;
    panelBody.innerHTML = html;
}

function escHtml(s) {
    if (!s) return '';
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
