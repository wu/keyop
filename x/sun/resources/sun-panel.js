let container = null;
let body = null;
let elEvent = null;
let elRemaining = null;
let elDay = null;
let elNight = null;
let elIcon = null;
let remainingTimer = null;
let nextEventTime = null;

const sunSVG = `
<svg viewBox="0 0 24 24" fill="none" xmlns="http://www.w3.org/2000/svg" aria-hidden="true">
  <circle cx="12" cy="12" r="4" fill="currentColor" />
  <g stroke="currentColor" stroke-width="1.2" stroke-linecap="round">
    <path d="M12 2v2"/>
    <path d="M12 20v2"/>
    <path d="M4.2 4.2l1.4 1.4"/>
    <path d="M18.4 18.4l1.4 1.4"/>
    <path d="M2 12h2"/>
    <path d="M20 12h2"/>
    <path d="M4.2 19.8l1.4-1.4"/>
    <path d="M18.4 5.6l1.4-1.4"/>
  </g>
</svg>`;

function formatDuration(ms) {
    if (ms == null || isNaN(ms)) return '—';
    ms = Math.max(0, ms);
    const totalSec = Math.floor(ms / 1000);
    const h = Math.floor(totalSec / 3600);
    const m = Math.floor((totalSec % 3600) / 60);
    if (h > 0) return `${h}h ${m}m`;
    return `${m}m`;
}

function formatRemaining(ms) {
    if (ms == null || isNaN(ms)) return '—';
    ms = Math.max(0, ms);
    const totalSec = Math.floor(ms / 1000);
    const days = Math.floor(totalSec / 86400);
    const h = Math.floor((totalSec % 86400) / 3600);
    const m = Math.floor((totalSec % 3600) / 60);
    const parts = [];
    if (days > 0) parts.push(`${days}d`);
    if (h > 0) parts.push(`${h}h`);
    parts.push(`${m}m`);
    return parts.join(' ');
}

function parseDurationString(s) {
    // Accept formats like "13h2m3s" or "13h 2m 3s"
    if (!s || typeof s !== 'string') return NaN;
    let hours = 0, minutes = 0, seconds = 0;
    const hMatch = s.match(/(\d+)h/);
    const mMatch = s.match(/(\d+)m/);
    const sMatch = s.match(/(\d+(?:\.\d+)?)s/);
    if (hMatch) hours = parseInt(hMatch[1], 10);
    if (mMatch) minutes = parseInt(mMatch[1], 10);
    if (sMatch) seconds = Math.floor(parseFloat(sMatch[1]));
    return ((hours * 3600) + (minutes * 60) + seconds) * 1000;
}

function formatTime(d) {
    if (!d) return '';
    try {
        return (new Date(d)).toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'});
    } catch (e) {
        return '';
    }
}

function clearRemainingTimer() {
    if (remainingTimer) {
        clearInterval(remainingTimer);
        remainingTimer = null;
    }
    // keep nextEventTime and dataset so re-init can resume the countdown
}

function clearCountdown() {
    // stop ticking and forget the next event entirely
    if (remainingTimer) {
        clearInterval(remainingTimer);
        remainingTimer = null;
    }
    nextEventTime = null;
    if (body && body.dataset) {
        delete body.dataset.nextEventTime;
        delete body.dataset.nextEventName;
    }
    try {
        if (typeof localStorage !== 'undefined') {
            localStorage.removeItem('panel-sun-nextEventTime');
            localStorage.removeItem('panel-sun-nextEventName');
        }
    } catch (e) {
        // ignore
    }
}

function updateRemainingDisplay() {
    if (!elRemaining) return;

    // If we don't have an in-memory nextEventTime, try to resume from DOM dataset
    if (!nextEventTime && body && body.dataset && body.dataset.nextEventTime) {
        const parsed = new Date(body.dataset.nextEventTime);
        if (!isNaN(parsed) && parsed > new Date()) {
            nextEventTime = parsed;
        } else {
            // stale value, remove it
            delete body.dataset.nextEventTime;
            delete body.dataset.nextEventName;
        }
    }

    if (!nextEventTime) {
        elRemaining.textContent = '';
        return;
    }

    const now = new Date();
    const diff = nextEventTime - now;
    if (diff <= 0) {
        elRemaining.textContent = 'Now';
        return;
    }
    elRemaining.textContent = `In ${formatRemaining(diff)}`;
}

