let searchTimeout;
let currentFilters = {
    types: [],
    tags: []
};
let currentPage = 0;
let lastTotal = 0;
const RESULTS_PER_PAGE = 20;

// Remove HTML marks and ellipsis, compress whitespace
function cleanSnippet(text) {
    if (!text) return '';

    // Remove <mark> tags but keep the content
    text = text.replace(/<\/?mark>/g, '');

    // Keep the ellipsis between fragments (…) but remove leading/trailing dots
    text = text.replace(/^\.\s+/, '').replace(/\s+\.$/, '');

    // Compress excessive whitespace but keep line breaks as spaces
    text = text.replace(/\s+/g, ' ').trim();

    // Limit to 400 characters for better context
    if (text.length > 400) {
        text = text.substring(0, 400) + '…';
    }

    return text;
}

// Format date as relative age (e.g., "5d 4h", "2h 30m")
function formatRelativeAge(date) {
    const now = new Date();
    const diffMs = now - date;
    const diffSecs = Math.floor(diffMs / 1000);

    if (diffSecs < 60) return 'now';

    const mins = Math.floor(diffSecs / 60);
    const hours = Math.floor(mins / 60);
    const days = Math.floor(hours / 24);
    const weeks = Math.floor(days / 7);
    const months = Math.floor(days / 30);
    const years = Math.floor(days / 365);

    // Return two most significant portions
    if (years > 0) return `${years}y ${months % 12}mo`;
    if (months > 0) return `${months}mo ${days % 30}d`;
    if (weeks > 0) return `${weeks}w ${days % 7}d`;
    if (days > 0) return `${days}d ${hours % 24}h`;
    if (hours > 0) return `${hours}h ${mins % 60}m`;
    return `${mins}m`;
}

// Simple markdown to HTML converter (basic implementation)
function markdownToHtml(text) {
    if (!text) return '';

    // Escape HTML first
    let html = text
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');

    // Convert markdown syntax
    // Bold: **text** or __text__
    html = html.replace(/\*\*(.+?)\*\*/g, '<strong>$1</strong>');
    html = html.replace(/__(.+?)__/g, '<strong>$1</strong>');

    // Italic: *text* or _text_
    html = html.replace(/\*(.+?)\*/g, '<em>$1</em>');
    html = html.replace(/_(.+?)_/g, '<em>$1</em>');

    // Code: `text`
    html = html.replace(/`(.+?)`/g, '<code>$1</code>');

    // Links: [text](url)
    html = html.replace(/\[(.+?)\]\((.+?)\)/g, '<a href="$2" target="_blank">$1</a>');

    // Line breaks
    html = html.replace(/\n/g, '<br>');

    return html;
}

// Navigate to a source item by switching to its tab and passing the item ID
function navigateToSource(sourceType, sourceID) {
    console.log(`Navigating to ${sourceType}:${sourceID}`);

    // Map source types to tab IDs
    const tabMap = {
        'notes': 'notes',
        'links': 'links',
        'tasks': 'tasks',
        'journal': 'journal',
        'rss': 'rss'
    };

    const tabId = tabMap[sourceType];
    if (!tabId) {
        console.warn(`Don't know how to navigate to ${sourceType}`);
        return;
    }

    // Find and click the tab link to switch tabs
    const tabLink = document.querySelector(`.tab-link[data-tab-id="${tabId}"]`);
    if (tabLink) {
        console.log(`Clicking tab link for ${tabId}`);
        tabLink.click();

        // Dispatch event to the tab's content container so it can navigate to the item
        setTimeout(() => {
            console.log(`Dispatching navigate-to-item event for ${sourceID} to tab ${tabId}`);
            const contentDiv = document.getElementById(`tab-content-${tabId}`);
            if (contentDiv) {
                const event = new CustomEvent('navigate-to-item', {
                    detail: {itemId: sourceID, sourceType: sourceType},
                    bubbles: true
                });
                contentDiv.dispatchEvent(event);
            } else {
                console.warn(`Could not find content div for tab ${tabId}`);
            }
        }, 100);
    } else {
        console.warn(`Could not find tab link for ${tabId}`);
    }
}

