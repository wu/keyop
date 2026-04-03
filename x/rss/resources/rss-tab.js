export const handlesHorizontalNav = true;

// Module-level state
let allArticles = [];
let allTags = [];
let currentFeedURL = '';
let currentTag = null;
let currentArticleId = null;
let viewMode = 'unseen';
let feedsCollapsed = false;
let tagsCollapsed = false;
let totalArticles = 0;
let currentPage = 0;
const PAGE_SIZE = 50;
let searchQuery = '';
let searchFullText = false;
let searchTimeout = null;

let els = null;
let allFeeds = [];

// ────────────────────────────────────────────────────────────────────────────
// Export: Handle incoming messages (called by webui framework)
// ────────────────────────────────────────────────────────────────────────────

export async function onMessage(msg) {
    if (!els) return;
    if (msg.event === 'new_article') {
        // New article received; refresh all views
        await Promise.all([loadArticles(), loadFeeds(), loadTags(), refreshBadge()]);
    }
}

// ────────────────────────────────────────────────────────────────────────────
// Export: Initialize the RSS tab (called by webui framework)
// ────────────────────────────────────────────────────────────────────────────

export async function init(container) {
    els = {
        feedList: container.querySelector('#rss-feed-list'),
        tagList: container.querySelector('#rss-tag-list'),
        articleList: container.querySelector('#rss-article-list'),
        detail: container.querySelector('#rss-detail'),
        articleCount: container.querySelector('#rss-article-count'),
        container: container.querySelector('#rss-container'),
        searchInput: container.querySelector('#rss-search-input'),
        searchClear: container.querySelector('#rss-search-clear'),
        searchFull: container.querySelector('#rss-search-full'),
        toolbarMarkBtn: container.querySelector('#rss-mark-seen-toolbar'),
        toolbarReadLaterBtn: container.querySelector('#rss-read-later-toolbar'),
        toolbarDeleteBtn: container.querySelector('#rss-delete-toolbar'),
        feedsHeader: container.querySelector('#rss-feeds-header'),
        tagsHeader: container.querySelector('#rss-tags-header'),
        pagination: container.querySelector('#rss-pagination'),
    };

    if (!els.feedList) return;

    // Setup view mode radio buttons
    container.querySelectorAll('input[name="rss-view"]').forEach(radio => {
        radio.addEventListener('change', () => {
            if (radio.checked) {
                viewMode = radio.value;
                currentPage = 0;
                loadArticles();
            }
        });
    });

    // Search input handlers
    if (els.searchInput) {
        els.searchInput.addEventListener('input', (e) => {
            clearTimeout(searchTimeout);
            searchQuery = (e.target.value || '').trim();
            searchTimeout = setTimeout(() => loadArticles(), 300);
        });
    }
    if (els.searchClear) {
        els.searchClear.addEventListener('click', () => {
            searchQuery = '';
            if (els.searchInput) els.searchInput.value = '';
            loadArticles();
        });
    }
    if (els.searchFull) {
        els.searchFull.addEventListener('change', () => {
            searchFullText = !!els.searchFull.checked;
            loadArticles();
        });
    }

    // Collapsible headers
    if (els.feedsHeader) {
        els.feedsHeader.addEventListener('click', () => {
            feedsCollapsed = !feedsCollapsed;
            const section = els.feedsHeader.closest('.rss-sidebar-section');
            if (feedsCollapsed) {
                section.classList.add('collapsed');
                els.feedsHeader.classList.add('collapsed');
            } else {
                section.classList.remove('collapsed');
                els.feedsHeader.classList.remove('collapsed');
            }
        });
    }

    if (els.tagsHeader) {
        els.tagsHeader.addEventListener('click', () => {
            tagsCollapsed = !tagsCollapsed;
            const section = els.tagsHeader.closest('.rss-sidebar-section');
            if (tagsCollapsed) {
                section.classList.add('collapsed');
                els.tagsHeader.classList.add('collapsed');
            } else {
                section.classList.remove('collapsed');
                els.tagsHeader.classList.remove('collapsed');
            }
        });
    }

    // Toolbar buttons
    function getCurrentArticle() {
        if (!currentArticleId) return null;
        return allArticles.find(a => a.id === currentArticleId) || null;
    }

    if (els.toolbarMarkBtn) {
        els.toolbarMarkBtn.addEventListener('click', () => {
            const art = getCurrentArticle();
            if (art) markSeen(art);
        });
    }

    if (els.toolbarReadLaterBtn) {
        els.toolbarReadLaterBtn.addEventListener('click', () => {
            const art = getCurrentArticle();
            if (art) markReadLater(art);
        });
    }

    if (els.toolbarDeleteBtn) {
        els.toolbarDeleteBtn.addEventListener('click', () => {
            const art = getCurrentArticle();
            if (art) deleteArticle(art);
        });
    }

    // Listen for navigation from search tab
    document.addEventListener('navigate-to-item', (e) => {
        const {itemId, sourceType} = e.detail || {};
        if (itemId && sourceType === 'rss') {
            const articleId = parseInt(itemId, 10) || itemId;
            // Switch to 'all' view to ensure article is visible
            viewMode = 'all';
            currentPage = 0;
            currentFeedURL = '';
            currentTag = null;
            const allRadio = container.querySelector('input[name="rss-view"][value="all"]');
            if (allRadio) {
                allRadio.checked = true;
                allRadio.dispatchEvent(new Event('change', {bubbles: true}));
            }
            // Load and select the article
            loadArticlePage(articleId).then(() => {
                const article = allArticles.find(a => a.id === articleId);
                if (article) {
                    selectArticle(article);
                }
            });
        }
    });

    // Perform initial load
    await Promise.all([loadFeeds(), loadTags(), loadArticles(), refreshBadge()]);
}