export function init(el) {
    // Stop any existing interval; resume countdown from dataset if present
    clearRemainingTimer();

    container = el;
    body = el.querySelector('.panel-body') || el;
    if (!body) return;

    // Replace body with a richer layout (icon on its own row; countdown under event)
    body.innerHTML = `
        <div class="sun-wrapper">
            <div class="sun-icon">${sunSVG}</div>
            <div class="sun-event">Loading…</div>
            <div class="sun-remaining">—</div>
            <div class="sun-meta">
                <div class="sun-length sun-day">
                    <div class="sun-label">Day</div>
                    <div class="sun-value sun-day-value">—</div>
                </div>
                <div class="sun-length sun-night">
                    <div class="sun-label">Night</div>
                    <div class="sun-value sun-night-value">—</div>
                </div>
            </div>
        </div>
    `;

    elEvent = body.querySelector('.sun-event');
    elRemaining = body.querySelector('.sun-remaining');
    elDay = body.querySelector('.sun-day-value');
    elNight = body.querySelector('.sun-night-value');
    elIcon = body.querySelector('.sun-icon');

    // Resume countdown if a next event was previously stored on the DOM
    if (body.dataset && body.dataset.nextEventTime) {
        const parsed = new Date(body.dataset.nextEventTime);
        if (!isNaN(parsed) && parsed > new Date()) {
            nextEventTime = parsed;
            updateRemainingDisplay();
            if (!remainingTimer) remainingTimer = setInterval(updateRemainingDisplay, 1000);
        } else {
            // stale, remove
            delete body.dataset.nextEventTime;
            delete body.dataset.nextEventName;
        }
    } else {
        // fallback: try localStorage (survives DOM re-creation)
        try {
            if (typeof localStorage !== 'undefined') {
                const ls = localStorage.getItem('panel-sun-nextEventTime');
                if (ls) {
                    const parsed = new Date(ls);
                    if (!isNaN(parsed) && parsed > new Date()) {
                        nextEventTime = parsed;
                        updateRemainingDisplay();
                        if (!remainingTimer) remainingTimer = setInterval(updateRemainingDisplay, 1000);
                        // also copy to dataset for quick access
                        if (body && body.dataset) body.dataset.nextEventTime = ls;
                    } else {
                        localStorage.removeItem('panel-sun-nextEventTime');
                        localStorage.removeItem('panel-sun-nextEventName');
                    }
                }
            }
        } catch (e) {
            // ignore storage errors
        }
    }
}