async function performSearch(page = 0) {
    const query = document.getElementById('search-query').value.trim();

    // If search is empty, show no results and restore original source counts
    if (!query) {
        const resultsDiv = document.getElementById('search-results');
        const emptyDiv = document.getElementById('search-empty');
        resultsDiv.innerHTML = '';
        emptyDiv.style.display = 'block';
        emptyDiv.textContent = 'Enter a search query to begin';

        // Restore sidebar to show total counts
        const sourcesDiv = document.getElementById('search-sources');
        if (sourcesDiv) {
            sourcesDiv.querySelectorAll('.source-filter').forEach(filter => {
                const source = filter.dataset.source;
                const count = allSourceCounts[source] || 0;
                const countSpan = filter.querySelector('.source-count');
                if (countSpan) {
                    countSpan.textContent = count;
                }
            });
        }
        return;
    }

    currentPage = page;
    const from = page * RESULTS_PER_PAGE;

    try {
        // Fetch unfiltered search results to get source counts for this search
        const unfilteredResponse = await fetch('/api/tabs/search/action/search', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                q: query,
                types: [],  // No type filter
                tags: [],   // No tag filter
                from: 0,
                size: 1000  // Get enough to count all sources
            })
        });

        if (unfilteredResponse.ok) {
            const unfilteredData = await unfilteredResponse.json();
            lastTotal = unfilteredData.total || 0;

            // Update sidebar with search result counts
            const searchSourceCounts = {};
            (unfilteredData.results || []).forEach(r => {
                const source = r.sourceType || 'unknown';
                searchSourceCounts[source] = (searchSourceCounts[source] || 0) + 1;
            });

            // Update sidebar with these counts
            const sourcesDiv = document.getElementById('search-sources');
            if (sourcesDiv) {
                sourcesDiv.querySelectorAll('.source-filter').forEach(filter => {
                    const source = filter.dataset.source;
                    const count = searchSourceCounts[source] || 0;
                    const countSpan = filter.querySelector('.source-count');
                    if (countSpan) {
                        countSpan.textContent = count;
                    }
                });
            }
        }

        // Now fetch with actual filters for display
        const response = await fetch('/api/tabs/search/action/search', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                q: query,
                types: currentFilters.types,
                tags: currentFilters.tags,
                from: from,
                size: RESULTS_PER_PAGE
            })
        });

        if (!response.ok) {
            console.error('Search failed:', response.statusText);
            return;
        }

        const data = await response.json();
        renderResults(data.results || [], data.total || 0);
    } catch (err) {
        console.error('Search error:', err);
        document.getElementById('search-results').innerHTML = `<div style="color: red;">Error: ${err.message}</div>`;
    }
}