// ────────────────────────────────────────────────────────────────────────────
// Implementation functions (rest of the original file extracted from IIFE)
// ────────────────────────────────────────────────────────────────────────────

// ── API helper ──────────────────────────────────────────────────────────

async function callAction(action, params) {
    try {
        const resp = await fetch(`/api/tabs/rss/action/${action}`, {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(params || {}),
        });
        if (!resp.ok) {
            console.error(`RSS ${action} failed: ${resp.status}`);
            return null;
        }
        return await resp.json();
    } catch (err) {
        console.error('RSS API error:', err);
        return null;
    }
}

// ── Formatting helpers ──────────────────────────────────────────────────

function formatAge(iso) {
    try {
        const timestamp = new Date(iso);
        const now = new Date();
        const elapsedMs = now - timestamp;
        if (elapsedMs < 0) return 'in the future';
        const seconds = Math.floor(elapsedMs / 1000);
        const minutes = Math.floor(seconds / 60);
        const hours = Math.floor(minutes / 60);
        const days = Math.floor(hours / 24);
        const weeks = Math.floor(days / 7);
        const months = Math.floor(days / 30);
        const years = Math.floor(days / 365);
        if (years > 0) return `${years}y ${months % 12}m ago`;
        if (months > 0) return `${months}m ${days % 30}d ago`;
        if (weeks > 0) return `${weeks}w ${days % 7}d ago`;
        if (days > 0) return `${days}d ${hours % 24}h ago`;
        if (hours > 0) return `${hours}h ${minutes % 60}m ago`;
        if (minutes > 0) return `${minutes}m ago`;
        return 'just now';
    } catch (_) {
        return iso;
    }
}

function formatFullDate(iso) {
    try {
        const d = new Date(iso);
        return d.toLocaleDateString() + ' ' + d.toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'});
    } catch (_) {
        return iso;
    }
}

function parseTags(tagsStr) {
    if (!tagsStr) return [];
    return tagsStr.split(',').map(t => t.trim()).filter(t => t !== '');
}

// ── Badge ───────────────────────────────────────────────────────────────

function updateBadge(count) {
    const tabLink = document.querySelector('[data-tab-id="rss"]');
    if (!tabLink) return;
    const existing = tabLink.querySelector('.tab-badge');
    if (existing) existing.remove();
    if (count > 0) {
        const badge = document.createElement('span');
        badge.className = 'tab-badge';
        badge.textContent = count;
        tabLink.appendChild(badge);
    }
}

async function refreshBadge() {
    const result = await callAction('fetch-unseen-count', {});
    if (result != null) updateBadge(result.count || 0);
}

// ── Feed sidebar ────────────────────────────────────────────────────────

function renderFeeds() {
    els.feedList.innerHTML = '';

    // Count articles in current view for each feed
    const feedCounts = {};
    allArticles.forEach(article => {
        if (!feedCounts[article.feedUrl]) {
            feedCounts[article.feedUrl] = 0;
        }
        feedCounts[article.feedUrl]++;
    });

    const allItem = document.createElement('div');
    allItem.className = 'rss-feed-item' + (currentFeedURL === '' ? ' active' : '');
    const totalCount = Object.values(feedCounts).reduce((s, n) => s + n, 0);
    allItem.innerHTML = `<span class="rss-feed-label">All Feeds</span><span class="rss-feed-count">${totalCount}</span>`;
    allItem.addEventListener('click', () => selectFeed(''));
    els.feedList.appendChild(allItem);

    allFeeds.forEach(f => {
        const item = document.createElement('div');
        item.className = 'rss-feed-item' + (currentFeedURL === f.url ? ' active' : '');
        const count = feedCounts[f.url] || 0;
        item.innerHTML = `<span class="rss-feed-label">${escHtml(f.title || f.url)}</span><span class="rss-feed-count">${count}</span>`;
        item.addEventListener('click', () => selectFeed(f.url));
        els.feedList.appendChild(item);
    });
}

