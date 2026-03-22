let weatherContainer = null;

export async function init(container) {
    weatherContainer = container;
    await refreshForecast();
}

export function onMessage(msg) {
    if (!weatherContainer) return;
    if (msg.event !== 'weather_forecast' && msg['data-type'] !== 'service.weather.v1' && msg['data-type'] !== 'weather_forecast') return;
    refreshForecast();
}

async function refreshForecast() {
    if (!weatherContainer) return;
    try {
        const res = await fetch('/api/tabs/weather/action/fetch-forecast', {method: 'POST'});
        if (!res.ok) {
            weatherContainer.querySelector('#weather-forecast').innerHTML =
                '<div style="color:var(--text);opacity:0.6;padding:20px">Failed to load forecast</div>';
            return;
        }
        const data = await res.json();
        renderForecast(data);
    } catch (e) {
        console.error('weather: failed to fetch forecast', e);
    }
}

function renderForecast(data) {
    const el = weatherContainer.querySelector('#weather-forecast');
    if (!el) return;
    const periods = data.periods;
    if (!periods || periods.length === 0) {
        el.innerHTML = '<div style="color:var(--text);opacity:0.6;padding:20px;text-align:center">No forecast data available</div>';
        return;
    }

    // Group into day pairs: [daytime, nighttime]
    const days = [];
    for (let i = 0; i < periods.length; i++) {
        const p = periods[i];
        if (p.isDaytime) {
            // Peek ahead for matching night
            const night = periods[i + 1] && !periods[i + 1].isDaytime ? periods[i + 1] : null;
            days.push({day: p, night});
            if (night) i++;
        } else {
            // Forecast starts at night (e.g. Tonight)
            days.push({day: null, night: p});
        }
    }

    let html = '';
    if (data.timestamp) {
        const ts = new Date(data.timestamp);
        html += `<div class="weather-updated">Updated ${ts.toLocaleString()}</div>`;
    }

    html += `<div class="weather-days">`;
    days.forEach(({day, night}) => {
        const label = day ? day.name.replace(' Night', '').replace(' night', '') : night.name;
        const high = day ? `${Math.round(day.temperature)}°${day.temperatureUnit}` : null;
        const low = night ? `${Math.round(night.temperature)}°${night.temperatureUnit}` : null;
        const dayPrecip = day?.probabilityOfPrecipitation?.value;
        const nightPrecip = night?.probabilityOfPrecipitation?.value;
        const maxPrecip = Math.max(dayPrecip ?? 0, nightPrecip ?? 0);
        const shortForecast = day ? day.shortForecast : night.shortForecast;
        const detailedForecast = [day?.detailedForecast, night?.detailedForecast].filter(Boolean).join(' ');

        html += `<div class="weather-day-row">
            <div class="weather-day-header">
                <span class="weather-day-label">${escapeHtml(label)}</span>
                <span class="weather-day-short">${escapeHtml(shortForecast)}</span>
                <span class="weather-day-temps">`;
        if (high) html += `<span class="weather-temp-high">${high}</span>`;
        if (high && low) html += `<span class="weather-temp-sep"> / </span>`;
        if (low) html += `<span class="weather-temp-low">${low}</span>`;
        html += `</span>`;
        if (maxPrecip > 0) {
            html += `<span class="weather-precip">💧 ${Math.round(maxPrecip)}%</span>`;
        }
        if (day) html += `<span class="weather-wind">🌬 ${escapeHtml(day.windSpeed)} ${escapeHtml(day.windDirection)}</span>`;
        html += `</div>`;
        if (detailedForecast) {
            html += `<div class="weather-detail">${escapeHtml(detailedForecast)}</div>`;
        }
        html += `</div>`;
    });
    html += `</div>`;
    el.innerHTML = html;
}

function escapeHtml(s) {
    if (!s) return '';
    return String(s).replace(/&/g, '&amp;').replace(/</g, '&lt;').replace(/>/g, '&gt;');
}