function renderResults(results, total) {
    const resultsDiv = document.getElementById('search-results');
    const emptyDiv = document.getElementById('search-empty');

    if (!results || results.length === 0) {
        resultsDiv.innerHTML = '';
        emptyDiv.style.display = 'block';
        return;
    }

    emptyDiv.style.display = 'none';

    // Calculate counts by source type for the displayed (filtered) results summary
    const countBySource = {};
    results.forEach(r => {
        const source = r.sourceType || 'unknown';
        countBySource[source] = (countBySource[source] || 0) + 1;
    });

    // Build pagination info
    const pageStart = currentPage * RESULTS_PER_PAGE + 1;
    const pageEnd = pageStart + results.length - 1;
    const hasNextPage = pageEnd < total;
    const hasPrevPage = currentPage > 0;

    // Build pagination buttons
    let paginationHtml = `<div class="search-pagination">
        <div class="pagination-info">Results ${pageStart}–${pageEnd} of ${total}</div>
        <div class="pagination-buttons">`;

    if (hasPrevPage) {
        paginationHtml += `<button class="pagination-btn">← Previous</button>`;
    }

    if (hasNextPage) {
        paginationHtml += `<button class="pagination-btn">Next →</button>`;
    }

    paginationHtml += `</div></div>`;

    // Build summary text showing displayed results vs total
    const summaryParts = [];
    for (const [source, count] of Object.entries(countBySource)) {
        summaryParts.push(`${count} ${source}`);
    }
    const filteredCount = results.length;
    const summary = `<div class="search-results-summary">Showing ${filteredCount} of ${total} total results (${summaryParts.join(', ')})</div>`;

    console.log('renderResults: got', results.length, 'results, total:', total);
    if (results.length > 0) {
        console.log('First result:', results[0]);
    }
    resultsDiv.innerHTML = summary + results.map((result, index) => {
        const title = escapeHtml(result.title || '(no title)');
        const sourceType = escapeHtml(result.sourceType || '(no source)');

        const tags = result.tags && result.tags.length > 0
            ? `<div class="result-tags">${result.tags.map(tag =>
                `<span class="result-tag">${escapeHtml(tag)}</span>`
            ).join('')}</div>`
            : '';

        // Format date as relative age (e.g., "5d 4h")
        let dateStr = '';
        try {
            const date = new Date(result.updatedAt);
            if (!isNaN(date.getTime())) {
                dateStr = formatRelativeAge(date);
            }
        } catch (e) {
            // If date parsing fails, just use empty string
        }

        // Clean and convert snippet markdown to HTML
        const cleanedSnippet = cleanSnippet(result.snippet || '');
        const snippetHtml = markdownToHtml(cleanedSnippet);

        // Add URL link for clickable navigation (styled subtly)
        let urlDisplay = '';
        if (result.url && sourceType === 'links') {
            urlDisplay = `<div class="search-result-url"><a href="${escapeHtml(result.url)}" target="_blank" rel="noopener noreferrer" class="result-url-link">${escapeHtml(result.url)}</a></div>`;
        }

        const html = `
            <div class="search-result" data-result-index="${index}" data-source-type="${sourceType}" data-source-id="${escapeHtml(result.sourceID)}" data-url="${escapeHtml(result.url || '')}">
                <div class="search-result-header">
                    <div class="search-result-title">${title}</div>
                    <span class="source-badge">${sourceType}</span>
                </div>
                <div class="search-result-meta">
                    ${dateStr ? `<span class="result-date">${dateStr}</span>` : ''}
                </div>
                ${tags}
                ${urlDisplay}
                <div class="search-result-snippet">${snippetHtml}</div>
            </div>
        `;
        console.log(`Result ${index}: title="${title}", sourceType="${sourceType}", url="${result.url || 'none'}"`);
        return html;
    }).join('') + paginationHtml;

    // Add click handlers to result divs
    resultsDiv.querySelectorAll('.search-result').forEach(resultDiv => {
        const sourceType = resultDiv.dataset.sourceType;
        const sourceId = resultDiv.dataset.sourceId;
        const url = resultDiv.dataset.url;

        // For links: make the whole result clickable to go to links tab
        // The URL link inside will be handled by its own href
        if (sourceType === 'links') {
            resultDiv.style.cursor = 'pointer';
            resultDiv.addEventListener('click', (e) => {
                // Don't navigate if they clicked the URL link
                if (e.target.classList.contains('result-url-link')) {
                    return;
                }
                e.stopPropagation();
                navigateToSource(sourceType, sourceId);
            });
        } else {
            // For other types, make the title clickable to navigate
            const titleDiv = resultDiv.querySelector('.search-result-title');
            if (titleDiv) {
                titleDiv.style.cursor = 'pointer';
                titleDiv.addEventListener('click', (e) => {
                    e.stopPropagation();
                    navigateToSource(sourceType, sourceId);
                });
            }
        }
    });

    // Add click handlers to pagination buttons
    resultsDiv.querySelectorAll('.pagination-btn').forEach(btn => {
        if (btn.textContent.includes('Previous')) {
            btn.addEventListener('click', () => performSearch(currentPage - 1));
        } else if (btn.textContent.includes('Next')) {
            btn.addEventListener('click', () => performSearch(currentPage + 1));
        }
    });
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function updateIndexStatus(docCount, providersCount) {
    const statsDiv = document.getElementById('search-stats');
    if (!statsDiv) return;

    if (docCount === 0 && providersCount === 0) {
        statsDiv.textContent = '⚠️ No search service configured';
        statsDiv.classList.remove('loading');
    } else if (docCount === 0 && providersCount > 0) {
        statsDiv.textContent = '📋 Building index...';
        statsDiv.classList.add('loading');
    } else {
        statsDiv.textContent = `📊 ${docCount} doc${docCount !== 1 ? 's' : ''} | ${providersCount} source${providersCount !== 1 ? 's' : ''}`;
        statsDiv.classList.remove('loading');
    }
}

function setupSearchInput() {
    const queryInput = document.getElementById('search-query');
    const clearBtn = document.getElementById('search-clear-btn');
    if (!queryInput) return;

    // Update clear button visibility
    const updateClearButton = () => {
        if (queryInput.value.trim()) {
            clearBtn.classList.add('visible');
        } else {
            clearBtn.classList.remove('visible');
        }
    };

    queryInput.addEventListener('input', () => {
        updateClearButton();
        clearTimeout(searchTimeout);
        searchTimeout = setTimeout(performSearch, 300);
    });

    queryInput.addEventListener('keydown', (e) => {
        if (e.key === 'Enter') {
            clearTimeout(searchTimeout);
            performSearch();
        }
    });

    // Clear button click handler
    if (clearBtn) {
        clearBtn.addEventListener('click', () => {
            queryInput.value = '';
            updateClearButton();
            clearTimeout(searchTimeout);
            performSearch();
        });
    }

    // Set up help toggle button
    const helpToggle = document.getElementById('search-help-toggle');
    const helpDiv = document.getElementById('search-help');
    if (helpToggle && helpDiv) {
        helpToggle.addEventListener('click', () => {
            const isOpen = helpDiv.style.display !== 'none';
            helpDiv.style.display = isOpen ? 'none' : 'block';
            helpToggle.textContent = isOpen ? '? Help' : '✕ Help';
        });
    }
}

async function handleReindex() {
    const btn = document.getElementById('reindex-btn');
    if (!btn) return;

    btn.disabled = true;
    btn.textContent = '⏳ Reindexing...';

    try {
        const response = await fetch('/api/tabs/search/action/reindex', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
        });

        if (response.ok) {
            // Wait a bit for the reindex to start, then check status
            setTimeout(async () => {
                await checkIndexStatus();
                btn.disabled = false;
                btn.textContent = '🔄 Reindex';
            }, 1000);
        } else {
            btn.disabled = false;
            btn.textContent = '🔄 Reindex';
            console.error('Reindex failed');
        }
    } catch (err) {
        btn.disabled = false;
        btn.textContent = '🔄 Reindex';
        console.error('Failed to reindex:', err);
    }
}