function selectFeed(url) {
    currentFeedURL = url;
    currentTag = null;
    currentArticleId = null;
    currentPage = 0;
    renderFeeds();
    renderTags();
    renderArticles();
    showDetail(null);
}

// ── Tags sidebar ────────────────────────────────────────────────────────

function renderTags() {
    els.tagList.innerHTML = '';

    if (allTags.length === 0) {
        els.tagList.innerHTML = '<div class="rss-detail-empty">No tags</div>';
        return;
    }

    allTags.forEach(tag => {
        const item = document.createElement('div');
        item.className = 'rss-tag-item' + (currentTag === tag ? ' active' : '');
        // Count articles with this tag
        const count = allArticles.filter(a => parseTags(a.tags).includes(tag)).length;
        item.innerHTML = `<span class="rss-tag-label">${escHtml(tag)}</span><span class="rss-tag-count">${count}</span>`;
        item.addEventListener('click', () => selectTag(tag));
        els.tagList.appendChild(item);
    });
}

function selectTag(tag) {
    currentTag = tag;
    currentArticleId = null;
    currentPage = 0;
    renderTags();
    renderArticles();
    showDetail(null);
}

// ── Article list ────────────────────────────────────────────────────────

function filteredArticles() {
    let articles = allArticles;
    // Feed and tag filtering - these happen on the frontend
    if (currentFeedURL) articles = articles.filter(a => a.feedUrl === currentFeedURL);
    if (currentTag) articles = articles.filter(a => parseTags(a.tags).includes(currentTag));
    // View mode filtering already done on backend, no need to filter again
    return articles;
}

function renderArticles() {
    const articles = filteredArticles();
    const hasFilters = currentFeedURL !== '' || currentTag !== null;

    // Calculate total for display
    let displayTotal = totalArticles;
    if (hasFilters) {
        // When filters are active, the total shown is the filtered total
        displayTotal = articles.length;
    }

    els.articleCount.textContent = `${articles.length} article${articles.length !== 1 ? 's' : ''}${hasFilters ? '' : ` (${displayTotal} total)`}`;
    els.articleList.innerHTML = '';

    if (articles.length === 0) {
        els.articleList.innerHTML = '<div class="rss-detail-empty">No articles</div>';
        els.pagination.style.display = 'none';
        return;
    }

    let displayArticles = articles;
    let totalPages = 1;

    if (hasFilters) {
        // Client-side pagination for filtered results
        totalPages = Math.ceil(articles.length / PAGE_SIZE);
        const start = currentPage * PAGE_SIZE;
        const end = start + PAGE_SIZE;
        displayArticles = articles.slice(start, end);
    } else {
        // Backend pagination - articles are already paginated
        totalPages = Math.ceil(totalArticles / PAGE_SIZE);
    }

    displayArticles.forEach(a => {
        const item = document.createElement('div');
        item.className = 'rss-article-item' + (a.id === currentArticleId ? ' active' : '') + (a.seen ? ' seen' : '');
        item.dataset.id = a.id;
        item.innerHTML = `
            <div class="rss-article-item-title">${escHtml(a.title)}</div>
            <div class="rss-article-item-meta">
                <span class="rss-article-item-feed">${escHtml(a.feedTitle)}</span>
                <span class="rss-article-item-date">${formatAge(a.published)}</span>
            </div>`;
        item.addEventListener('click', () => selectArticle(a));
        els.articleList.appendChild(item);
    });

    // Show pagination if needed
    if (totalPages > 1) {
        els.pagination.style.display = 'block';
        els.pagination.innerHTML = '';

        const pageInfo = document.createElement('div');
        pageInfo.className = 'rss-pagination-info';
        pageInfo.textContent = `Page ${currentPage + 1} of ${totalPages}`;
        els.pagination.appendChild(pageInfo);

        const buttonContainer = document.createElement('div');
        buttonContainer.className = 'rss-pagination-buttons';

        const prevBtn = document.createElement('button');
        prevBtn.textContent = '← Previous';
        prevBtn.disabled = currentPage === 0;
        prevBtn.addEventListener('click', () => {
            if (currentPage > 0) {
                currentPage--;
                if (hasFilters) {
                    renderArticles();
                } else {
                    loadArticles();
                }
                els.articleList.scrollTop = 0;
            }
        });
        buttonContainer.appendChild(prevBtn);

        const nextBtn = document.createElement('button');
        nextBtn.textContent = 'Next →';
        nextBtn.disabled = currentPage >= totalPages - 1;
        nextBtn.addEventListener('click', () => {
            if (currentPage < totalPages - 1) {
                currentPage++;
                if (hasFilters) {
                    renderArticles();
                } else {
                    loadArticles();
                }
                els.articleList.scrollTop = 0;
            }
        });
        buttonContainer.appendChild(nextBtn);

        els.pagination.appendChild(buttonContainer);
    } else {
        els.pagination.style.display = 'none';
    }
}

