/**
 * Format an ISO8601 timestamp as elapsed time with two significant units (plain text version)
 * Works for both past and future times
 * @param {string} isoString - ISO8601 formatted timestamp
 * @returns {string} Elapsed time string (e.g., "2h 30m ago", "3d 5h")
 */
export function formatAge(isoString) {
    if (!isoString) {
        return 'never';
    }

    const timestamp = new Date(isoString);
    const now = new Date();
    const elapsedMs = now - timestamp;
    const isPast = elapsedMs >= 0;
    const absElapsedMs = Math.abs(elapsedMs);

    // Calculate time units
    const seconds = Math.floor(absElapsedMs / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    const weeks = Math.floor(days / 7);
    const months = Math.floor(days / 30);
    const years = Math.floor(days / 365);

    // Build the two most significant time units, or just one if less than a minute
    let timeStr;
    if (years > 0) {
        timeStr = `${years}y ${months % 12}m`;
    } else if (months > 0) {
        timeStr = `${months}m ${days % 30}d`;
    } else if (weeks > 0) {
        timeStr = `${weeks}w ${days % 7}d`;
    } else if (days > 0) {
        timeStr = `${days}d ${hours % 24}h`;
    } else if (hours > 0) {
        timeStr = `${hours}h ${minutes % 60}m`;
    } else if (minutes > 0) {
        timeStr = `${minutes}m`;
    } else {
        return isPast ? 'just now' : 'in seconds';
    }

    return isPast ? `${timeStr} ago` : `in ${timeStr}`;
}

/**
 * Format an ISO8601 timestamp as elapsed time with two significant units
 * Works for both past and future times
 * @param {string} isoString - ISO8601 formatted timestamp
 * @returns {string} HTML span with elapsed time and full date tooltip
 */
export function formatElapsedTime(isoString) {
    if (!isoString) {
        return 'never';
    }

    const timestamp = new Date(isoString);
    const now = new Date();
    const elapsedMs = now - timestamp;
    const isPast = elapsedMs >= 0;
    const absElapsedMs = Math.abs(elapsedMs);

    // Calculate time units
    const seconds = Math.floor(absElapsedMs / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    const weeks = Math.floor(days / 7);
    const months = Math.floor(days / 30);
    const years = Math.floor(days / 365);

    let elapsedText;

    // Build the two most significant time units, or just one if less than a minute
    if (years > 0) {
        elapsedText = `${years}y ${months % 12}m`;
    } else if (months > 0) {
        elapsedText = `${months}m ${days % 30}d`;
    } else if (weeks > 0) {
        elapsedText = `${weeks}w ${days % 7}d`;
    } else if (days > 0) {
        elapsedText = `${days}d ${hours % 24}h`;
    } else if (hours > 0) {
        elapsedText = `${hours}h ${minutes % 60}m`;
    } else if (minutes > 0) {
        elapsedText = `${minutes}m`;
    } else {
        elapsedText = isPast ? '0m' : '0m';
    }

    const fullDate = timestamp.toLocaleString();
    const suffix = isPast ? 'ago' : 'from now';

    return `<span class="elapsed-time" title="${fullDate}" data-timestamp="${isoString}">${elapsedText} ${suffix}</span>`;
}

/**
 * Start updating all elapsed times on the page
 * Refreshes every second to show real-time elapsed time
 */
export function startElapsedTimeUpdates() {
    function updateAllTimestamps() {
        const spans = document.querySelectorAll('.elapsed-time[data-timestamp]');
        spans.forEach(span => {
            const isoString = span.getAttribute('data-timestamp');
            if (!isoString) return;

            const timestamp = new Date(isoString);
            const now = new Date();
            const elapsedMs = now - timestamp;
            const isPast = elapsedMs >= 0;
            const absElapsedMs = Math.abs(elapsedMs);

            // Calculate time units
            const seconds = Math.floor(absElapsedMs / 1000);
            const minutes = Math.floor(seconds / 60);
            const hours = Math.floor(minutes / 60);
            const days = Math.floor(hours / 24);
            const weeks = Math.floor(days / 7);
            const months = Math.floor(days / 30);
            const years = Math.floor(days / 365);

            let elapsedText;

            if (years > 0) {
                elapsedText = `${years}y ${months % 12}m`;
            } else if (months > 0) {
                elapsedText = `${months}m ${days % 30}d`;
            } else if (weeks > 0) {
                elapsedText = `${weeks}w ${days % 7}d`;
            } else if (days > 0) {
                elapsedText = `${days}d ${hours % 24}h`;
            } else if (hours > 0) {
                elapsedText = `${hours}h ${minutes % 60}m`;
            } else if (minutes > 0) {
                elapsedText = `${minutes}m`;
            } else {
                elapsedText = '0m';
            }

            const suffix = isPast ? 'ago' : 'from now';
            span.textContent = `${elapsedText} ${suffix}`;
        });
    }

    // Update immediately and then every second
    updateAllTimestamps();
    setInterval(updateAllTimestamps, 1000);
}

