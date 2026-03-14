/**
 * Format an ISO8601 timestamp as elapsed time with two significant units (plain text version)
 * Useful for tabs that don't need HTML wrapping
 * @param {string} isoString - ISO8601 formatted timestamp
 * @returns {string} Elapsed time string (e.g., "2h 30m", "3d 5h")
 */
export function formatAge(isoString) {
    if (!isoString) {
        return 'never';
    }

    const timestamp = new Date(isoString);
    const now = new Date();
    const elapsedMs = now - timestamp;

    if (elapsedMs < 0) {
        return 'in the future';
    }

    // Calculate time units
    const seconds = Math.floor(elapsedMs / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);
    const weeks = Math.floor(days / 7);
    const months = Math.floor(days / 30);
    const years = Math.floor(days / 365);

    // Build the two most significant time units, or just one if less than a minute
    if (years > 0) {
        return `${years}y ${months % 12}m ago`;
    } else if (months > 0) {
        return `${months}m ${days % 30}d ago`;
    } else if (weeks > 0) {
        return `${weeks}w ${days % 7}d ago`;
    } else if (days > 0) {
        return `${days}d ${hours % 24}h ago`;
    } else if (hours > 0) {
        return `${hours}h ${minutes % 60}m ago`;
    } else if (minutes > 0) {
        return `${minutes}m ago`;
    } else {
        return 'just now';
    }
}

/**
 * Format an ISO8601 timestamp as elapsed time with two significant units
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

    if (elapsedMs < 0) {
        return 'in the future';
    }

    // Calculate time units
    const seconds = Math.floor(elapsedMs / 1000);
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
        elapsedText = '0m';
    }

    const fullDate = timestamp.toLocaleString();

    return `<span class="elapsed-time" title="${fullDate}" data-timestamp="${isoString}">${elapsedText}</span>`;
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

            if (elapsedMs < 0) {
                span.textContent = 'in the future';
                return;
            }

            // Calculate time units
            const seconds = Math.floor(elapsedMs / 1000);
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

            span.textContent = elapsedText;
        });
    }

    // Update immediately and then every second
    updateAllTimestamps();
    setInterval(updateAllTimestamps, 1000);
}