function selectArticle(article) {
    currentArticleId = article.id;
    els.articleList.querySelectorAll('.rss-article-item').forEach(el => {
        el.classList.toggle('active', parseInt(el.dataset.id) === article.id);
    });
    showDetail(article);
    els.container && els.container.classList.add('article-selected');
}

// ── Detail pane ─────────────────────────────────────────────────────────

function showDetail(article) {
    const toolbarBtn = document.getElementById('rss-mark-seen-toolbar');
    const readLaterBtn = document.getElementById('rss-read-later-toolbar');
    const deleteBtn = document.getElementById('rss-delete-toolbar');

    if (!article) {
        if (toolbarBtn) {
            toolbarBtn.textContent = 'Mark as seen';
            toolbarBtn.disabled = true;
            toolbarBtn.classList.remove('seen');
        }
        if (readLaterBtn) {
            readLaterBtn.textContent = '🔖 Read later';
            readLaterBtn.disabled = true;
            readLaterBtn.classList.remove('seen');
        }
        if (deleteBtn) {
            deleteBtn.disabled = true;
        }
        els.detail.innerHTML = '<div class="rss-detail-empty">Select an article to read</div>';
        return;
    }

    if (toolbarBtn) {
        if (article.readLater && !article.seen) {
            // Read-later but not yet seen
            toolbarBtn.textContent = '✓ Done reading';
            toolbarBtn.disabled = false;
            toolbarBtn.classList.remove('seen');
            toolbarBtn.dataset.doneReading = '1';
            delete toolbarBtn.dataset.unseen;
        } else if (article.seen && !article.readLater) {
            // Seen but not read-later - allow marking unseen
            toolbarBtn.textContent = 'Mark as unseen';
            toolbarBtn.disabled = false;
            toolbarBtn.classList.add('seen');
            toolbarBtn.dataset.unseen = '1';
            delete toolbarBtn.dataset.doneReading;
        } else if (article.seen && article.readLater && viewMode === 'read-later') {
            // In read-later view, show "Done reading" to remove from read-later
            toolbarBtn.textContent = '✓ Done reading';
            toolbarBtn.disabled = false;
            toolbarBtn.classList.add('seen');
            delete toolbarBtn.dataset.doneReading;
            delete toolbarBtn.dataset.unseen;
        } else if (article.seen) {
            // Seen (for other views)
            toolbarBtn.textContent = '✓ Seen';
            toolbarBtn.disabled = false;
            toolbarBtn.classList.add('seen');
            toolbarBtn.dataset.unseen = '1';
            delete toolbarBtn.dataset.doneReading;
        } else {
            // Unseen and not read-later
            toolbarBtn.textContent = 'Mark as seen';
            toolbarBtn.disabled = false;
            toolbarBtn.classList.remove('seen');
            delete toolbarBtn.dataset.doneReading;
            delete toolbarBtn.dataset.unseen;
        }
    }

    if (readLaterBtn) {
        if (article.readLater) {
            // In 'read-later' view, show toggle for seen state
            // In other views, show button to mark/unmark as read-later
            if (viewMode === 'read-later') {
                // Toggle seen state
                if (article.seen) {
                    readLaterBtn.textContent = 'Mark as unseen';
                    readLaterBtn.disabled = false;
                    readLaterBtn.classList.remove('seen');
                    readLaterBtn.dataset.toggleSeen = '1';
                    delete readLaterBtn.dataset.saved;
                } else {
                    readLaterBtn.textContent = 'Mark as seen';
                    readLaterBtn.disabled = false;
                    readLaterBtn.classList.remove('seen');
                    readLaterBtn.dataset.toggleSeen = '1';
                    delete readLaterBtn.dataset.saved;
                }
            } else {
                // Other views: show button to unmark read-later
                readLaterBtn.textContent = '🔖 Saved';
                readLaterBtn.disabled = false;  // Enable to allow unmarking
                readLaterBtn.classList.add('seen');
                readLaterBtn.dataset.saved = '1';
                delete readLaterBtn.dataset.toggleSeen;
            }
        } else {
            readLaterBtn.textContent = '🔖 Read later';
            readLaterBtn.disabled = false;
            readLaterBtn.classList.remove('seen');
            delete readLaterBtn.dataset.saved;
            delete readLaterBtn.dataset.toggleSeen;
        }
    }

    if (deleteBtn) {
        deleteBtn.disabled = false;
    }

    const body = article.description
        ? `<div class="rss-detail-body">${sanitizeHtml(article.description, article.link)}</div>`
        : '';

    const tags = parseTags(article.tags);
    const tagsHtml = tags.length > 0
        ? `<div class="rss-detail-tags">${tags.map(t => `<span class="rss-detail-tag">${escHtml(t)}<span class="rss-detail-tag-remove" data-tag="${escAttr(t)}">×</span></span>`).join('')}</div>`
        : '';

    els.detail.innerHTML = `
        <h1 class="rss-detail-title">
            <a href="${escAttr(article.link)}" target="_blank" rel="noopener noreferrer">${escHtml(article.title)}</a>
        </h1>
        <div class="rss-detail-meta">
            <span class="rss-detail-feed">${escHtml(article.feedTitle)}</span>
            <span>${formatFullDate(article.published)}</span>
        </div>
        ${tagsHtml}
        ${body}
        <div class="rss-tag-input-section">
            <label class="rss-tag-input-label">Add tag</label>
            <div class="rss-tag-input-wrapper">
                <input type="text" id="rss-new-tag-input" placeholder="New tag..." />
                <button id="rss-add-tag-btn">Add</button>
            </div>
        </div>
        <a class="rss-open-link" href="${escAttr(article.link)}" target="_blank" rel="noopener noreferrer">
            Open original ↗
        </a>`;

    // Attach event listeners to tag remove buttons
    els.detail.querySelectorAll('.rss-detail-tag-remove').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            const tag = btn.dataset.tag;
            removeTagFromArticle(article, tag);
        });
    });

    // Attach event listeners to add tag button
    const addTagBtn = els.detail.querySelector('#rss-add-tag-btn');
    const newTagInput = els.detail.querySelector('#rss-new-tag-input');
    if (addTagBtn && newTagInput) {
        addTagBtn.addEventListener('click', () => {
            const newTag = newTagInput.value.trim();
            if (newTag) {
                addTagToArticle(article, newTag);
                newTagInput.value = '';
            }
        });
        newTagInput.addEventListener('keypress', (e) => {
            if (e.key === 'Enter') {
                addTagBtn.click();
            }
        });
    }
}

