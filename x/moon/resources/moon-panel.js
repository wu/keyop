let container = null;
let body = null;
let elPhase = null;
let elPhaseValue = null;
let elIllum = null;
let elRemaining = null;
let remainingTimer = null;
let nextEventTime = null;
let nextEventName = null;

function clamp(v, lo, hi) {
    return Math.max(lo, Math.min(hi, v));
}

function phaseToFile(name) {
    if (!name) return 'new.jpg';
    // Normalize: lower-case, replace non-alnum with hyphen, trim
    let n = String(name).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    // Common aliases
    const aliases = {
        'new-moon': 'new.jpg',
        'new': 'new.jpg',
        'full-moon': 'full.jpg',
        'full': 'full.jpg',
        'first-quarter': 'first-quarter.jpg',
        'last-quarter': 'last-quarter.jpg',
        'last quarter': 'last-quarter.jpg'
    };
    if (aliases[n]) return aliases[n];
    // If it's one of the waxing/waning forms, map directly
    if (['waxing-crescent', 'waxing-gibbous', 'waning-gibbous', 'waning-crescent'].includes(n)) return n + '.jpg';
    // fallback: try using normalized name as filename
    return n + '.jpg';
}

function updateImage(name) {
    if (!body) return;
    try {
        const file = phaseToFile(name);
        const img = body.querySelector('.moon-image');
        if (img) {
            img.src = '/images/moon/' + file;
            img.alt = name || 'Moon';
        }
    } catch (e) {
        // ignore
    }
}

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

function parseDurationLike(s) {
    // Accept "3d 4h", "4h", "5h 30m" (minutes ignored for short accuracy)
    if (!s || typeof s !== 'string') return NaN;
    let days = 0, hours = 0, minutes = 0;
    const dMatch = s.match(/(\d+)d/);
    const hMatch = s.match(/(\d+)h/);
    const mMatch = s.match(/(\d+)m/);
    if (dMatch) days = parseInt(dMatch[1], 10);
    if (hMatch) hours = parseInt(hMatch[1], 10);
    if (mMatch) minutes = parseInt(mMatch[1], 10);
    return ((days * 24 + hours) * 3600 + minutes * 60) * 1000;
}

function computeIlluminationFromPhase(phase) {
    // phase expected ~0..28 where 0=new,14=full. Convert to fraction 0..1
    if (phase == null || isNaN(phase)) return NaN;
    const f = ((phase % 28) + 28) % 28 / 28; // 0..1
    // approximate illumination curve
    const illum = (1 - Math.cos(2 * Math.PI * f)) / 2;
    return Math.round(clamp(illum, 0, 1) * 100);
}

function clearRemainingTimer() {
    if (remainingTimer) {
        clearInterval(remainingTimer);
        remainingTimer = null;
    }
}