async function checkIndexStatus() {
    console.log('checkIndexStatus called');
    try {
        console.log('Fetching index status...');
        const statusResponse = await fetch('/api/tabs/search/action/get-index-status', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
        });

        console.log('Status response ok?', statusResponse.ok);
        if (!statusResponse.ok) {
            console.error('Failed to get index status:', statusResponse.statusText);
            const statusDiv = document.getElementById('search-stats');
            if (statusDiv) {
                statusDiv.textContent = '❌ Failed to load index status';
                statusDiv.classList.remove('loading');
            }
            return;
        }

        const statusData = await statusResponse.json();
        console.log('Index status received:', statusData);

        // Populate source counts for sidebar display
        if (statusData.sourceCounts) {
            allSourceCounts = statusData.sourceCounts;
            console.log('Loaded source counts:', allSourceCounts);
        }

        updateIndexStatus(statusData.docCount || 0, statusData.providersCount || 0);
    } catch (err) {
        console.error('Failed to check index status:', err);
        const statusDiv = document.getElementById('search-stats');
        if (statusDiv) {
            statusDiv.textContent = '❌ Error: ' + err.message;
            statusDiv.classList.remove('loading');
        }
    }
}

export async function init(container) {
    console.log('Search tab init called');

    // Set up reindex button
    const reindexBtn = document.getElementById('reindex-btn');
    if (reindexBtn) {
        reindexBtn.addEventListener('click', handleReindex);
    }

    setupSearchInput();
    console.log('About to check index status...');
    await checkIndexStatus();
    console.log('Index status check completed');
    // Fetch available sources for filtering
    try {
        const response = await fetch('/api/tabs/search/action/get-sources', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
        });

        if (response.ok) {
            const data = await response.json();
            renderSourceFilters(data.sources || []);
        }
    } catch (err) {
        console.error('Failed to load sources:', err);
    }
}

let sourceCountsCache = {};
let allSourceCounts = {};  // Counts from unfiltered results

function renderSourceFilters(sources) {
    const sourcesDiv = document.getElementById('search-sources');
    if (!sources || sources.length === 0) return;

    sourcesDiv.innerHTML = sources.map(source => {
        const count = allSourceCounts[source] || 0;
        return `<div class="source-filter" data-source="${escapeHtml(source)}">
            <span>${escapeHtml(source)}</span>
            <span class="source-count">${count}</span>
        </div>`;
    }).join('');

    sourcesDiv.querySelectorAll('.source-filter').forEach(filter => {
        filter.addEventListener('click', () => {
            const source = filter.dataset.source;
            if (currentFilters.types.includes(source)) {
                currentFilters.types = currentFilters.types.filter(t => t !== source);
                filter.classList.remove('active');
            } else {
                currentFilters.types.push(source);
                filter.classList.add('active');
            }
            performSearch();
        });
    });
}

function updateSourceCounts(results) {
    // Track counts from all results for display in sidebar
    console.log('updateSourceCounts called with', results.length, 'results');
    allSourceCounts = {};

    results.forEach(r => {
        const source = r.sourceType || 'unknown';
        allSourceCounts[source] = (allSourceCounts[source] || 0) + 1;
    });

    console.log('allSourceCounts after update:', allSourceCounts);

    // Update sidebar with new counts
    const sourcesDiv = document.getElementById('search-sources');
    if (!sourcesDiv) return;

    sourcesDiv.querySelectorAll('.source-filter').forEach(filter => {
        const source = filter.dataset.source;
        const count = allSourceCounts[source] || 0;
        const countSpan = filter.querySelector('.source-count');
        if (countSpan) {
            countSpan.textContent = count;
        }
    });
}