async function markSeen(article) {
    // Check if the button is in "mark unseen" mode
    const toolbarBtn = document.getElementById('rss-mark-seen-toolbar');
    const isMarkingUnseen = toolbarBtn && toolbarBtn.dataset.unseen === '1';

    if (isMarkingUnseen) {
        // Mark as unseen
        const result = await callAction('mark-unseen', {id: article.id});
        if (!result) {
            console.error('Failed to mark as unseen');
            return;
        }
        article.seen = false;

        // update toolbar button and list item
        if (toolbarBtn) {
            toolbarBtn.textContent = 'Mark as seen';
            toolbarBtn.disabled = false;
            toolbarBtn.classList.remove('seen');
            delete toolbarBtn.dataset.unseen;
        }

        const items = Array.from(els.articleList.querySelectorAll('.rss-article-item'));
        const idx = items.findIndex(el => parseInt(el.dataset.id) === article.id);
        items[idx] && items[idx].classList.remove('seen');

        showDetail(article);
        await Promise.all([refreshBadge(), loadFeeds()]);
        return;
    }

    // Mark as seen (original behavior)
    const isDoneReading = article.readLater;
    let result;
    if (isDoneReading) {
        result = await callAction('mark-done-reading', {id: article.id});
        if (!result) {
            console.error('Failed to mark done reading');
            return;
        }
        article.seen = true;
        article.readLater = false;
    } else {
        result = await callAction('mark-seen', {id: article.id});
        if (!result) {
            console.error('Failed to mark as seen');
            return;
        }
        article.seen = true;
    }

    // update toolbar button
    if (toolbarBtn) {
        toolbarBtn.textContent = '✓ Seen';
        toolbarBtn.disabled = false;  // Enable to allow marking unseen
        toolbarBtn.classList.add('seen');
        toolbarBtn.dataset.unseen = '1';
        delete toolbarBtn.dataset.id;
    }

    const items = Array.from(els.articleList.querySelectorAll('.rss-article-item'));
    const idx = items.findIndex(el => parseInt(el.dataset.id) === article.id);

    if (viewMode === 'unseen' || (isDoneReading && viewMode === 'read-later')) {
        // article leaves the current view — advance to next
        const nextItem = items[idx + 1] || items[idx - 1];
        const nextId = nextItem ? parseInt(nextItem.dataset.id) : null;
        const nextArticle = nextId != null ? filteredArticles().find(a => a.id === nextId) : null;

        items[idx] && items[idx].remove();
        const remaining = els.articleList.querySelectorAll('.rss-article-item').length;
        els.articleCount.textContent = `${remaining} article${remaining !== 1 ? 's' : ''}`;

        if (nextArticle) {
            selectArticle(nextArticle);
        } else {
            currentArticleId = null;
            showDetail(null);
        }
    } else {
        items[idx] && items[idx].classList.add('seen');
        showDetail(article);
    }

    await Promise.all([refreshBadge(), loadFeeds()]);
}