function updateRemainingDisplay() {
    if (!elRemaining) return;
    if (!nextEventTime && body && body.dataset && body.dataset.nextEventTime) {
        const parsed = new Date(body.dataset.nextEventTime);
        if (!isNaN(parsed) && parsed > new Date()) {
            nextEventTime = parsed;
            nextEventName = body.dataset.nextEventName || null;
        } else {
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
    elRemaining.textContent = `${nextEventName ? nextEventName + ' in ' : 'In '}${formatRemaining(diff)}`;
}

export function init(el) {
    clearRemainingTimer();
    container = el;
    body = el.querySelector('.panel-body') || el;
    if (!body) return;

    // Use the same structural classes as the sun panel to keep visual consistency
    body.innerHTML = `
        <div class="sun-wrapper">
            <div class="sun-icon"><img class="moon-image" src="/images/moon/new.jpg" alt="Moon phase"></div>
            <div class="sun-event">Loading…</div>
            <div class="sun-remaining">—</div>
            <div class="sun-meta">
                <div class="sun-length sun-day">
                    <div class="sun-label">Phase</div>
                    <div class="sun-value sun-day-value">—</div>
                </div>
                <div class="sun-length sun-night">
                    <div class="sun-label">Illumination</div>
                    <div class="sun-value sun-night-value">—</div>
                </div>
            </div>
        </div>
    `;

    elPhase = body.querySelector('.sun-event');
    elIllum = body.querySelector('.sun-night-value');
    elRemaining = body.querySelector('.sun-remaining');
    elPhaseValue = body.querySelector('.sun-day-value');

    // resume next event from localStorage or dataset
    if (body.dataset && body.dataset.nextEventTime) {
        const p = new Date(body.dataset.nextEventTime);
        if (!isNaN(p) && p > new Date()) {
            nextEventTime = p;
            nextEventName = body.dataset.nextEventName || null;
        } else {
            delete body.dataset.nextEventTime;
            delete body.dataset.nextEventName;
        }
    } else {
        try {
            if (typeof localStorage !== 'undefined') {
                const ls = localStorage.getItem('panel-moon-nextEventTime');
                if (ls) {
                    const p = new Date(ls);
                    if (!isNaN(p) && p > new Date()) {
                        nextEventTime = p;
                        nextEventName = localStorage.getItem('panel-moon-nextEventName') || null;
                        if (body && body.dataset) body.dataset.nextEventTime = ls;
                    } else {
                        localStorage.removeItem('panel-moon-nextEventTime');
                        localStorage.removeItem('panel-moon-nextEventName');
                    }
                }
            }
        } catch (e) {
        }
    }

    if (nextEventTime) {
        updateRemainingDisplay();
        if (!remainingTimer) remainingTimer = setInterval(updateRemainingDisplay, 1000);
    }
}

export function onMessage(msg) {
    if (!container) return;
    if (msg.serviceType !== 'moon') return;

    const data = msg.data || {};

    if (msg.event === 'moon_phase' || msg.event === 'moon_phase_change') {
        // phase name
        const name = data.name || (msg.summary || msg.text || '').replace(/^Moon[:\s]*/i, '') || 'Moon';
        if (elPhase) elPhase.textContent = `${name}`;
        if (elPhaseValue) elPhaseValue.textContent = `${name}`;
        updateImage(name);

        // illumination: prefer server-provided illumination if present
        if (data.illumination !== undefined) {
            const illum = Number(data.illumination);
            if (!isNaN(illum) && elIllum) elIllum.textContent = `${illum}% illuminated`;
        } else if (data.phase !== undefined) {
            const p = Number(data.phase);
            const illum = computeIlluminationFromPhase(p);
            if (!isNaN(illum) && elIllum) elIllum.textContent = `${illum}% illuminated`;
        } else if (elIllum) {
            elIllum.textContent = '';
        }

        // If server sent an absolute next event time, use it
        if (data.next_major_time) {
            const t = new Date(data.next_major_time);
            if (!isNaN(t)) {
                nextEventTime = t;
                nextEventName = data.next_major_name || null;
                if (body && body.dataset) {
                    body.dataset.nextEventTime = t.toISOString();
                    body.dataset.nextEventName = nextEventName || '';
                }
                try {
                    if (typeof localStorage !== 'undefined') {
                        localStorage.setItem('panel-moon-nextEventTime', t.toISOString());
                        localStorage.setItem('panel-moon-nextEventName', nextEventName || '');
                    }
                } catch (e) {
                }
                updateRemainingDisplay();
                clearRemainingTimer();
                remainingTimer = setInterval(updateRemainingDisplay, 1000);
            }
        }
    }

    // If message text contains "Next <Type> in <duration>", set countdown
    const text = msg.text || msg.summary || '';
    const m = text.match(/Next\s+(New Moon|Full Moon)\s+in\s+(.+)$/i);
    if (m) {
        const nextName = m[1];
        const durStr = m[2];
        const ms = parseDurationLike(durStr);
        if (!isNaN(ms)) {
            const fut = new Date(Date.now() + ms);
            nextEventTime = fut;
            nextEventName = nextName;
            if (body && body.dataset) {
                body.dataset.nextEventTime = fut.toISOString();
                body.dataset.nextEventName = nextName;
            }
            try {
                if (typeof localStorage !== 'undefined') {
                    localStorage.setItem('panel-moon-nextEventTime', fut.toISOString());
                    localStorage.setItem('panel-moon-nextEventName', nextName);
                }
            } catch (e) {
            }
            updateRemainingDisplay();
            clearRemainingTimer();
            remainingTimer = setInterval(updateRemainingDisplay, 1000);
        }
    }
}
