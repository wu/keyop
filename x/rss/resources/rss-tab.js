export const handlesHorizontalNav = true;

(() => {
    let allArticles = [];
    let currentFeedURL = '';   // '' = all feeds
    let currentArticleId = null;
    let viewMode = 'unseen';   // 'unseen' | 'read-later' | 'seen' | 'all'

    const els = {
        feedList: document.getElementById('rss-feed-list'),
        articleList: document.getElementById('rss-article-list'),
        detail: document.getElementById('rss-detail'),
        articleCount: document.getElementById('rss-article-count'),
        container: document.getElementById('rss-container'),
        searchInput: document.getElementById('rss-search-input'),
        searchClear: document.getElementById('rss-search-clear'),
        searchFull: document.getElementById('rss-search-full'),
        toolbarMarkBtn: document.getElementById('rss-mark-seen-toolbar'),
        toolbarReadLaterBtn: document.getElementById('rss-read-later-toolbar'),
    };
    if (!els.feedList) return;

    document.querySelectorAll('input[name="rss-view"]').forEach(radio => {
        radio.addEventListener('change', () => {
            if (radio.checked) {
                viewMode = radio.value;
                loadArticles();
            }
        });
    });

    // Search controls
    let searchQuery = '';
    let searchFullText = false;
    let searchTimeout = null;

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

    // Toolbar helper
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

    let allFeeds = [];

    function renderFeeds() {
        els.feedList.innerHTML = '';

        const allItem = document.createElement('div');
        allItem.className = 'rss-feed-item' + (currentFeedURL === '' ? ' active' : '');
        const totalUnseen = allFeeds.reduce((s, f) => s + (f.unseenCount || 0), 0);
        const totalCount = allFeeds.reduce((s, f) => s + (f.count || 0), 0);
        const displayCount = viewMode === 'unseen' ? totalUnseen : totalCount;
        allItem.innerHTML = `<span class="rss-feed-label">All Feeds</span><span class="rss-feed-count">${displayCount}</span>`;
        allItem.addEventListener('click', () => selectFeed(''));
        els.feedList.appendChild(allItem);

        allFeeds.forEach(f => {
            const item = document.createElement('div');
            item.className = 'rss-feed-item' + (currentFeedURL === f.url ? ' active' : '');
            const n = viewMode === 'unseen' ? (f.unseenCount || 0) : (f.count || 0);
            item.innerHTML = `<span class="rss-feed-label">${escHtml(f.title || f.url)}</span><span class="rss-feed-count">${n}</span>`;
            item.addEventListener('click', () => selectFeed(f.url));
            els.feedList.appendChild(item);
        });
    }

    function selectFeed(url) {
        currentFeedURL = url;
        currentArticleId = null;
        renderFeeds();
        renderArticles();
        showDetail(null);
    }

    // ── Article list ────────────────────────────────────────────────────────

    function filteredArticles() {
        let articles = allArticles;
        if (currentFeedURL) articles = articles.filter(a => a.feedUrl === currentFeedURL);
        if (viewMode === 'unseen') articles = articles.filter(a => !a.seen && !a.readLater);
        else if (viewMode === 'seen') articles = articles.filter(a => a.seen && !a.readLater);
        else if (viewMode === 'read-later') articles = articles.filter(a => a.readLater);
        // 'all': no additional filter
        return articles;
    }

    function renderArticles() {
        const articles = filteredArticles();
        els.articleCount.textContent = `${articles.length} article${articles.length !== 1 ? 's' : ''}`;
        els.articleList.innerHTML = '';

        if (articles.length === 0) {
            els.articleList.innerHTML = '<div class="rss-detail-empty">No articles</div>';
            return;
        }

        articles.forEach(a => {
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
            } else if (article.seen) {
                // Seen (could be read-later or not) - allow marking unseen
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
                readLaterBtn.textContent = '🔖 Saved';
                readLaterBtn.disabled = false;  // Enable to allow unmarking
                readLaterBtn.classList.add('seen');
                readLaterBtn.dataset.saved = '1';
            } else {
                readLaterBtn.textContent = '🔖 Read later';
                readLaterBtn.disabled = false;
                readLaterBtn.classList.remove('seen');
                delete readLaterBtn.dataset.saved;
            }
        }

        const body = article.description
            ? `<div class="rss-detail-body">${sanitizeHtml(article.description, article.link)}</div>`
            : '';

        els.detail.innerHTML = `
            <h1 class="rss-detail-title">
                <a href="${escAttr(article.link)}" target="_blank" rel="noopener noreferrer">${escHtml(article.title)}</a>
            </h1>
            <div class="rss-detail-meta">
                <span class="rss-detail-feed">${escHtml(article.feedTitle)}</span>
                <span>${formatFullDate(article.published)}</span>
            </div>
            ${body}
            <a class="rss-open-link" href="${escAttr(article.link)}" target="_blank" rel="noopener noreferrer">
                Open original ↗
            </a>`;
    }

    async function markSeen(article) {
        // Check if the button is in "mark unseen" mode
        const toolbarBtn = document.getElementById('rss-mark-seen-toolbar');
        const isMarkingUnseen = toolbarBtn && toolbarBtn.dataset.unseen === '1';

        if (isMarkingUnseen) {
            // Mark as unseen
            await callAction('mark-unseen', {id: article.id});
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
        if (isDoneReading) {
            await callAction('mark-done-reading', {id: article.id});
            article.seen = true;
            article.readLater = false;
        } else {
            await callAction('mark-seen', {id: article.id});
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

        if (isSaved) {
            // Unmark as read-later
            await callAction('unmark-read-later', {id: article.id});
            article.readLater = false;

            // update button
            if (readLaterBtn) {
                readLaterBtn.textContent = '🔖 Read later';
                readLaterBtn.disabled = false;
                readLaterBtn.classList.remove('seen');
                delete readLaterBtn.dataset.saved;
            }
        } else {
            // Mark as read-later (which auto-marks as seen)
            await callAction('mark-read-later', {id: article.id});
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
        }

        showDetail(article);
        await Promise.all([refreshBadge(), loadFeeds()]);
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

    async function loadArticles() {
        const params = {view: viewMode, q: searchQuery || '', full: !!searchFullText};
        const result = await callAction('fetch-articles', params);
        if (result && result.articles) {
            allArticles = result.articles;
            renderArticles();
            // If selected article is no longer present, clear selection
            if (currentArticleId && !allArticles.some(a => a.id === currentArticleId)) {
                currentArticleId = null;
                showDetail(null);
                els.container && els.container.classList.remove('article-selected');
            }
        }
    }

    // ── SSE refresh ──────────────────────────────────────────────────────────

    document.addEventListener('keyop-sse', (e) => {
        try {
            const msg = JSON.parse(e.detail);
            if (msg.event === 'new_article') {
                Promise.all([loadArticles(), loadFeeds(), refreshBadge()]);
            }
        } catch (_) {
        }
    });

    // ── Init ─────────────────────────────────────────────────────────────────

    Promise.all([loadFeeds(), loadArticles(), refreshBadge()]);
})();