async function markReadLater(article) {
    const readLaterBtn = document.getElementById('rss-read-later-toolbar');
    const toolbarBtn = document.getElementById('rss-mark-seen-toolbar');
    const isSaved = readLaterBtn && readLaterBtn.dataset.saved === '1';
    const isToggleSeen = readLaterBtn && readLaterBtn.dataset.toggleSeen === '1';

    if (isToggleSeen) {
        // In read-later view: toggle the seen state
        let result;
        if (article.seen) {
            // Mark as unseen
            result = await callAction('mark-unseen', {id: article.id});
            if (!result) {
                console.error('Failed to mark as unseen');
                return;
            }
            article.seen = false;
        } else {
            // Mark as seen
            result = await callAction('mark-seen', {id: article.id});
            if (!result) {
                console.error('Failed to mark as seen');
                return;
            }
            article.seen = true;
        }

        showDetail(article);
        await Promise.all([refreshBadge(), loadFeeds()]);
    } else if (isSaved) {
        // Unmark as read-later
        const result = await callAction('unmark-read-later', {id: article.id});
        if (!result) {
            console.error('Failed to unmark read-later');
            return;
        }
        article.readLater = false;

        // update button
        if (readLaterBtn) {
            readLaterBtn.textContent = '🔖 Read later';
            readLaterBtn.disabled = false;
            readLaterBtn.classList.remove('seen');
            delete readLaterBtn.dataset.saved;
        }

        showDetail(article);
        await Promise.all([refreshBadge(), loadFeeds()]);
    } else {
        // Mark as read-later (which auto-marks as seen)
        const result = await callAction('mark-read-later', {id: article.id});
        if (!result) {
            console.error('Failed to mark as read-later');
            return;
        }
        article.readLater = true;
        article.seen = true;  // Backend now auto-marks as seen

        // update read-later button
        if (readLaterBtn) {
            readLaterBtn.textContent = '🔖 Saved';
            readLaterBtn.disabled = false;
            readLaterBtn.classList.add('seen');
            readLaterBtn.dataset.saved = '1';
        }

        // update mark-seen button to show "Mark as unseen" (with unseen indicator)
        if (toolbarBtn) {
            toolbarBtn.textContent = 'Mark as unseen';
            toolbarBtn.classList.add('seen');
            toolbarBtn.dataset.unseen = '1';
        }

        // If we're in unseen view, this item leaves the view — advance to next
        if (viewMode === 'unseen') {
            const items = Array.from(els.articleList.querySelectorAll('.rss-article-item'));
            const idx = items.findIndex(el => parseInt(el.dataset.id) === article.id);

            const nextItem = items[idx + 1] || items[idx - 1];
            const nextId = nextItem ? parseInt(nextItem.dataset.id) : null;
            const nextArticle = nextId != null ? filteredArticles().find(a => a.id === nextId) : null;

            items[idx] && items[idx].remove();
            const remaining = els.articleList.querySelectorAll('.rss-article-item').length;
            els.articleCount.textContent = `${remaining} article${remaining !== 1 ? 's' : ''}`;

            if (nextArticle) {
                selectArticle(nextArticle);
            } else {
                currentArticleId = null;
                showDetail(null);
            }
        } else {
            showDetail(article);
        }

        await Promise.all([refreshBadge(), loadFeeds()]);
    }
}

async function deleteArticle(article) {
    if (!confirm(`Delete article "${article.title}"?`)) {
        return;
    }

    const result = await callAction('delete-article', {id: article.id});
    if (!result) {
        console.error('Failed to delete article');
        return;
    }

    // Remove from allArticles
    allArticles = allArticles.filter(a => a.id !== article.id);

    // Remove from display
    const items = Array.from(els.articleList.querySelectorAll('.rss-article-item'));
    const idx = items.findIndex(el => parseInt(el.dataset.id) === article.id);

    const nextItem = items[idx + 1] || items[idx - 1];
    const nextId = nextItem ? parseInt(nextItem.dataset.id) : null;
    const nextArticle = nextId != null ? filteredArticles().find(a => a.id === nextId) : null;

    items[idx] && items[idx].remove();
    const remaining = els.articleList.querySelectorAll('.rss-article-item').length;
    els.articleCount.textContent = `${remaining} article${remaining !== 1 ? 's' : ''}`;

    currentArticleId = null;
    if (nextArticle) {
        selectArticle(nextArticle);
    } else {
        showDetail(null);
    }

    await Promise.all([refreshBadge(), loadFeeds(), loadTags()]);
}

