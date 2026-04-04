let container = null;
let body = null;
let elPhase = null;
let elPhaseValue = null;
let elIllum = null;
let elFullMoonRemaining = null;
let elNewMoonRemaining = null;
let remainingTimer = null;
let nextFullMoonTime = null;
let nextNewMoonTime = null;

function clamp(v, lo, hi) {
    return Math.max(lo, Math.min(hi, v));
}

function phaseToFile(name) {
    if (!name) return 'new.png';
    // Normalize: lower-case, replace non-alnum with hyphen, trim
    let n = String(name).toLowerCase().replace(/[^a-z0-9]+/g, '-').replace(/^-+|-+$/g, '');
    // Common aliases
    const aliases = {
        'new-moon': 'new.png',
        'new': 'new.png',
        'full-moon': 'full.png',
        'full': 'full.png',
        'first-quarter': 'first-quarter.png',
        'last-quarter': 'last-quarter.png',
        'last quarter': 'last-quarter.png'
    };
    if (aliases[n]) return aliases[n];
    // If it's one of the waxing/waning forms, map directly
    if (['waxing-crescent', 'waxing-gibbous', 'waning-gibbous', 'waning-crescent'].includes(n)) return n + '.png';
    // fallback: try using normalized name as filename
    return n + '.png';
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
    const now = new Date();

    // Update full moon timer
    if (elFullMoonRemaining && nextFullMoonTime) {
        const diff = nextFullMoonTime - now;
        if (diff > 0) {
            elFullMoonRemaining.textContent = formatRemaining(diff);
        } else {
            elFullMoonRemaining.textContent = 'Now';
        }
    }

    // Update new moon timer
    if (elNewMoonRemaining && nextNewMoonTime) {
        const diff = nextNewMoonTime - now;
        if (diff > 0) {
            elNewMoonRemaining.textContent = formatRemaining(diff);
        } else {
            elNewMoonRemaining.textContent = 'Now';
        }
    }
}

export function init(el) {
    clearRemainingTimer();
    container = el;
    body = el.querySelector('.panel-body') || el;
    if (!body) return;

    // Use the same structural classes as the sun panel to keep visual consistency
    body.innerHTML = `
        <div class="sun-wrapper">
            <div class="sun-icon"><img class="moon-image" src="/images/moon/new.png" alt="Moon phase"></div>
            <div class="sun-event">Loading…</div>
            <div class="sun-meta" style="flex-direction: column; gap: 12px; text-align: center;">
                <div style="display: flex; flex-direction: column; gap: 4px; align-items: center;">
                    <div class="sun-label">Illumination</div>
                    <div class="sun-value sun-night-value">—</div>
                </div>
                <div style="display: flex; flex-direction: column; gap: 4px; align-items: center;">
                    <div class="sun-label">Full Moon</div>
                    <div class="sun-value moon-full-remaining">—</div>
                </div>
                <div style="display: flex; flex-direction: column; gap: 4px; align-items: center;">
                    <div class="sun-label">New Moon</div>
                    <div class="sun-value moon-new-remaining">—</div>
                </div>
            </div>
        </div>
    `;

    elPhase = body.querySelector('.sun-event');
    elIllum = body.querySelector('.sun-night-value');
    elPhaseValue = null;
    elFullMoonRemaining = body.querySelector('.moon-full-remaining');
    elNewMoonRemaining = body.querySelector('.moon-new-remaining');

    // Resume next events from localStorage
    try {
        if (typeof localStorage !== 'undefined') {
            const fullLS = localStorage.getItem('panel-moon-nextFullTime');
            if (fullLS) {
                const p = new Date(fullLS);
                if (!isNaN(p) && p > new Date()) {
                    nextFullMoonTime = p;
                    if (body && body.dataset) body.dataset.nextFullTime = fullLS;
                } else {
                    localStorage.removeItem('panel-moon-nextFullTime');
                }
            }

            const newLS = localStorage.getItem('panel-moon-nextNewTime');
            if (newLS) {
                const p = new Date(newLS);
                if (!isNaN(p) && p > new Date()) {
                    nextNewMoonTime = p;
                    if (body && body.dataset) body.dataset.nextNewTime = newLS;
                } else {
                    localStorage.removeItem('panel-moon-nextNewTime');
                }
            }
        }
    } catch (e) {
    }

    if (nextFullMoonTime || nextNewMoonTime) {
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
        updateImage(name);

        // illumination: prefer server-provided illumination if present
        if (data.illumination !== undefined) {
            const illum = Number(data.illumination);
            if (!isNaN(illum) && elIllum) elIllum.textContent = `${illum}%`;
        } else if (data.phase !== undefined) {
            const p = Number(data.phase);
            const illum = computeIlluminationFromPhase(p);
            if (!isNaN(illum) && elIllum) elIllum.textContent = `${illum}%`;
        } else if (elIllum) {
            elIllum.textContent = '';
        }

        // Handle next_full time from server
        if (data.next_full) {
            const t = new Date(data.next_full);
            if (!isNaN(t) && t > new Date()) {
                nextFullMoonTime = t;
                if (body && body.dataset) {
                    body.dataset.nextFullTime = t.toISOString();
                }
                try {
                    if (typeof localStorage !== 'undefined') {
                        localStorage.setItem('panel-moon-nextFullTime', t.toISOString());
                    }
                } catch (e) {
                }
            }
        }

        // Handle next_new time from server
        if (data.next_new) {
            const t = new Date(data.next_new);
            if (!isNaN(t) && t > new Date()) {
                nextNewMoonTime = t;
                if (body && body.dataset) {
                    body.dataset.nextNewTime = t.toISOString();
                }
                try {
                    if (typeof localStorage !== 'undefined') {
                        localStorage.setItem('panel-moon-nextNewTime', t.toISOString());
                    }
                } catch (e) {
                }
            }
        }

        updateRemainingDisplay();
        clearRemainingTimer();
        remainingTimer = setInterval(updateRemainingDisplay, 1000);
    }
}
