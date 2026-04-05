// Signal to app.js that this tab owns horizontal arrow-key navigation.
export const handlesHorizontalNav = true;

// Bridge variables set by the IIFE so app.js can call into module state.
let _focusItems = null;
let _canReturnToTabs = null;

export function focusItems() {
    if (_focusItems) _focusItems();
}

export function canReturnToTabs() {
    return _canReturnToTabs ? _canReturnToTabs() : true;
}

(() => {
    let selectedMovieId = null;
    let selectedTag = null;
    let selectedSet = null;
    let filterActor = null;
    let searchQuery = '';
    let fulltextSearch = false;
    let sortOrder = 'title'; // title | year | runtime | last
    let groupCollections = true; // collapse multi-movie sets into one card
    let editingMovieId = null; // null = creating new
    let searchDebounce = null;
    let flaggedMovieIds = new Set(); // temporary in-memory set of flagged movie IDs
    let flaggedViewMode = null; // 'flagged' | null - tracks which render mode is active
    let loadRequestId = 0; // incremented on each load request, used to prevent race conditions
    let currentRenderRequestId = 0; // tracks which request actually rendered

    // Grid view state
    let viewMode = 'list'; // 'list' | 'grid'
    let gridColumns = 6; // number of columns in grid view (default 6)
    let navigationStack = []; // [{viewMode, context}, ...] for tracking navigation history

    // Keyboard navigation state
    let focusedPanel = 'list'; // 'tags' | 'list'
    let focusedListIdx = -1;   // index into visible .movies-card elements
    let focusedListMovieId = null; // movie/set id at focusedListIdx for cross-reload identity
    let focusedTagIdx = -1;    // index into visible .movies-tag-item elements
    let lastSelectedSetName = null;  // collection name to restore focus after exiting

    const el = {
        container: document.getElementById('movies-container'),
        search: document.getElementById('movies-search'),
        searchClear: document.getElementById('movies-search-clear'),
        fulltextCheck: document.getElementById('movies-fulltext'),
        collapseCheck: document.getElementById('movies-collapse-collections'),
        tmdbSearchBtn: document.getElementById('movies-tmdb-search-btn'),
        newBtn: document.getElementById('movies-new-btn'),
        editBtn: document.getElementById('movies-edit-btn'),
        flagBtn: document.getElementById('movies-flag-btn'),
        deleteBtn: document.getElementById('movies-delete-btn'),
        flaggedBtn: document.getElementById('movies-flagged-btn'),
        nfoInput: document.getElementById('movies-nfo-input'),
        tagList: document.getElementById('movies-tag-list'),
        movieList: document.getElementById('movies-list'),
        detail: document.getElementById('movies-detail'),
        detailPanel: document.getElementById('movies-detail-panel'),
        detailToolbar: document.getElementById('movies-detail-toolbar'),
        detailToolbarBack: document.getElementById('movies-detail-toolbar-back'),
        detailToolbarFlag: document.getElementById('movies-detail-toolbar-flag'),
        detailToolbarEdit: document.getElementById('movies-detail-toolbar-edit'),
        detailToolbarDelete: document.getElementById('movies-detail-toolbar-delete'),
        posterUpload: document.getElementById('movies-poster-upload'),
        // Grid view elements
        listPanel: document.getElementById('movies-list-panel'),
        gridPanel: document.getElementById('movies-grid-panel'),
        tagSidebar: document.getElementById('movies-tag-sidebar'),
        viewToggleList: document.getElementById('movies-view-toggle'),
        viewToggleGrid: document.getElementById('movies-grid-toggle'),
        gridBackBtn: document.getElementById('movies-grid-back-btn'),
        grid: document.getElementById('movies-grid'),
        gridHeaderText: document.getElementById('movies-grid-header-text'),
        gridZoomOut: document.getElementById('movies-grid-zoom-out'),
        gridZoomIn: document.getElementById('movies-grid-zoom-in'),
        gridColumnCount: document.getElementById('movies-grid-column-count'),
        gridPreview: document.getElementById('movies-grid-preview'),
        gridPreviewContent: document.getElementById('movies-grid-preview-content'),
        // Edit modal
        modalOverlay: document.getElementById('movies-modal-overlay'),
        modalTitle: document.getElementById('movies-modal-title'),
        modalClose: document.getElementById('movies-modal-close'),
        modalCancel: document.getElementById('movies-modal-cancel'),
        modalSave: document.getElementById('movies-modal-save'),
        editTitle: document.getElementById('movies-edit-title'),
        editSortTitle: document.getElementById('movies-edit-sort-title'),
        editYear: document.getElementById('movies-edit-year'),
        editRuntime: document.getElementById('movies-edit-runtime'),
        editRating: document.getElementById('movies-edit-rating'),
        editTagline: document.getElementById('movies-edit-tagline'),
        editPlot: document.getElementById('movies-edit-plot'),
        editTmdbId: document.getElementById('movies-edit-tmdb-id'),
        editImdbId: document.getElementById('movies-edit-imdb-id'),
        editPosterUrl: document.getElementById('movies-edit-poster-url'),
        editTags: document.getElementById('movies-edit-tags'),
        editActors: document.getElementById('movies-edit-actors'),
        editSetName: document.getElementById('movies-edit-set-name'),
        editLastPlayed: document.getElementById('movies-edit-last-played'),
        // TMDB modal
        tmdbModalOverlay: document.getElementById('movies-tmdb-modal-overlay'),
        tmdbModalClose: document.getElementById('movies-tmdb-modal-close'),
        tmdbQuery: document.getElementById('movies-tmdb-query'),
        tmdbYear: document.getElementById('movies-tmdb-year'),
        tmdbSearchSubmit: document.getElementById('movies-tmdb-search-submit'),
        tmdbResults: document.getElementById('movies-tmdb-results'),
    };

    // ── API ──────────────────────────────────────────────────────────────

    async function callAction(action, params = {}) {
        try {
            const res = await fetch(`/api/tabs/movies/action/${action}`, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(params),
            });
            if (!res.ok) {
                const text = (await res.text()).trim();
                console.error(`movies action ${action} failed:`, text);
                return {_error: text || `${res.status} ${res.statusText}`};
            }
            return await res.json();
        } catch (e) {
            console.error('movies callAction error:', e);
            return {_error: e.message};
        }
    }

    // ── Load & render ────────────────────────────────────────────────────

    async function loadMovies() {
        loadRequestId++;
        const myRequestId = loadRequestId;
        const effectiveSort = sortOrder;
        if (filterActor) {
            const result = await callAction('list-movies-by-actor', {actor: filterActor, sort: effectiveSort});
            // Only render if this request is still the latest and we're not in flagged mode
            if (myRequestId === loadRequestId && flaggedViewMode !== 'flagged' && result && result.movies) {
                currentRenderRequestId = myRequestId;
                renderMovieList(result.movies, `🎭 ${filterActor}`);
            }
            return;
        }
        const [moviesResult, tagResult] = await Promise.all([
            callAction('list-movies', {
                tag: (selectedSet ? '' : selectedTag) || '',
                search: searchQuery,
                set_name: selectedSet || '',
                sort: effectiveSort,
                fulltext: fulltextSearch
            }),
            callAction('get-tag-counts', {search: searchQuery, fulltext: fulltextSearch}),
        ]);
        // Only render if this request is still the latest and we're not in flagged mode
        if (myRequestId === loadRequestId && flaggedViewMode !== 'flagged' && moviesResult && moviesResult.movies) {
            const header = selectedSet ? `📦 ${selectedSet}` : null;
            currentRenderRequestId = myRequestId;
            renderMovieList(moviesResult.movies, header);
        }
        if (tagResult && tagResult.counts) {
            renderTagSidebar(tagResult);
        }
    }

    async function loadFlaggedMovies() {
        loadRequestId++;
        const myRequestId = loadRequestId;
        const result = await callAction('list-movies', {sort: sortOrder});
        // Only render if this request is still the latest and we're still in flagged mode
        if (myRequestId === loadRequestId && flaggedViewMode === 'flagged' && result && result.movies) {
            const flagged = result.movies.filter(m => flaggedMovieIds.has(m.id));
            // Always pass headerText when viewing flagged to ensure consistency
            currentRenderRequestId = myRequestId;
            renderMovieList(flagged, `🚩 Flagged (${flaggedMovieIds.size})`);
        }
    }

    function renderMovieList(movies, headerText) {
        el.movieList.innerHTML = '';
        // Show header for collection/actor filter, search, or when headerText is provided
        const shouldShowHeader = headerText || selectedSet || filterActor;
        if (shouldShowHeader) {
            const h = document.createElement('div');
            h.className = 'movies-list-header';
            const displayText = headerText || (selectedSet ? `📦 ${selectedSet}` : (filterActor ? `🎭 ${filterActor}` : ''));

            let buttonText = '✕';
            let buttonTitle = 'Clear filter';
            // When headerText starts with 🚩, we're viewing flagged - show "Clear all" button
            const isFlaggedView = headerText && headerText.startsWith('🚩');
            if (isFlaggedView) {
                buttonText = 'Clear all';
                buttonTitle = 'Clear all flags and exit flagged view';
            }

            h.innerHTML = `${escHtml(displayText)} <button class="movies-list-header-clear" title="${escHtml(buttonTitle)}">${escHtml(buttonText)}</button>`;
            h.querySelector('.movies-list-header-clear').addEventListener('click', () => {
                if (isFlaggedView) {
                    // Clear all flags and exit flagged view
                    flaggedMovieIds.clear();
                    flaggedViewMode = null;
                    updateFlaggedButtonDisplay();
                    loadMovies();
                } else if (selectedSet || filterActor) {
                    // Clear collection/actor filter but preserve search
                    selectedSet = null;
                    filterActor = null;
                    sortOrder = 'title';
                    updateSortButtons();
                    loadMovies();
                } else {
                    // No collection/actor filter, so clear search instead
                    exitFilteredView();
                }
            });
            el.movieList.appendChild(h);
        }
        if (!movies || movies.length === 0) {
            const emptyDiv = document.createElement('div');
            emptyDiv.className = 'movies-empty';
            emptyDiv.textContent = 'No movies found.';
            el.movieList.appendChild(emptyDiv);
            focusedListIdx = -1;
            focusedListMovieId = null;
            return;
        }

        // In unfiltered view, optionally collapse multi-movie collections into a single row.
        const isFiltered = !!(selectedSet || filterActor);
        const items = (isFiltered || !groupCollections)
            ? movies.map(m => ({type: 'movie', movie: m}))
            : collapseCollections(movies);

        items.forEach(item => {
            if (item.type === 'collection') {
                el.movieList.appendChild(renderCollectionCard(item));
            } else {
                el.movieList.appendChild(renderMovieCard(item.movie));
            }
        });

        // Re-apply keyboard focus highlight after re-render.
        // Prefer the previously focused movie ID, then the selected (open) movie.
        if (focusedPanel === 'list') {
            const cards = listCards();
            let idx = focusedListMovieId != null
                ? cards.findIndex(c => c.dataset.id == focusedListMovieId)
                : -1;
            if (idx < 0) idx = cards.findIndex(c => c.dataset.id == selectedMovieId);
            setListFocused(idx >= 0 ? idx : -1);
        } else {
            // Not in list panel — reset index but scroll the selected card back into view.
            focusedListIdx = -1;
            if (selectedMovieId != null) {
                const cards = listCards();
                const idx = cards.findIndex(c => c.dataset.id == selectedMovieId);
                if (idx >= 0) cards[idx].scrollIntoView({block: 'nearest'});
            }
        }
    }

    // collapseCollections groups movies that share a set_name (2+ members) into
    // collection items, preserving sort order for solo movies.
    function collapseCollections(movies) {
        // Count occurrences of each set name
        const setCounts = {};
        movies.forEach(m => {
            if (m.set_name) setCounts[m.set_name] = (setCounts[m.set_name] || 0) + 1;
        });

        const items = [];
        const seenSets = new Set();
        movies.forEach(m => {
            if (m.set_name && setCounts[m.set_name] >= 2) {
                if (!seenSets.has(m.set_name)) {
                    seenSets.add(m.set_name);
                    const members = movies.filter(x => x.set_name === m.set_name);
                    // Use oldest movie (by release year) as poster
                    const oldest = members.reduce((a, b) => {
                        const aYear = a.year || 9999;
                        const bYear = b.year || 9999;
                        return aYear < bYear ? a : b;
                    });
                    items.push({type: 'collection', setName: m.set_name, movies: members, poster: oldest});
                }
            } else {
                items.push({type: 'movie', movie: m});
            }
        });

        // Re-sort so collection cards use their collection name as the sort key,
        // not the sort_title of their first member (which may differ, e.g. "007…").
        // Only needed for title sort; other sorts use the backend order as-is.
        if (sortOrder === 'title') {
            const sortKey = s => {
                const k = s.replace(/^the\s+/i, '');
                return k;
            };
            items.sort((a, b) => {
                const keyA = sortKey(a.type === 'collection'
                    ? a.setName.toLowerCase()
                    : (a.movie.sort_title || a.movie.title || '').toLowerCase());
                const keyB = sortKey(b.type === 'collection'
                    ? b.setName.toLowerCase()
                    : (b.movie.sort_title || b.movie.title || '').toLowerCase());
                return keyA < keyB ? -1 : keyA > keyB ? 1 : 0;
            });
        }

        return items;
    }

    function renderCollectionCard(item) {
        const card = document.createElement('div');
        card.className = 'movies-card movies-card-collection';

        // Use the oldest movie (if provided in poster) or first movie for thumbnail
        const thumbMovie = item.poster || item.movies[0];
        const thumbInner = thumbMovie.poster_url
            ? `<img src="${escHtml(thumbMovie.poster_url)}" alt="" loading="lazy">`
            : '📦';

        // Year range
        const years = item.movies.map(m => m.year).filter(Boolean).map(Number);
        const yearRange = years.length
            ? (Math.min(...years) === Math.max(...years)
                ? String(Math.min(...years))
                : `${Math.min(...years)}–${Math.max(...years)}`)
            : '';
        const count = `${item.movies.length} films`;
        const meta = [yearRange, count].filter(Boolean).join(' · ');

        // Most recent last-played across all members
        const lastPlayed = item.movies
            .map(m => m.last_played).filter(Boolean)
            .sort().reverse()[0] || '';
        const elapsed = lastPlayed ? formatElapsed(lastPlayed) : '';

        card.innerHTML = `
            <div class="movies-card-thumb">${thumbInner}</div>
            <div class="movies-card-info">
                <div class="movies-card-collection-icon">📦</div>
                <div class="movies-card-title">${escHtml(item.setName)}</div>
                ${meta ? `<div class="movies-card-meta">${escHtml(meta)}</div>` : ''}
                ${elapsed ? `<div class="movies-card-elapsed">${escHtml(elapsed)}</div>` : ''}
            </div>
        `;
        card.addEventListener('click', () => {
            lastSelectedSetName = item.setName;
            selectedSet = item.setName;
            filterActor = null;
            if (sortOrder === 'title' || sortOrder === 'last') {
                sortOrder = 'year';
                updateSortButtons();
            }
            loadMovies();
        });
        return card;
    }

    function renderMovieCard(m) {
        const card = document.createElement('div');
        card.className = 'movies-card' + (m.id === selectedMovieId ? ' active' : '');
        card.dataset.id = m.id;

        const thumbInner = m.poster_url
            ? `<img src="${escHtml(m.poster_url)}" alt="" loading="lazy">`
            : '🎬';
        const year = m.year ? m.year : '';
        const runtime = m.runtime ? formatRuntime(m.runtime) : '';
        const elapsed = m.last_played ? formatElapsed(m.last_played) : '';
        const meta = [year, runtime].filter(Boolean).join(' · ');

        card.innerHTML = `
            <div class="movies-card-thumb">${thumbInner}</div>
            <div class="movies-card-info">
                <div class="movies-card-title">${escHtml(m.title)}</div>
                ${meta ? `<div class="movies-card-meta">${escHtml(meta)}</div>` : ''}
                ${elapsed ? `<div class="movies-card-elapsed">${escHtml(elapsed)}</div>` : ''}
            </div>
        `;
        card.addEventListener('click', () => selectMovie(m.id));
        return card;
    }

    function renderTagSidebar(tagResult) {
        el.tagList.innerHTML = '';
        const tagCounts = tagResult.counts || tagResult || [];
        const total = tagResult.total ?? '';

        const allItem = document.createElement('div');
        allItem.className = 'movies-tag-item' + (!selectedTag ? ' active' : '');
        allItem.innerHTML = `
            <span class="movies-tag-label">All</span>
            ${total !== '' ? `<span class="movies-tag-count">${total}</span>` : ''}
        `;
        allItem.addEventListener('click', () => {
            selectedTag = null;
            loadMovies();
        });
        el.tagList.appendChild(allItem);

        tagCounts.forEach(tc => {
            const item = document.createElement('div');
            item.className = 'movies-tag-item' + (selectedTag === tc.tag ? ' active' : '');
            item.innerHTML = `
                <span class="movies-tag-label">${escHtml(tc.tag)}</span>
                <span class="movies-tag-count">${tc.count}</span>
            `;
            item.addEventListener('click', () => {
                selectedTag = tc.tag;
                loadMovies();
            });
            el.tagList.appendChild(item);
        });
    }

    async function selectMovie(id) {
        selectedMovieId = id;
        // Update active card
        document.querySelectorAll('.movies-card').forEach(c => {
            c.classList.toggle('active', c.dataset.id == id);
        });
        el.editBtn.style.display = '';
        el.deleteBtn.style.display = '';
        el.flagBtn.style.display = '';

        // Update flag button text
        el.flagBtn.textContent = flaggedMovieIds.has(id) ? '🚩 Flagged' : '🚩 Flag';

        // Update flag button display in detail toolbar (grid view)
        if (el.detailToolbarFlag) {
            el.detailToolbarFlag.textContent = flaggedMovieIds.has(id) ? '🚩 Flagged' : '🚩 Flag';
        }

        // Only remove grid-mode if we're in list view (not already in grid mode)
        if (!el.container.classList.contains('movies-grid-mode')) {
            el.container.classList.add('movie-selected');
        }

        const m = await callAction('get-movie', {id});
        if (m && !m._error) {
            renderMovieDetail(m);
        }
    }

    function renderMovieDetail(m) {
        // Ensure detail panel is visible
        el.detailPanel.style.display = '';
        el.detail.style.display = '';
        
        const poster = m.poster_url
            ? `<img class="movies-detail-poster" src="${escHtml(m.poster_url)}" alt="">`
            : `<div class="movies-detail-poster-placeholder">🎬</div>`;

        const posterWrap = `
            <div class="movies-detail-poster-wrap" title="Click to upload a new poster image">
                ${poster}
                <div class="movies-poster-upload-overlay">📷</div>
            </div>`;

        const year = m.year ? m.year : '';
        const runtime = m.runtime ? formatRuntime(m.runtime) : '';
        const rating = m.rating ? `<span class="movies-rating-star">⭐</span> ${Number(m.rating).toFixed(1)}/10` : '';
        const metaParts = [year, runtime, rating].filter(Boolean);

        const tags = (m.tags || []).map(t =>
            `<span class="movies-tag-chip" data-tag="${escHtml(t)}">${escHtml(t)}</span>`
        ).join('');

        const actors = (m.actors || []).map(a => {
            const role = a.role ? `<div class="movies-actor-chip-role">${escHtml(a.role)}</div>` : '';
            return `<div class="movies-actor-chip" data-actor="${escHtml(a.name)}" title="Show all films with ${escHtml(a.name)}">${escHtml(a.name)}${role}</div>`;
        }).join('');

        const imdbLink = m.imdb_id
            ? `<a href="https://www.imdb.com/title/${escHtml(m.imdb_id)}" target="_blank" rel="noopener" style="opacity:0.7;font-size:12px;">IMDb ↗</a>`
            : '';
        const tmdbLink = m.tmdb_id
            ? `<a href="https://www.themoviedb.org/movie/${escHtml(m.tmdb_id)}" target="_blank" rel="noopener" style="opacity:0.7;font-size:12px;">TMDB ↗</a>`
            : '';

        const setRow = m.set_name
            ? `<div class="movies-detail-set"><span class="movies-detail-section-title">Collection</span> <span class="movies-set-link" data-set="${escHtml(m.set_name)}">${escHtml(m.set_name)}</span></div>`
            : '';
        const lastPlayedRow = m.last_played
            ? `<div class="movies-detail-lastplayed"><span class="movies-detail-section-title">Last watched</span> ${escHtml(formatElapsed(m.last_played))} <span style="opacity:0.5;font-size:12px;">(${escHtml(m.last_played.slice(0, 10))})</span></div>`
            : '';

        el.detail.innerHTML = `
            <div class="movies-detail-header">
                ${posterWrap}
                <div class="movies-detail-header-info">
                    <div class="movies-detail-title">${escHtml(m.title)}</div>
                    ${metaParts.length ? `<div class="movies-detail-meta">${metaParts.join('<span style="opacity:0.3"> · </span>')}</div>` : ''}
                    ${m.tagline ? `<div class="movies-detail-tagline">${escHtml(m.tagline)}</div>` : ''}
                    <div style="display:flex;gap:10px;">${imdbLink}${tmdbLink}</div>
                </div>
            </div>
            ${setRow}
            ${lastPlayedRow}
            ${m.plot ? `<div class="movies-detail-plot">${escHtml(m.plot)}</div>` : ''}
            ${tags ? `<div><div class="movies-detail-section-title">Tags</div><div class="movies-tags-row">${tags}</div></div>` : ''}
            ${actors ? `<div><div class="movies-detail-section-title">Cast</div><div class="movies-actor-list">${actors}</div></div>` : ''}
        `;

        // Tag chip click → filter by tag
        el.detail.querySelectorAll('.movies-tag-chip[data-tag]').forEach(chip => {
            chip.addEventListener('click', () => {
                selectedTag = chip.dataset.tag;
                selectedSet = null;
                filterActor = null;
                loadMovies();
            });
        });

        // Set link click → filter by collection
        el.detail.querySelectorAll('.movies-set-link[data-set]').forEach(link => {
            link.addEventListener('click', () => {
                lastSelectedSetName = link.dataset.set;
                selectedSet = link.dataset.set;
                filterActor = null;
                if (sortOrder === 'title' || sortOrder === 'last') {
                    sortOrder = 'year';
                    updateSortButtons();
                }
                loadMovies();
            });
        });

        // Actor chip click → filter by actor
        el.detail.querySelectorAll('.movies-actor-chip[data-actor]').forEach(chip => {
            chip.addEventListener('click', () => {
                filterActor = chip.dataset.actor;
                selectedSet = null;
                if (sortOrder === 'title' || sortOrder === 'last') {
                    sortOrder = 'year';
                    updateSortButtons();
                }
                loadMovies();
            });
        });

        // Poster click → trigger image upload
        const posterWrapEl = el.detail.querySelector('.movies-detail-poster-wrap');
        if (posterWrapEl && el.posterUpload) {
            posterWrapEl.addEventListener('click', () => {
                el.posterUpload.dataset.movieUuid = m.uuid;
                el.posterUpload.value = '';
                el.posterUpload.click();
            });
        }
    }

    // ── Grid View ────────────────────────────────────────────────────────

    function switchViewMode(newMode) {
        viewMode = newMode;
        if (newMode === 'grid') {
            // Push current state to navigation stack
            navigationStack = [{viewMode: 'grid', context: {selectedSet, selectedTag, filterActor, searchQuery}}];
            el.container.classList.add('movies-grid-mode');
            el.container.classList.remove('movies-detail-view');
            el.listPanel.style.display = 'none';
            el.tagSidebar.style.display = 'none';
            el.gridPanel.style.display = 'flex';
            el.gridPanel.style.height = '100%';
            el.gridPreview.style.display = 'flex';
            el.detail.style.display = 'none';
            el.detailToolbar.style.display = 'none';
            renderGridView();
        } else {
            // Back to list view
            navigationStack = [];
            flaggedViewMode = null;  // Reset flagged view when switching to list
            el.container.classList.remove('movies-grid-mode');
            el.container.classList.remove('movies-detail-view');
            el.gridPanel.style.display = 'none';
            el.gridPanel.style.height = '';
            el.gridPreview.style.display = 'none';
            el.detailPanel.style.display = 'none';
            el.detail.style.display = 'none';
            el.detailToolbar.style.display = 'none';
            el.tagSidebar.style.display = '';
            el.listPanel.style.display = '';
            loadMovies();
        }
    }

    async function renderGridView() {
        // Force year sorting when inside a collection in grid view
        const effectiveSort = selectedSet ? 'year' : sortOrder;
        let movies;

        // Set grid columns based on current column count
        if (el.grid) {
            el.grid.style.gridTemplateColumns = `repeat(${gridColumns}, minmax(0, 1fr))`;
            // Set row height accounting for aspect ratio (2:3) and gaps
            // Container is roughly 1200px, with 12px padding on each side and 12px gaps between columns
            const containerWidth = el.grid.offsetWidth || 1200;
            const totalGapWidth = (gridColumns - 1) * 12 + 24; // gaps + padding
            const availableWidth = containerWidth - totalGapWidth;
            const columnWidth = availableWidth / gridColumns;
            // Height = width * 1.5 for 2:3 aspect ratio, plus 2% buffer to prevent overlap
            const rowHeight = Math.round(columnWidth * 1.5 * 1.02);
            el.grid.style.gridAutoRows = `${rowHeight}px`;
        }

        // Load movies based on current filters
        if (filterActor) {
            const result = await callAction('list-movies-by-actor', {actor: filterActor, sort: effectiveSort});
            movies = result && result.movies ? result.movies : [];
        } else {
            const result = await callAction('list-movies', {
                tag: (selectedSet ? '' : selectedTag) || '',
                search: searchQuery,
                set_name: selectedSet || '',
                sort: effectiveSort,
                fulltext: fulltextSearch
            });
            movies = result && result.movies ? result.movies : [];
        }

        // Update header text
        let headerText = '';
        if (selectedSet) {
            headerText = `📦 ${selectedSet}`;
        } else if (selectedTag) {
            headerText = `🏷️ ${selectedTag}`;
        } else if (filterActor) {
            headerText = `🎭 ${filterActor}`;
        } else if (searchQuery) {
            headerText = `🔍 ${searchQuery}`;
        } else {
            headerText = 'All Movies';
        }
        el.gridHeaderText.textContent = headerText;

        // Update back button visibility
        el.gridBackBtn.style.display = navigationStack.length > 1 ? '' : 'none';

        // Organize movies into collections and singles
        const items = organizeIntoGridItems(movies);

        // Render grid
        el.grid.innerHTML = '';
        items.forEach(item => {
            const poster = createGridPoster(item);
            el.grid.appendChild(poster);
        });

        // Ensure preview panel is visible and clear any previous content
        el.gridPreview.style.display = '';
        hideGridPreview();
    }

    function organizeIntoGridItems(movies) {
        // Don't group into collections if we're already filtering by a specific collection
        if (selectedSet) {
            // Sort by release year when inside a collection
            const sorted = [...movies].sort((a, b) => {
                const aYear = a.year || 9999;
                const bYear = b.year || 9999;
                return aYear - bYear;
            });
            return sorted.map(m => ({type: 'movie', movie: m}));
        }

        // In grid view, show collections as grouped items (only if 2+ movies in set)
        const setCounts = {};
        movies.forEach(m => {
            if (m.set_name) setCounts[m.set_name] = (setCounts[m.set_name] || 0) + 1;
        });

        const items = [];
        const seenSets = new Set();
        movies.forEach(m => {
            if (m.set_name && setCounts[m.set_name] >= 2) {
                if (!seenSets.has(m.set_name)) {
                    seenSets.add(m.set_name);
                    const members = movies.filter(x => x.set_name === m.set_name);
                    // Use oldest movie (by release year) as poster
                    const oldest = members.reduce((a, b) => {
                        const aYear = a.year || 9999;
                        const bYear = b.year || 9999;
                        return aYear < bYear ? a : b;
                    });
                    items.push({type: 'collection', setName: m.set_name, movies: members, poster: oldest});
                }
            } else {
                items.push({type: 'movie', movie: m});
            }
        });

        // Sort items by title to maintain consistent order in grid view
        if (sortOrder === 'title') {
            const sortKey = s => {
                const k = s.replace(/^the\s+/i, '');
                return k;
            };
            items.sort((a, b) => {
                const keyA = sortKey(a.type === 'collection'
                    ? a.setName.toLowerCase()
                    : (a.movie.sort_title || a.movie.title || '').toLowerCase());
                const keyB = sortKey(b.type === 'collection'
                    ? b.setName.toLowerCase()
                    : (b.movie.sort_title || b.movie.title || '').toLowerCase());
                return keyA < keyB ? -1 : keyA > keyB ? 1 : 0;
            });
        }

        return items;
    }

    function createGridPoster(item) {
        const card = document.createElement('div');
        card.className = 'movies-grid-poster' + (item.type === 'collection' ? ' collection' : '');

        const posterUrl = item.type === 'collection' ? item.poster.poster_url : item.movie.poster_url;

        // Create poster image/placeholder
        if (posterUrl) {
            const imgContainer = document.createElement('div');
            imgContainer.className = 'movies-grid-poster-image-container';
            const img = document.createElement('img');
            img.className = 'movies-grid-poster-image';
            img.src = posterUrl;
            img.alt = '';
            imgContainer.appendChild(img);
            card.appendChild(imgContainer);
        } else {
            const placeholder = document.createElement('div');
            placeholder.className = 'movies-grid-poster-placeholder';
            placeholder.textContent = item.type === 'collection' ? '📦' : '🎬';
            card.appendChild(placeholder);
        }

        card.addEventListener('mouseover', () => {
            showGridPreview(item);
        });

        card.addEventListener('mouseout', () => {
            hideGridPreview();
        });

        card.addEventListener('click', () => {
            if (item.type === 'collection') {
                // Click on collection → filter to show only that collection's movies
                navigationStack.push({viewMode: 'grid', context: {selectedSet, selectedTag, filterActor, searchQuery}});
                selectedSet = item.setName;
                selectedTag = null;
                filterActor = null;
                renderGridView();
            } else {
                // Click on movie → show details
                navigationStack.push({viewMode: 'grid', context: {selectedSet, selectedTag, filterActor, searchQuery}});
                switchViewToMovieDetail(item.movie.id);
            }
        });

        return card;
    }

    function showGridPreview(item) {
        const preview = el.gridPreviewContent;
        let text = '';

        if (item.type === 'collection') {
            const years = item.movies.map(m => m.year).filter(Boolean).map(Number);
            const minYear = years.length > 0 ? Math.min(...years) : 'N/A';
            const maxYear = years.length > 0 ? Math.max(...years) : 'N/A';
            const lastWatched = item.movies
                .filter(m => m.last_played)
                .map(m => new Date(m.last_played).getTime())
                .reduce((max, current) => Math.max(max, current), 0);
            const lastWatchedText = lastWatched > 0 ? formatElapsed(new Date(lastWatched).toISOString()) : 'Never';
            const yearRange = minYear !== maxYear ? `${minYear}–${maxYear}` : minYear;

            text = `📦 ${escHtml(item.setName)} · ${item.movies.length} movie${item.movies.length !== 1 ? 's' : ''} · ${yearRange} · ${escHtml(lastWatchedText)}`;
        } else {
            const m = item.movie;
            const year = m.year ? m.year : 'N/A';
            const elapsed = m.last_played ? formatElapsed(m.last_played) : 'Never';

            text = `${escHtml(m.title)} · ${year} · ${escHtml(elapsed)}`;
        }

        preview.textContent = text;
        el.gridPreview.style.display = '';
    }

    function hideGridPreview() {
        el.gridPreviewContent.textContent = '';
    }

    function switchViewToMovieDetail(movieId) {
        // Show detail panel full-screen while staying in grid mode
        selectedMovieId = movieId;

        // Hide grid and sidebar, show detail panel with full-width layout
        el.gridPanel.style.display = 'none';
        el.tagSidebar.style.display = 'none';
        el.detailPanel.style.display = '';
        el.detail.style.display = '';

        // Add detail view mode to change grid layout
        el.container.classList.add('movies-grid-mode');
        el.container.classList.add('movies-detail-view');

        // Show toolbar
        el.detailToolbar.style.display = 'flex';

        // Hide grid back button since we're in detail view
        if (el.gridBackBtn) {
            el.gridBackBtn.style.display = 'none';
        }

        // Reset navigationStack so back button doesn't show when we return
        navigationStack = [];

        // Load movie details
        selectMovie(movieId);
    }

    async function navigateBack() {
        // If detail panel is showing, hide it and go back to grid
        if (el.detailPanel.style.display !== 'none' && el.detail.style.display !== 'none') {
            // Hide toolbar
            el.detailToolbar.style.display = 'none';

            el.detailPanel.style.display = 'none';
            el.detail.style.display = 'none';
            // Remove inline styles and let CSS control the display
            el.gridPanel.style.display = '';
            el.tagSidebar.style.display = '';

            // When exiting detail view, clear navigationStack (we're back at main grid, no sub-navigation)
            navigationStack = [];

            // Explicitly hide the grid back button when returning from detail view
            if (el.gridBackBtn) {
                el.gridBackBtn.style.display = 'none';
            }

            // Ensure movies-grid-mode class is still on container
            if (!el.container.classList.contains('movies-grid-mode')) {
                el.container.classList.add('movies-grid-mode');
            }
            // Remove detail view mode to restore grid layout
            el.container.classList.remove('movies-detail-view');

            selectedMovieId = null;
            // Re-render the grid to restore proper layout
            await renderGridView();
            return;
        }

        // Otherwise pop from navigation stack
        if (navigationStack.length <= 1) return;
        navigationStack.pop();
        const prevState = navigationStack[navigationStack.length - 1];
        selectedSet = prevState.context.selectedSet;
        selectedTag = prevState.context.selectedTag;
        filterActor = prevState.context.filterActor;
        searchQuery = prevState.context.searchQuery;
        if (el.search) el.search.value = searchQuery;
        if (el.searchClear) el.searchClear.style.display = searchQuery ? '' : 'none';
        renderGridView();
    }

    // ── Edit modal ───────────────────────────────────────────────────────

    function openEditModal(m) {
        editingMovieId = m ? m.id : null;
        el.modalTitle.textContent = m ? 'Edit Movie' : 'New Movie';
        el.editTitle.value = m ? (m.title || '') : '';
        el.editSortTitle.value = m ? (m.sort_title || '') : '';
        el.editYear.value = m ? (m.year || '') : '';
        el.editRuntime.value = m ? (m.runtime || '') : '';
        el.editRating.value = m ? (m.rating || '') : '';
        el.editTagline.value = m ? (m.tagline || '') : '';
        el.editPlot.value = m ? (m.plot || '') : '';
        el.editTmdbId.value = m ? (m.tmdb_id || '') : '';
        el.editImdbId.value = m ? (m.imdb_id || '') : '';
        el.editPosterUrl.value = m ? (m.poster_url || '') : '';
        el.editTags.value = m ? ((m.tags || []).join(', ')) : '';
        el.editActors.value = m
            ? ((m.actors || []).map(a => a.role ? `${a.name} | ${a.role}` : a.name).join('\n'))
            : '';
        if (el.editSetName) el.editSetName.value = m ? (m.set_name || '') : '';
        if (el.editLastPlayed) el.editLastPlayed.value = m ? (m.last_played ? m.last_played.slice(0, 10) : '') : '';
        el.modalOverlay.style.display = 'flex';
        el.editTitle.focus();
    }

    function closeEditModal() {
        el.modalOverlay.style.display = 'none';
    }

    async function saveMovie() {
        const tags = el.editTags.value
            .split(',')
            .map(t => t.trim())
            .filter(Boolean);

        const actors = el.editActors.value
            .split('\n')
            .map(line => line.trim())
            .filter(Boolean)
            .map((line, i) => {
                const parts = line.split('|');
                return {
                    name: parts[0].trim(),
                    role: parts[1] ? parts[1].trim() : '',
                    order: i,
                };
            })
            .filter(a => a.name);

        const params = {
            title: el.editTitle.value.trim(),
            sort_title: el.editSortTitle.value.trim(),
            year: Number(el.editYear.value) || 0,
            runtime: Number(el.editRuntime.value) || 0,
            rating: Number(el.editRating.value) || 0,
            tagline: el.editTagline.value.trim(),
            plot: el.editPlot.value.trim(),
            tmdb_id: el.editTmdbId.value.trim(),
            imdb_id: el.editImdbId.value.trim(),
            poster_url: el.editPosterUrl.value.trim(),
            set_name: el.editSetName ? el.editSetName.value.trim() : '',
            last_played: el.editLastPlayed ? el.editLastPlayed.value.trim() : '',
            tags,
            actors,
        };

        if (!params.title) {
            alert('Title is required.');
            return;
        }

        let result;
        if (editingMovieId) {
            result = await callAction('update-movie', {...params, id: editingMovieId});
        } else {
            result = await callAction('create-movie', params);
        }

        if (result && result._error) {
            alert('Save failed: ' + result._error);
            return;
        }

        closeEditModal();
        await loadMovies();
        if (result && result.id) {
            selectMovie(result.id);
            // Scroll the card back into view without stealing keyboard focus.
            const cards = listCards();
            const idx = cards.findIndex(c => c.dataset.id == result.id);
            if (idx >= 0) cards[idx].scrollIntoView({block: 'nearest'});
        }
    }

    // ── Delete ───────────────────────────────────────────────────────────

    async function deleteMovie() {
        if (!selectedMovieId) return;
        if (!confirm('Delete this movie?')) return;

        const result = await callAction('delete-movie', {id: selectedMovieId});
        if (result && result._error) {
            alert('Delete failed: ' + result._error);
            return;
        }

        selectedMovieId = null;
        el.editBtn.style.display = 'none';
        el.deleteBtn.style.display = 'none';
        el.detail.innerHTML = '';
        el.container && el.container.classList.remove('movie-selected');
        await loadMovies();
    }

    // ── NFO import ───────────────────────────────────────────────────────

    async function importNFO(fileList) {
        const files = [];
        for (const f of fileList) {
            const content = await f.text();
            files.push({name: f.name, content});
        }

        const result = await callAction('import-nfo', {files});
        if (result && result._error) {
            alert('Import failed: ' + result._error);
            return;
        }
        if (result) {
            const msgs = [`Imported: ${(result.imported || []).length}`, `Skipped: ${result.skipped || 0}`];
            if (result.errors && result.errors.length) {
                msgs.push(`Errors: ${result.errors.join('; ')}`);
            }
            alert(msgs.join('\n'));
        }
        await loadMovies();
    }

    // ── TMDB search ──────────────────────────────────────────────────────

    function openTMDBSearch() {
        el.tmdbQuery.value = '';
        el.tmdbYear.value = '';
        el.tmdbResults.innerHTML = '';
        el.tmdbModalOverlay.style.display = 'flex';
        el.tmdbQuery.focus();
    }

    function closeTMDBModal() {
        el.tmdbModalOverlay.style.display = 'none';
    }

    async function searchTMDB() {
        const query = el.tmdbQuery.value.trim();
        if (!query) return;
        const year = el.tmdbYear.value.trim();

        el.tmdbResults.innerHTML = '<div style="opacity:0.5;font-size:13px;">Searching…</div>';
        const result = await callAction('search-tmdb', {query, year});

        if (result && result.error) {
            el.tmdbResults.innerHTML = `<div style="color:var(--error,#ef4444);font-size:13px;">${escHtml(result.error)}</div>`;
            return;
        }
        if (result && result._error) {
            el.tmdbResults.innerHTML = `<div style="color:var(--error,#ef4444);font-size:13px;">${escHtml(result._error)}</div>`;
            return;
        }

        const results = (result && result.results) || [];
        if (results.length === 0) {
            el.tmdbResults.innerHTML = '<div class="movies-empty">No results found.</div>';
            return;
        }

        el.tmdbResults.innerHTML = '';
        results.slice(0, 10).forEach(r => {
            const item = document.createElement('div');
            item.className = 'movies-tmdb-result';
            const poster = r.poster_url
                ? `<img class="movies-tmdb-result-poster" src="${escHtml(r.poster_url)}" alt="" loading="lazy">`
                : `<div class="movies-tmdb-result-poster" style="display:flex;align-items:center;justify-content:center;font-size:24px;">🎬</div>`;
            const year = r.release_date ? r.release_date.slice(0, 4) : '';
            const rating = r.vote_average ? `⭐ ${Number(r.vote_average).toFixed(1)}` : '';
            const overview = r.overview || '';

            item.innerHTML = `
                ${poster}
                <div class="movies-tmdb-result-info">
                    <div class="movies-tmdb-result-title">${escHtml(r.title || '')}</div>
                    <div class="movies-tmdb-result-meta">${escHtml([year, rating].filter(Boolean).join(' · '))}</div>
                    ${overview ? `<div class="movies-tmdb-result-overview">${escHtml(overview)}</div>` : ''}
                </div>
            `;
            item.addEventListener('click', () => importTMDBMovie(String(r.id)));
            el.tmdbResults.appendChild(item);
        });
    }

    async function importTMDBMovie(tmdbId) {
        const result = await callAction('fetch-tmdb', {tmdb_id: tmdbId});
        if (!result || result._error || result.error) {
            alert('Failed to fetch TMDB data: ' + (result._error || result.error || 'unknown error'));
            return;
        }

        // Pre-fill edit modal with TMDB data
        const actors = (result.actors || []).map(a =>
            a.role ? `${a.name} | ${a.role}` : a.name
        ).join('\n');

        const m = {
            id: null,
            title: result.title || '',
            sort_title: result.sort_title || '',
            year: result.year || 0,
            runtime: result.runtime || 0,
            rating: result.rating || 0,
            tagline: result.tagline || '',
            plot: result.plot || '',
            tmdb_id: result.tmdb_id || tmdbId,
            imdb_id: result.imdb_id || '',
            poster_url: result.poster_url || '',
            tags: result.tags || [],
            actors: result.actors || [],
        };

        closeTMDBModal();
        openEditModal(m);
        // Override actors textarea with formatted string
        el.editActors.value = actors;
    }

    // ── Utilities ────────────────────────────────────────────────────────

    function escHtml(str) {
        return String(str)
            .replace(/&/g, '&amp;')
            .replace(/</g, '&lt;')
            .replace(/>/g, '&gt;')
            .replace(/"/g, '&quot;');
    }

    function formatRuntime(minutes) {
        if (!minutes) return '';
        const h = Math.floor(minutes / 60);
        const m = minutes % 60;
        if (h === 0) return `${m}m`;
        if (m === 0) return `${h}h`;
        return `${h}h ${m}m`;
    }

    // formatElapsed returns a 2-significant-field "time ago" string.
    function formatElapsed(dateStr) {
        if (!dateStr) return '';
        try {
            const ts = new Date(dateStr);
            const ms = Date.now() - ts.getTime();
            if (ms < 0) return 'in the future';
            const seconds = Math.floor(ms / 1000);
            const minutes = Math.floor(seconds / 60);
            const hours = Math.floor(minutes / 60);
            const days = Math.floor(hours / 24);
            const months = Math.floor(days / 30);
            const years = Math.floor(days / 365);
            if (years > 0) return `${years}y ${months % 12}mo ago`;
            if (months > 0) return `${months}mo ${days % 30}d ago`;
            if (days > 0) return `${days}d ${hours % 24}h ago`;
            if (hours > 0) return `${hours}h ${minutes % 60}m ago`;
            if (minutes > 0) return `${minutes}m ago`;
            return 'just now';
        } catch {
            return '';
        }
    }

    // ── Poster image upload ───────────────────────────────────────────────

    if (el.posterUpload) {
        el.posterUpload.addEventListener('change', async () => {
            const file = el.posterUpload.files[0];
            const movieUUID = el.posterUpload.dataset.movieUuid;
            if (!file || !movieUUID) return;

            const form = new FormData();
            form.append('uuid', movieUUID);
            form.append('file', file);

            try {
                const res = await fetch('/api/movies/upload-image', {method: 'POST', body: form});
                if (!res.ok) {
                    const msg = (await res.text()).trim();
                    alert('Upload failed: ' + msg);
                    return;
                }
                const data = await res.json();
                // Refresh detail panel with updated poster (force-bust cache)
                const img = el.detail.querySelector('.movies-detail-poster');
                if (img && data.poster_url) {
                    img.src = data.poster_url + '?t=' + Date.now();
                }
                // Also refresh list thumbnails
                loadMovies();
            } catch (e) {
                alert('Upload error: ' + e.message);
            }
        });
    }

    // ── Sort buttons ─────────────────────────────────────────────────────

    function updateSortButtons() {
        document.querySelectorAll('.movies-sort-btn').forEach(btn => {
            btn.classList.toggle('active', btn.dataset.sort === sortOrder);
        });
    }

    document.querySelectorAll('.movies-sort-btn').forEach(btn => {
        btn.addEventListener('click', () => {
            sortOrder = btn.dataset.sort;
            updateSortButtons();
            loadMovies();
        });
    });

    if (el.collapseCheck) {
        el.collapseCheck.addEventListener('change', () => {
            groupCollections = el.collapseCheck.checked;
            loadMovies();
        });
    }

    // Grid view controls
    if (el.viewToggleList) {
        el.viewToggleList.addEventListener('click', () => switchViewMode('grid'));
    }

    if (el.viewToggleGrid) {
        el.viewToggleGrid.addEventListener('click', () => switchViewMode('list'));
    }

    if (el.gridBackBtn) {
        el.gridBackBtn.addEventListener('click', () => navigateBack());
    }

    if (el.gridZoomOut) {
        el.gridZoomOut.addEventListener('click', () => {
            if (gridColumns > 1) {
                gridColumns--;
                el.gridColumnCount.textContent = gridColumns + ' columns';
                saveGridState();
                renderGridView();
            }
        });
    }

    if (el.gridZoomIn) {
        el.gridZoomIn.addEventListener('click', () => {
            gridColumns++;
            el.gridColumnCount.textContent = gridColumns + ' columns';
            saveGridState();
            renderGridView();
        });
    }

    if (el.search) {
        el.search.addEventListener('input', () => {
            clearTimeout(searchDebounce);
            searchQuery = el.search.value.trim();
            if (el.searchClear) el.searchClear.style.display = searchQuery ? '' : 'none';
            searchDebounce = setTimeout(() => loadMovies(), 300);
        });
    }

    if (el.searchClear) {
        el.searchClear.addEventListener('click', () => {
            el.search.value = '';
            searchQuery = '';
            el.searchClear.style.display = 'none';
            loadMovies();
            el.search.focus();
        });
    }

    if (el.fulltextCheck) {
        el.fulltextCheck.addEventListener('change', () => {
            fulltextSearch = el.fulltextCheck.checked;
            loadMovies();
        });
    }

    if (el.newBtn) {
        el.newBtn.addEventListener('click', () => openEditModal(null));
    }

    if (el.editBtn) {
        el.editBtn.addEventListener('click', async () => {
            if (!selectedMovieId) return;
            const m = await callAction('get-movie', {id: selectedMovieId});
            if (m && !m._error) openEditModal(m);
        });
    }

    if (el.deleteBtn) {
        el.deleteBtn.addEventListener('click', () => deleteMovie());
    }

    if (el.flagBtn) {
        el.flagBtn.addEventListener('click', () => {
            if (selectedMovieId) {
                if (flaggedMovieIds.has(selectedMovieId)) {
                    flaggedMovieIds.delete(selectedMovieId);
                    el.flagBtn.textContent = '🚩 Flag';
                } else {
                    flaggedMovieIds.add(selectedMovieId);
                    el.flagBtn.textContent = '🚩 Flagged';
                }
                updateFlaggedButtonDisplay();
            }
        });
    }

    if (el.flaggedBtn) {
        el.flaggedBtn.addEventListener('click', () => {
            if (flaggedViewMode === 'flagged') {
                // Clear the flagged view
                flaggedViewMode = null;
                selectedSet = null;
                filterActor = null;
                searchQuery = '';
                el.search.value = '';
                loadMovies();
            } else {
                // Show flagged movies
                flaggedViewMode = 'flagged';
                selectedSet = null;
                filterActor = null;
                searchQuery = '';
                el.search.value = '';
                loadFlaggedMovies();
            }
        });
    }

    if (el.nfoInput) {
        el.nfoInput.addEventListener('change', async (e) => {
            if (e.target.files && e.target.files.length > 0) {
                await importNFO(e.target.files);
                e.target.value = '';
            }
        });
    }

    if (el.tmdbSearchBtn) {
        el.tmdbSearchBtn.addEventListener('click', () => openTMDBSearch());
    }

    if (el.modalClose) el.modalClose.addEventListener('click', closeEditModal);
    if (el.modalCancel) el.modalCancel.addEventListener('click', closeEditModal);
    if (el.modalSave) el.modalSave.addEventListener('click', saveMovie);

    // Detail toolbar buttons
    if (el.detailToolbarBack) el.detailToolbarBack.addEventListener('click', navigateBack);
    if (el.detailToolbarEdit) {
        el.detailToolbarEdit.addEventListener('click', async () => {
            if (selectedMovieId) {
                const m = await callAction('get-movie', {id: selectedMovieId});
                if (m && !m._error) openEditModal(m);
            }
        });
    }
    if (el.detailToolbarDelete) {
        el.detailToolbarDelete.addEventListener('click', async () => {
            if (selectedMovieId) {
                const confirmed = confirm('Delete this movie?');
                if (confirmed) {
                    await deleteMovie();
                }
            }
        });
    }
    if (el.detailToolbarFlag) {
        el.detailToolbarFlag.addEventListener('click', () => {
            if (selectedMovieId) {
                if (flaggedMovieIds.has(selectedMovieId)) {
                    flaggedMovieIds.delete(selectedMovieId);
                    el.detailToolbarFlag.textContent = '🚩 Flag';
                } else {
                    flaggedMovieIds.add(selectedMovieId);
                    el.detailToolbarFlag.textContent = '🚩 Flagged';
                }
                updateFlaggedButtonDisplay();
            }
        });
    }

    if (el.tmdbModalClose) el.tmdbModalClose.addEventListener('click', closeTMDBModal);
    if (el.tmdbSearchSubmit) el.tmdbSearchSubmit.addEventListener('click', searchTMDB);
    if (el.tmdbQuery) {
        el.tmdbQuery.addEventListener('keydown', e => {
            if (e.key === 'Enter') searchTMDB();
        });
    }

    // Close modals on overlay click
    if (el.modalOverlay) {
        el.modalOverlay.addEventListener('click', e => {
            if (e.target === el.modalOverlay) closeEditModal();
        });
    }
    if (el.tmdbModalOverlay) {
        el.tmdbModalOverlay.addEventListener('click', e => {
            if (e.target === el.tmdbModalOverlay) closeTMDBModal();
        });
    }

    // ── Keyboard navigation ──────────────────────────────────────────────

    // exitFilteredView clears any active collection/actor filter and returns to
    // the main list, restoring focus to the collection card that was entered.
    function exitFilteredView() {
        const wasSet = selectedSet;
        filterActor = null;
        selectedSet = null;
        sortOrder = 'title';
        updateSortButtons();
        loadMovies().then(() => {
            if (wasSet) {
                const cards = listCards();
                const idx = cards.findIndex(c => c.classList.contains('movies-card-collection') &&
                    c.querySelector('.movies-card-title')?.textContent === wasSet);
                if (idx >= 0) setListFocused(idx);
            }
        });
    }

    function listCards() {
        return Array.from(el.movieList.querySelectorAll('.movies-card'));
    }

    function tagItems() {
        return Array.from(el.tagList.querySelectorAll('.movies-tag-item'));
    }

    // Returns how many items fit in the visible height of the scroll container.
    function pageItemCount(container, firstItem) {
        if (!container || !firstItem) return 10;
        const itemH = firstItem.getBoundingClientRect().height || 1;
        const visibleH = container.clientHeight || 400;
        return Math.max(1, Math.floor(visibleH / itemH));
    }

    function setListFocused(idx) {
        const cards = listCards();
        focusedListIdx = idx;
        focusedListMovieId = (idx >= 0 && idx < cards.length) ? (cards[idx].dataset.id || null) : null;
        cards.forEach((c, i) => c.classList.toggle('kbd-focused', i === idx));
        if (idx >= 0 && idx < cards.length) {
            cards[idx].scrollIntoView({block: 'nearest'});
        }
    }

    function setTagFocused(idx) {
        const items = tagItems();
        focusedTagIdx = idx;
        items.forEach((el, i) => el.classList.toggle('kbd-focused', i === idx));
        if (idx >= 0 && idx < items.length) {
            items[idx].scrollIntoView({block: 'nearest'});
        }
    }

    function setFocusedPanel(panel) {
        focusedPanel = panel;
        if (panel === 'list') {
            setTagFocused(-1);
            const cards = listCards();
            // Try to restore by movie identity (survives tag-change reloads),
            // then fall back to the selected movie, then the first card.
            let idx = -1;
            if (focusedListMovieId != null) {
                idx = cards.findIndex(c => c.dataset.id == focusedListMovieId);
            }
            if (idx < 0) idx = cards.findIndex(c => c.dataset.id == selectedMovieId);
            if (idx < 0 && cards.length > 0) idx = 0;
            setListFocused(idx);
        } else {
            setListFocused(-1);
            // Restore cursor to active tag
            const items = tagItems();
            const idx = items.findIndex(i => i.classList.contains('active'));
            setTagFocused(idx >= 0 ? idx : 0);
        }
    }

    function isModalOpen() {
        return (el.modalOverlay && el.modalOverlay.style.display !== 'none') ||
            (el.tmdbModalOverlay && el.tmdbModalOverlay.style.display !== 'none');
    }

    function updateFlaggedButtonDisplay() {
        if (el.flaggedBtn) {
            el.flaggedBtn.textContent = `🚩 Flagged (${flaggedMovieIds.size})`;
        }
    }

    document.addEventListener('keydown', async (e) => {
        if (isModalOpen()) return;
        if (e.target.tagName === 'TEXTAREA' || e.target.tagName === 'INPUT') {
            e.stopPropagation();
            return;
        }

        // ArrowLeft: exit collection/actor filter if active, otherwise move to tag sidebar
        if (e.key === 'ArrowLeft' && focusedPanel === 'list') {
            e.preventDefault();
            e.stopPropagation();
            if (selectedSet || filterActor) {
                exitFilteredView();
            } else {
                setFocusedPanel('tags');
            }
            return;
        }

        // ArrowRight: move focus back to list
        if (e.key === 'ArrowRight' && focusedPanel === 'tags') {
            e.preventDefault();
            e.stopPropagation();
            setFocusedPanel('list');
            return;
        }

        // ArrowUp / ArrowDown: navigate within panel
        if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
            e.preventDefault();
            const dir = e.key === 'ArrowUp' ? -1 : 1;

            if (focusedPanel === 'tags') {
                const items = tagItems();
                if (!items.length) {
                    e.stopPropagation();
                    return;
                }
                const cur = focusedTagIdx < 0 ? 0 : focusedTagIdx;
                if (dir === -1 && cur <= 0) {
                    // At top — clear focus and let app.js return to tab bar
                    setTagFocused(-1);
                    return; // no stopPropagation
                }
                e.stopPropagation();
                setTagFocused(Math.min(cur + dir, items.length - 1));
            } else {
                const cards = listCards();
                if (!cards.length) {
                    e.stopPropagation();
                    return;
                }
                const cur = focusedListIdx < 0 ? 0 : focusedListIdx;
                if (dir === -1 && cur <= 0) {
                    // At top — clear focus and let app.js return to tab bar
                    setListFocused(-1);
                    return; // no stopPropagation
                }
                e.stopPropagation();
                setListFocused(Math.min(cur + dir, cards.length - 1));
            }
            return;
        }

        // PageUp / PageDown / Home / End
        if (e.key === 'PageUp' || e.key === 'PageDown' || e.key === 'Home' || e.key === 'End') {
            e.preventDefault();
            e.stopPropagation();

            if (focusedPanel === 'tags') {
                const items = tagItems();
                if (!items.length) return;
                if (e.key === 'Home') {
                    setTagFocused(0);
                    return;
                }
                if (e.key === 'End') {
                    setTagFocused(items.length - 1);
                    return;
                }
                const pageSize = pageItemCount(el.tagList, items[0]);
                const cur = focusedTagIdx < 0 ? 0 : focusedTagIdx;
                const next = e.key === 'PageUp'
                    ? Math.max(0, cur - pageSize)
                    : Math.min(items.length - 1, cur + pageSize);
                setTagFocused(next);
            } else {
                const cards = listCards();
                if (!cards.length) return;
                if (e.key === 'Home') {
                    setListFocused(0);
                    return;
                }
                if (e.key === 'End') {
                    setListFocused(cards.length - 1);
                    return;
                }
                const pageSize = pageItemCount(el.movieList, cards[0]);
                const cur = focusedListIdx < 0 ? 0 : focusedListIdx;
                const next = e.key === 'PageUp'
                    ? Math.max(0, cur - pageSize)
                    : Math.min(cards.length - 1, cur + pageSize);
                setListFocused(next);
            }
            return;
        }

        // Enter: commit the keyboard-focused selection
        if (e.key === 'Enter') {
            if (focusedPanel === 'tags') {
                const items = tagItems();
                if (focusedTagIdx < 0 || focusedTagIdx >= items.length) return;
                e.preventDefault();
                e.stopPropagation();
                items[focusedTagIdx].click();
            } else {
                const cards = listCards();
                if (focusedListIdx < 0 || focusedListIdx >= cards.length) return;
                e.preventDefault();
                e.stopPropagation();
                cards[focusedListIdx].click();
            }
            return;
        }

        // Typing: redirect to search box
        if (e.key.length === 1 && !e.ctrlKey && !e.metaKey && !e.altKey) {
            el.search && el.search.focus();
        }
    }, true);

    // ── Init ─────────────────────────────────────────────────────────────

    // Load grid columns from state
    async function loadGridState() {
        try {
            const result = await callAction('get-state', {});
            if (result && result.gridColumns) {
                gridColumns = result.gridColumns;
            }
        } catch (err) {
            console.error('Failed to load grid state:', err);
        }
        // Always update the UI to match gridColumns
        if (el.gridColumnCount) {
            el.gridColumnCount.textContent = gridColumns + ' columns';
        }
    }

    // Save grid columns to state
    function saveGridState() {
        try {
            callAction('save-state', {gridColumns: gridColumns});
        } catch (err) {
            console.error('Failed to save grid state:', err);
        }
    }

    // Wire up app.js integration: focus first tag on ArrowDown from tab bar,
    // and tell app.js it can return to tabs when focus was cleared at the top.
    _focusItems = () => {
        focusedPanel = 'tags';
        setTagFocused(0);
    };
    _canReturnToTabs = () => true;

    loadGridState().then(() => {
        updateFlaggedButtonDisplay();
        loadMovies();
    });
})();