async function addTagToArticle(article, tag) {
    const result = await callAction('add-tag', {id: article.id, tag: tag});
    if (result && result.ok) {
        article.tags = result.tags;
        showDetail(article);
        await loadTags();
    }
}

async function removeTagFromArticle(article, tag) {
    const result = await callAction('remove-tag', {id: article.id, tag: tag});
    if (result && result.ok) {
        article.tags = result.tags;
        showDetail(article);
        await loadTags();
    }
}

// ── Safety helpers ──────────────────────────────────────────────────────

function escHtml(s) {
    const d = document.createElement('div');
    d.textContent = s || '';
    return d.innerHTML;
}

function escAttr(s) {
    return (s || '').replace(/"/g, '&quot;').replace(/'/g, '&#39;');
}

function sanitizeHtml(html, baseUrl) {
    const tmp = document.createElement('div');
    tmp.innerHTML = html || '';
    // remove potentially dangerous tags
    tmp.querySelectorAll('script,style,iframe,object,embed,form').forEach(el => el.remove());

    // strip event handlers
    tmp.querySelectorAll('[onclick],[onerror],[onload]').forEach(el => {
        ['onclick', 'onerror', 'onload'].forEach(attr => el.removeAttribute(attr));
    });

    // sanitize anchors and resolve relative links when possible
    tmp.querySelectorAll('a[href],link[href]').forEach(el => {
        const href = el.getAttribute('href') || '';
        const hrefTrim = href.trim();
        if (hrefTrim.toLowerCase().startsWith('javascript:')) {
            el.removeAttribute('href');
            return;
        }
        if (baseUrl && hrefTrim && !/^(https?:|mailto:|tel:|data:|blob:|\/\/|#)/i.test(hrefTrim)) {
            try {
                el.setAttribute('href', new URL(hrefTrim, baseUrl).href);
            } catch (_) {
            }
        }
    });

    // Process <img> elements: restore lazy-loaded sources and replace placeholders
    tmp.querySelectorAll('img').forEach(img => {
        let src = img.getAttribute('src') || '';
        const srcTrim = src.trim();
        const isPlaceholder = !srcTrim || srcTrim === 'about:blank' || srcTrim === '#' ||
            (srcTrim.startsWith('data:image/gif;base64') && srcTrim.length < 200) || /placeholder|transparent|pixel|blank|spinner|thumb/i.test(srcTrim);
        if (isPlaceholder) {
            const dataSrc = img.getAttribute('data-src') || img.getAttribute('data-lazy-src') || img.getAttribute('data-original') || img.getAttribute('data-actualsrc') || img.getAttribute('data-srcset') || img.getAttribute('data-lazy') || '';
            if (dataSrc) img.setAttribute('src', dataSrc);
        }
        // Fallback: if src still empty but data-srcset present, use first entry
        if (!(img.getAttribute('src') || '').trim()) {
            const ds = img.getAttribute('data-srcset') || img.getAttribute('data-lazy-srcset') || '';
            if (ds) {
                const first = ds.split(',')[0].trim().split(' ')[0];
                if (first) img.setAttribute('src', first);
            }
        }

        // Resolve src relative to baseUrl if necessary
        const finalSrc = img.getAttribute('src') || '';
        if (finalSrc && baseUrl) {
            try {
                img.setAttribute('src', new URL(finalSrc, baseUrl).href);
            } catch (_) {
            }
        }

        if (finalSrc.trim().toLowerCase().startsWith('javascript:')) img.removeAttribute('src');
        if (!img.getAttribute('alt')) img.setAttribute('alt', '');

        // Resolve srcset entries to absolute URLs when possible
        const ss = img.getAttribute('srcset') || img.getAttribute('data-srcset') || img.getAttribute('data-lazy-srcset') || '';
        if (ss && baseUrl) {
            const resolved = ss.split(',').map(part => {
                const [url, desc] = part.trim().split(/\s+/, 2);
                try {
                    const abs = new URL(url, baseUrl).href;
                    return desc ? `${abs} ${desc}` : `${abs}`;
                } catch (_) {
                    return part.trim();
                }
            }).join(', ');
            img.setAttribute('srcset', resolved);
        }
    });

    // Process <source> elements (picture/picture sources)
    tmp.querySelectorAll('source').forEach(source => {
        let ss = source.getAttribute('srcset') || '';
        const isPlaceholder = !ss.trim() || ss.toLowerCase().includes('data:image/gif') || /placeholder|transparent|pixel/i.test(ss);
        if (isPlaceholder) {
            const dataSs = source.getAttribute('data-srcset') || source.getAttribute('data-lazy-srcset') || '';
            if (dataSs) source.setAttribute('srcset', dataSs);
        }
        const finalSs = source.getAttribute('srcset') || '';
        if (finalSs && baseUrl) {
            const resolved = finalSs.split(',').map(part => {
                const [url, desc] = part.trim().split(/\s+/, 2);
                try {
                    const abs = new URL(url, baseUrl).href;
                    return desc ? `${abs} ${desc}` : `${abs}`;
                } catch (_) {
                    return part.trim();
                }
            }).join(', ');
            source.setAttribute('srcset', resolved);
        }
        if (source.getAttribute('srcset') && source.getAttribute('srcset').toLowerCase().includes('javascript:')) source.removeAttribute('srcset');
    });

    // Remove javascript: from srcset entries if present
    tmp.querySelectorAll('[srcset]').forEach(el => {
        const ss = el.getAttribute('srcset') || '';
        if (ss.toLowerCase().includes('javascript:')) el.removeAttribute('srcset');
    });

    // Replace <noscript> blocks that contain image markup (common lazy patterns)
    tmp.querySelectorAll('noscript').forEach(ns => {
        try {
            const ntd = document.createElement('div');
            ntd.innerHTML = ns.textContent || ns.innerHTML || '';
            const img = ntd.querySelector('img');
            if (img && ns.parentNode) {
                ns.parentNode.replaceChild(img, ns);
            } else {
                ns.remove();
            }
        } catch (e) {
            ns.remove();
        }
    });

    return tmp.innerHTML;
}

// ── Data loading ─────────────────────────────────────────────────────────

async function loadFeeds() {
    const result = await callAction('fetch-feeds', {});
    if (result && result.feeds) {
        allFeeds = result.feeds;
        renderFeeds();
    }
}

async function loadTags() {
    const result = await callAction('fetch-all-tags', {});
    if (result && result.tags) {
        allTags = result.tags;
        renderTags();
    }
}

async function loadArticles(loadAll = false) {
    // When filters are active, we need all articles for client-side pagination
    // When no filters, use backend pagination (unless loadAll is true for search navigation)
    const hasFilters = currentFeedURL !== '' || currentTag !== null;
    const params = {
        view: viewMode,
        q: searchQuery || '',
        full: !!searchFullText,
        feed_url: hasFilters ? currentFeedURL : '',
    };

    if (!hasFilters && !loadAll) {
        // Use backend pagination when no filters
        params.offset = currentPage * PAGE_SIZE;
        params.limit = PAGE_SIZE;
    }

    const result = await callAction('fetch-articles', params);
    if (result && result.articles) {
        allArticles = result.articles;
        totalArticles = result.total || 0;
        renderArticles();
        renderFeeds();  // Update feed counts for current view
        renderTags();  // Update tag counts after articles load
        // If selected article is no longer present, clear selection
        if (currentArticleId && !allArticles.some(a => a.id === currentArticleId)) {
            currentArticleId = null;
            showDetail(null);
            els.container && els.container.classList.remove('article-selected');
        }
    }
}

async function loadArticlePage(articleId) {
    // Load the page containing a specific article
    const hasFilters = currentFeedURL !== '' || currentTag !== null;
    const params = {
        view: viewMode,
        q: searchQuery || '',
        full: !!searchFullText,
        feed_url: hasFilters ? currentFeedURL : '',
        'contains-id': articleId,
    };

    if (!hasFilters) {
        params.limit = PAGE_SIZE;
    }

    const result = await callAction('fetch-articles', params);
    if (result && result.articles) {
        allArticles = result.articles;
        totalArticles = result.total || 0;
        // Update currentPage based on the offset returned from backend
        if (result.offset !== undefined) {
            currentPage = Math.floor(result.offset / PAGE_SIZE);
        }
        renderArticles();
        renderFeeds();  // Update feed counts for current view
        renderTags();  // Update tag counts after articles load
    }
}

// ── SSE refresh ──────────────────────────────────────────────────────────

document.addEventListener('keyop-sse', (e) => {
    try {
        const msg = JSON.parse(e.detail);
        if (msg.event === 'new_article') {
            Promise.all([loadArticles(), loadFeeds(), loadTags(), refreshBadge()]);
        }
    } catch (_) {
    }
});
// ────────────────────────────────────────────────────────────────────────────
// Navigation support (called by webui framework)
// ────────────────────────────────────────────────────────────────────────────

export function focusItems() {
    const item = els.articleList.querySelector('.rss-article-item');
    if (item) item.focus();
}

export function canReturnToTabs() {
    return true;
}