export function onMessage(msg) {
    if (!container) return;
    if (msg.serviceType !== 'sun' || (msg.event !== 'sun_check' && msg.event !== 'sun_event')) return;

    const data = msg.data || {};
    // Prefer server-provided 'now' timestamp when available (typed SunEvent payloads include 'now')
    const now = data.now ? new Date(data.now) : new Date();

    // Determine next event from available times - PRIMARY: dawn/dusk, SECONDARY: sunrise/sunset
    const times = [
        {name: 'Dawn', key: 'civil_dawn', secondary: 'Sunrise', secondaryKey: 'sunrise'},
        {name: 'Dusk', key: 'civil_dusk', secondary: 'Sunset', secondaryKey: 'sunset'},
    ];

    // Try to find next event time in msg.data using several common key forms
    function tryParseTs(val) {
        if (!val) return null;
        const d = new Date(val);
        return isNaN(d) ? null : d;
    }

    function camelCase(s) {
        return s.replace(/_([a-z])/g, (m, p1) => p1.toUpperCase());
    }

    function findTimestampForKey(key) {
        const variants = [key, camelCase(key), key.replace(/_/g, ''), key.split('_').pop()];
        for (const v of variants) {
            const ts = data[v];
            const d = tryParseTs(ts);
            if (d) return d;
        }
        return null;
    }

    // Get all times for color coding
    const times_data = {
        sunrise: findTimestampForKey('sunrise'),
        sunset: findTimestampForKey('sunset'),
        dawn: findTimestampForKey('civil_dawn'),
        dusk: findTimestampForKey('civil_dusk'),
    };

    // Determine icon color based on current time
    if (elIcon) {
        elIcon.className = 'sun-icon';  // reset
        if (times_data.sunrise && times_data.sunset) {
            if (now > times_data.sunrise && now < times_data.sunset) {
                // Between sunrise and sunset = daytime (yellow)
                elIcon.classList.add('sun-day-icon');
            } else if ((times_data.sunset && now > times_data.sunset && times_data.dusk && now < times_data.dusk) ||
                (times_data.dawn && now > times_data.dawn && times_data.sunrise && now < times_data.sunrise)) {
                // Between sunset and dusk OR dawn and sunrise = twilight (orange)
                elIcon.classList.add('sun-twilight-icon');
            } else if (times_data.dusk && times_data.dawn && (now > times_data.dusk || now < times_data.dawn)) {
                // Between dusk and dawn = night (purple)
                elIcon.classList.add('sun-night-icon');
            }
        }
    }

    let next = null;
    let secondary = null;
    for (const t of times) {
        const dt = findTimestampForKey(t.key);
        if (dt && dt > now) {
            next = {name: t.name, time: dt};

            // Add secondary time if available and in the future
            const secondaryTime = findTimestampForKey(t.secondaryKey);
            if (secondaryTime && secondaryTime > now) {
                secondary = {name: t.secondary, time: secondaryTime};
            }
            break;
        }
    }

    if (!next) {
        // Try parsing summary text like "Next: Dawn 06:56" or "Dawn 06:56"
        const summary = (msg.summary || msg.text || '').replace(/^Next:\s*/i, '');
        const m = summary.match(/(Dawn|Sunrise|Sunset|Dusk)\s+(\d{1,2}:\d{2})/i);
        if (m) {
            const name = m[1];
            const hm = m[2].split(':');
            const hh = parseInt(hm[0], 10);
            const mm = parseInt(hm[1], 10);
            let candidate = new Date(now);
            candidate.setHours(hh, mm, 0, 0);
            if (candidate <= now) candidate = new Date(candidate.getTime() + 24 * 60 * 60 * 1000);
            next = {name: name, time: candidate};
        }
    }

    if (next) {
        // Show primary event with time, optionally with secondary below
        let eventText = `${next.name} ${formatTime(next.time)}`;
        if (secondary) {
            eventText += `<div style="font-size: 0.75rem; opacity: 0.6; margin-top: 2px;">${secondary.name} ${formatTime(secondary.time)}</div>`;
        }
        if (elEvent) elEvent.innerHTML = eventText;

        // Persist next event on DOM so re-initialization can resume countdown
        // Countdown uses PRIMARY event (next.time), not secondary
        nextEventTime = new Date(next.time);
        if (body && body.dataset) {
            body.dataset.nextEventTime = nextEventTime.toISOString();
            body.dataset.nextEventName = next.name;
        }
        // Also persist to localStorage so the countdown survives DOM re-creation
        try {
            if (typeof localStorage !== 'undefined') {
                localStorage.setItem('panel-sun-nextEventTime', nextEventTime.toISOString());
                localStorage.setItem('panel-sun-nextEventName', next.name);
            }
        } catch (e) {
            // ignore storage errors
        }

        updateRemainingDisplay();
        clearRemainingTimer();
        remainingTimer = setInterval(updateRemainingDisplay, 1000);
    } else {
        // fallback to summary text (strip any leading "Next:" prefix)
        const summary = (msg.summary || msg.text || '').replace(/^Next:\s*/i, '');
        if (elEvent && summary) elEvent.textContent = summary;

        // If there is an existing countdown and it's still in the future, keep it.
        // Also keep it for a short grace window after expiry to avoid flicker.
        const nowCheck = new Date();
        const GRACE_MS = 60 * 1000; // 60 seconds
        if (nextEventTime && (nextEventTime > nowCheck || (nowCheck - nextEventTime) <= GRACE_MS)) {
            // keep the existing timer running; update display immediately
            updateRemainingDisplay();
        } else {
            // otherwise clear remaining display and stop timer
            if (elRemaining) elRemaining.textContent = '';
            clearCountdown();
        }
    }

    // Day length: based on civil_dawn and civil_dusk (if available), fall back to sunrise/sunset
    if (data.civil_dawn && data.civil_dusk) {
        const dawn = new Date(data.civil_dawn);
        const dusk = new Date(data.civil_dusk);
        if (!isNaN(dawn) && !isNaN(dusk)) {
            const dayMs = Math.max(0, dusk - dawn);
            if (elDay) elDay.textContent = formatDuration(dayMs);
        }
    } else if (data.sunrise && data.sunset) {
        const sunrise = new Date(data.sunrise);
        const sunset = new Date(data.sunset);
        if (!isNaN(sunrise) && !isNaN(sunset)) {
            const dayMs = Math.max(0, sunset - sunrise);
            if (elDay) elDay.textContent = formatDuration(dayMs);
        }
    } else if (data.day_length) {
        const dMs = parseDurationString(data.day_length);
        if (!isNaN(dMs) && elDay) elDay.textContent = formatDuration(dMs);
    }

    // Night length: prefer explicit night_length if available; otherwise compute from dusk->next dawn
    if (data.night_length) {
        const nMs = parseDurationString(data.night_length);
        if (!isNaN(nMs) && elNight) elNight.textContent = formatDuration(nMs);
    } else if (data.civil_dusk && data.civil_dawn) {
        const dusk = new Date(data.civil_dusk);
        const dawn = new Date(data.civil_dawn);
        if (!isNaN(dusk) && !isNaN(dawn)) {
            let nextDawn = dawn;
            // If the dawn timestamp is at or before dusk, assume it's the next day's dawn
            if (nextDawn <= dusk) {
                nextDawn = new Date(nextDawn.getTime() + 24 * 60 * 60 * 1000);
            }
            const nightMs = Math.max(0, nextDawn - dusk);
            if (elNight) elNight.textContent = formatDuration(nightMs);
        }
    }
}
