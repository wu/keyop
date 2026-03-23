// Signal to app.js that this tab owns horizontal arrow-key navigation
// (left/right switch between the tags panel and the notes list).
export const handlesHorizontalNav = true;

(() => {
    let currentNoteId = null;
    let currentNoteTitle = '';
    let currentNoteUpdatedAt = '';
    let isEditing = false;
    let allNotes = [];
    let currentSearch = '';
    let currentTag = 'all';
    let currentPage = 1;
    let pageSize = 10;
    let totalCount = 0;
    let tagFilterText = '';
    let focusedNoteId = null;  // keyboard cursor — not yet committed
    let focusedTagTag = null;  // keyboard cursor — not yet committed
    const recentNotes = []; // [{id, title}], most recent first, max 20
    const maxRecent = 20;

    const elements = {
        search: document.getElementById('notes-search'),
        searchContent: document.getElementById('notes-search-content'),
        newBtn: document.getElementById('notes-new-btn'),
        backBtn: document.getElementById('notes-back-btn'),
        list: document.getElementById('notes-list'),
        pagination: document.getElementById('notes-pagination'),
        tagList: document.getElementById('notes-tag-list'),
        tagFilter: document.getElementById('notes-tag-filter'),
        view: document.getElementById('notes-view'),
        edit: document.getElementById('notes-edit'),
        title: document.getElementById('notes-title'),
        content: document.getElementById('notes-content'),
        tags: document.getElementById('notes-tags'),
        editBtn: document.getElementById('notes-edit-btn'),
        saveBtn: document.getElementById('notes-save-btn'),
        deleteBtn: document.getElementById('notes-delete-btn'),
        cancelBtn: document.getElementById('notes-cancel-btn'),
        autolinkBtn: document.getElementById('notes-autolink-btn'),
        recentDropdown: document.getElementById('notes-recent-dropdown'),
        recentWrap: document.getElementById('notes-recent-wrap'),
        container: document.getElementById('notes-container'),
    };

    function showNotePanel() {
        elements.container && elements.container.classList.add('note-selected');
    }

    function showListPanel() {
        elements.container && elements.container.classList.remove('note-selected');
    }

    if (elements.backBtn) {
        elements.backBtn.addEventListener('click', () => showListPanel());
    }

    async function callAction(action, params) {
        try {
            const url = `/api/tabs/notes/action/${action}`;
            const response = await fetch(url, {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify(params),
            });
            if (!response.ok) {
                console.error(`API call failed: ${response.status} ${response.statusText}`);
                return null;
            }
            return await response.json();
        } catch (error) {
            console.error('API call failed:', error);
            return null;
        }
    }

    async function loadNotes() {
        const searchContent = elements.searchContent ? elements.searchContent.checked : false;
        const result = await callAction('get-notes', {
            search: currentSearch,
            tag: currentTag === 'all' ? '' : currentTag,
            limit: pageSize,
            offset: (currentPage - 1) * pageSize,
            search_content: searchContent,
        });
        if (result && result.notes) {
            allNotes = result.notes;
            totalCount = result.total ?? 0;
            renderNotesList();
            renderPagination();
        }
    }

    async function loadTagCounts() {
        const searchContent = elements.searchContent ? elements.searchContent.checked : false;
        const result = await callAction('get-tag-counts', {
            search: currentSearch,
            search_content: searchContent,
        });
        if (result && result.counts) {
            updateTagsList(result.counts);
        }
    }

    if (elements.searchContent) {
        elements.searchContent.addEventListener('change', () => {
            currentPage = 1;
            loadNotes();
            loadTagCounts();
        });
    }

    function renderNotesList() {
        elements.list.innerHTML = '';
        allNotes.forEach(note => {
            const item = document.createElement('div');
            item.className = 'notes-item' +
                (note.id === currentNoteId ? ' active' : '') +
                (note.id === focusedNoteId ? ' kbd-focused' : '');
            const tagParts = note.tags ? note.tags.split(',').map(t => t.trim()).filter(Boolean) : [];
            item.innerHTML = `
                <div class="notes-item-title">${escapeHtml(note.title)}</div>
                <div class="notes-item-meta">
                    <span class="notes-item-age">${formatAge(note.updated_at)}</span>
                    ${tagParts.length > 0 ? `<div class="notes-tags">${tagParts.map(t => `<span class="tag-badge" data-tag="${escapeHtml(t)}">${escapeHtml(t)}</span>`).join('')}</div>` : ''}
                </div>
            `;
            item.onclick = () => selectNote(note.id);
            // Tag badges: click selects that tag without navigating to the note
            item.querySelectorAll('.tag-badge[data-tag]').forEach(badge => {
                badge.addEventListener('click', (e) => {
                    e.stopPropagation();
                    setTagFilter(badge.dataset.tag);
                });
            });
            elements.list.appendChild(item);
        });

        // Show a pinned indicator when the current note is filtered out of the list
        const indicator = document.getElementById('notes-current-indicator');
        if (indicator) {
            const inList = currentNoteId && allNotes.some(n => n.id === currentNoteId);
            indicator.style.display = (currentNoteId && !inList) ? '' : 'none';
            if (currentNoteId && !inList) {
                indicator.querySelector('.notes-current-indicator-title').textContent = currentNoteTitle || '(untitled)';
                indicator.querySelector('.notes-current-indicator-meta').textContent = currentNoteUpdatedAt ? formatAge(currentNoteUpdatedAt) : '';
            }
        }
    }

    function renderPagination() {
        if (!elements.pagination) return;
        const totalPages = Math.ceil(totalCount / pageSize);
        if (totalPages <= 1) {
            elements.pagination.innerHTML = '';
            return;
        }
        elements.pagination.innerHTML = `
            <button class="notes-page-btn" id="notes-prev-btn" ${currentPage <= 1 ? 'disabled' : ''}>←</button>
            <span class="notes-page-info">${currentPage} / ${totalPages}</span>
            <button class="notes-page-btn" id="notes-next-btn" ${currentPage >= totalPages ? 'disabled' : ''}>→</button>
        `;
        document.getElementById('notes-prev-btn').addEventListener('click', () => {
            if (currentPage > 1) {
                currentPage--;
                loadNotes();
            }
        });
        document.getElementById('notes-next-btn').addEventListener('click', () => {
            if (currentPage < totalPages) {
                currentPage++;
                loadNotes();
            }
        });
    }

    function updateTagsList(counts) {
        if (!elements.tagList) return;
        elements.tagList.innerHTML = '';
        const filter = tagFilterText.toLowerCase();

        // Always show "all" unless it's filtered out
        if (!filter || 'all'.includes(filter)) {
            const allItem = document.createElement('div');
            allItem.className = 'tag-item' +
                (currentTag === 'all' ? ' active' : '') +
                (focusedTagTag === 'all' ? ' kbd-focused' : '');
            allItem.dataset.tag = 'all';
            allItem.innerHTML = `<span class="tag-label">all</span><span class="service-count">${counts['all'] ?? 0}</span>`;
            allItem.onclick = () => setTagFilter('all');
            elements.tagList.appendChild(allItem);
        }

        // Sort non-meta tags by count descending (exclude 'all'), apply filter
        const sorted = Object.entries(counts)
            .filter(([tag]) => tag !== 'all')
            .sort((a, b) => b[1] - a[1]);
        for (const [tag, count] of sorted) {
            if (filter && !tag.toLowerCase().includes(filter)) continue;
            const item = document.createElement('div');
            item.className = 'tag-item' +
                (currentTag === tag ? ' active' : '') +
                (focusedTagTag === tag ? ' kbd-focused' : '');
            item.dataset.tag = tag;
            item.innerHTML = `<span class="tag-label">${escapeHtml(tag)}</span><span class="service-count">${count}</span>`;
            item.onclick = () => setTagFilter(tag);
            elements.tagList.appendChild(item);
        }
    }

    function setTagFilter(tag) {
        currentTag = tag;
        focusedTagTag = tag;
        currentPage = 1;
        loadNotes();
        loadTagCounts();
    }

    function setNoteFocused(id) {
        focusedNoteId = id;
        elements.list.querySelectorAll('.notes-item').forEach((el, i) => {
            const note = allNotes[i];
            if (!note) return;
            el.classList.toggle('kbd-focused', id !== null && note.id === id);
            el.classList.toggle('active', note.id === currentNoteId);
        });
        if (id !== null) {
            const idx = allNotes.findIndex(n => n.id === id);
            if (idx !== -1) elements.list.children[idx]?.scrollIntoView({block: 'nearest'});
        }
    }

    function setTagFocused(tag) {
        focusedTagTag = tag;
        if (!elements.tagList) return;
        elements.tagList.querySelectorAll('.tag-item').forEach(el => {
            el.classList.toggle('kbd-focused', tag !== null && el.dataset.tag === tag);
            el.classList.toggle('active', el.dataset.tag === currentTag);
        });
        if (tag !== null) {
            elements.tagList.querySelector('.tag-item.kbd-focused')?.scrollIntoView({block: 'nearest'});
        }
    }

    function addToRecent(id, title) {
        const idx = recentNotes.findIndex(n => n.id === id);
        if (idx !== -1) recentNotes.splice(idx, 1);
        recentNotes.unshift({id, title});
        if (recentNotes.length > maxRecent) recentNotes.length = maxRecent;
    }

    function renderRecentDropdown() {
        if (!elements.recentDropdown) return;
        elements.recentDropdown.innerHTML = '';
        if (recentNotes.length === 0) {
            const empty = document.createElement('div');
            empty.className = 'notes-recent-empty';
            empty.textContent = 'No recently visited notes';
            elements.recentDropdown.appendChild(empty);
            return;
        }
        recentNotes.forEach(note => {
            const item = document.createElement('div');
            item.className = 'notes-recent-item' + (note.id === currentNoteId ? ' active' : '');
            item.textContent = note.title;
            item.onclick = () => {
                closeRecentDropdown();
                selectNote(note.id);
            };
            elements.recentDropdown.appendChild(item);
        });
    }

    function openRecentDropdown() {
        renderRecentDropdown();
        elements.recentDropdown.classList.add('open');
        elements.recentBtn && elements.recentBtn.classList.add('active');
    }

    function closeRecentDropdown() {
        elements.recentDropdown && elements.recentDropdown.classList.remove('open');
        elements.recentBtn && elements.recentBtn.classList.remove('active');
    }

    function toggleRecentDropdown() {
        if (elements.recentDropdown.classList.contains('open')) {
            closeRecentDropdown();
        } else {
            openRecentDropdown();
        }
    }

    async function selectNote(id) {
        if (isEditing) {
            if (!confirm('Discard changes?')) return;
        }

        currentNoteId = id;
        focusedNoteId = id;
        isEditing = false;
        showNotePanel();
        updateEditMode();
        renderNotesList();

        const result = await callAction('get-note', {id});
        if (result) {
            currentNoteTitle = result.title || '(untitled)';
            currentNoteUpdatedAt = result.updated_at || '';
            addToRecent(id, currentNoteTitle);
            elements.view.innerHTML = '';
            elements.title.value = result.title || '';
            elements.content.value = result.content || '';
            elements.tags.value = result.tags || '';

            // Show the full modified time and tags above the note
            const timestamp = document.createElement('div');
            timestamp.className = 'notes-view-timestamp';
            const tagParts = (result.tags || '').split(',').map(t => t.trim()).filter(Boolean);
            const tagsHtml = tagParts.length > 0
                ? tagParts.map(t => `<span class="tag-badge notes-view-tag" data-tag="${escapeHtml(t)}">${escapeHtml(t)}</span>`).join('')
                : '';
            timestamp.innerHTML = `<span>Modified: ${escapeHtml(formatFullDate(result.updated_at))}</span>${tagsHtml ? `<span class="notes-view-tags">${tagsHtml}</span>` : ''}`;
            elements.view.appendChild(timestamp);
            // Wire up tag clicks
            timestamp.querySelectorAll('.notes-view-tag').forEach(badge => {
                badge.addEventListener('click', () => setTagFilter(badge.dataset.tag));
            });

            // Render markdown preview (will append to elements.view)
            await renderPreview(result.content || '');
        }
    }

    async function renderPreview(content) {
        const result = await callAction('render-markdown', {content});
        if (result && result.html) {
            // Create a container for the markdown content and append (don't replace)
            const markdownContainer = document.createElement('div');
            markdownContainer.className = 'notes-view-content';
            markdownContainer.innerHTML = result.html || '<div class="notes-view-empty">No content</div>';
            elements.view.appendChild(markdownContainer);

            // Add click handlers for wiki links
            const wikiLinks = markdownContainer.querySelectorAll('a[href="#wiki-link"]');
            wikiLinks.forEach(link => {
                link.addEventListener('click', async (e) => {
                    e.preventDefault();
                    const pageName = link.getAttribute('title');
                    if (!pageName) return;

                    // Search for note with this title (high limit to find exact match)
                    const searchResult = await callAction('get-notes', {search: pageName, limit: 50});
                    if (searchResult && searchResult.notes) {
                        // Find exact title match
                        const matchingNote = searchResult.notes.find(n => n.title === pageName);
                        if (matchingNote) {
                            selectNote(matchingNote.id);
                        } else if (searchResult.notes.length > 0) {
                            // Fall back to first search result
                            selectNote(searchResult.notes[0].id);
                        }
                    }
                });
                // Style wiki links differently
                link.classList.add('wiki-link');
            });
        }
    }

    function formatAge(dateStr) {
        try {
            const timestamp = new Date(dateStr);
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

            // Build the two most significant time units
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
        } catch {
            return dateStr;
        }
    }

    function formatFullDate(dateStr) {
        try {
            const date = new Date(dateStr);
            return date.toLocaleDateString() + ' ' + date.toLocaleTimeString([], {hour: '2-digit', minute: '2-digit'});
        } catch {
            return dateStr;
        }
    }

    function startEdit() {
        if (!currentNoteId) return;
        isEditing = true;
        updateEditMode();
    }

    function updateEditMode() {
        const hidden = !isEditing;
        elements.view.style.display = hidden ? 'block' : 'none';
        elements.edit.style.display = hidden ? 'none' : 'flex';
        elements.editBtn.style.display = hidden ? 'block' : 'none';
        elements.saveBtn.style.display = hidden ? 'none' : 'block';
        elements.cancelBtn.style.display = hidden ? 'none' : 'block';
        if (elements.autolinkBtn) elements.autolinkBtn.style.display = hidden ? 'none' : 'block';
    }

    function cancelEdit() {
        isEditing = false;
        updateEditMode();
        if (currentNoteId) {
            selectNote(currentNoteId);
        }
    }

    async function saveNote() {
        const title = elements.title.value.trim();
        if (!title) {
            alert('Title is required');
            return;
        }

        const result = await callAction('update-note', {
            id: currentNoteId,
            title,
            content: elements.content.value
                .split('\n')
                .map(line => line.trimEnd())
                .join('\n')
                .trimEnd(),
            tags: elements.tags.value,
        });

        if (result) {
            isEditing = false;
            updateEditMode();
            loadNotes();
            loadTagCounts();
            selectNote(currentNoteId);
        }
    }

    async function deleteCurrentNote() {
        if (!currentNoteId || !confirm('Delete this note?')) return;

        const result = await callAction('delete-note', {id: currentNoteId});
        if (result && result.deleted) {
            currentNoteId = null;
            isEditing = false;
            updateEditMode();
            loadNotes();
            loadTagCounts();
            elements.view.innerHTML = '<div class="notes-view-empty">Select a note or create a new one</div>';
        }
    }

    async function createNewNote() {
        const title = prompt('Note title:');
        if (!title) return;

        const result = await callAction('create-note', {title, content: '', tags: ''});
        if (result && result.id) {
            currentPage = 1;
            currentTag = 'all';
            await loadNotes();
            await loadTagCounts();
            selectNote(result.id);
        }
    }

    async function handleFileImport(files) {
        const fileList = Array.from(files);
        const filesToImport = [];

        for (const file of fileList) {
            if (!file.name.endsWith('.md')) continue;

            const content = await file.text();
            filesToImport.push({name: file.name, content});
        }

        if (filesToImport.length === 0) {
            alert('No markdown files found');
            return;
        }

        const result = await callAction('import-notes', {files: filesToImport});
        if (result) {
            alert(`Imported ${result.count} notes`);
            await loadNotes();
            await loadTagCounts();
        }
    }

    // Event listeners
    elements.search.addEventListener('input', (e) => {
        currentSearch = e.target.value;
        currentPage = 1;
        loadNotes();
        loadTagCounts();
    });

    if (elements.tagFilter) {
        elements.tagFilter.addEventListener('input', (e) => {
            tagFilterText = e.target.value;
            // Re-render the existing counts with the new filter — no server round-trip needed
            loadTagCounts();
        });
    }

    elements.newBtn.addEventListener('click', createNewNote);
    elements.editBtn.addEventListener('click', startEdit);
    elements.saveBtn.addEventListener('click', saveNote);
    elements.cancelBtn.addEventListener('click', cancelEdit);
    elements.deleteBtn.addEventListener('click', deleteCurrentNote);

    // Auto-link: replace matched note titles in selected text with [[title]]
    function autoLinkText(text, titles) {
        // titles are pre-sorted longest-first; process character by character
        // to greedily match the longest title at each position, skipping existing [[...]]
        let result = '';
        let i = 0;
        while (i < text.length) {
            // Skip existing [[...]] blocks unchanged
            if (text[i] === '[' && text[i + 1] === '[') {
                const end = text.indexOf(']]', i + 2);
                if (end !== -1) {
                    result += text.substring(i, end + 2);
                    i = end + 2;
                    continue;
                }
            }
            // Try each title (longest first) at the current position
            let matched = false;
            for (const {title} of titles) {
                if (text.substring(i, i + title.length).toLowerCase() === title.toLowerCase()) {
                    result += `[[${title}]]`;
                    i += title.length;
                    matched = true;
                    break;
                }
            }
            if (!matched) {
                result += text[i];
                i++;
            }
        }
        return result;
    }

    async function runAutoLink() {
        const textarea = elements.content;
        if (!textarea) return;

        const start = textarea.selectionStart;
        const end = textarea.selectionEnd;
        const hasSelection = start !== end;
        const targetStart = hasSelection ? start : 0;
        const targetEnd = hasSelection ? end : textarea.value.length;
        const selected = textarea.value.substring(targetStart, targetEnd);

        if (!selected.trim()) return;

        const btn = elements.autolinkBtn;
        const origText = btn ? btn.textContent : '';
        if (btn) {
            btn.textContent = '⏳';
            btn.disabled = true;
        }

        const result = await callAction('get-note-titles', {});
        if (!result || !result.titles) {
            if (btn) {
                btn.textContent = origText;
                btn.disabled = false;
            }
            return;
        }

        // All titles are eligible including the current note (self-links are valid)
        const titles = result.titles;
        const linked = autoLinkText(selected, titles);

        if (linked !== selected) {
            textarea.focus();
            textarea.value = textarea.value.substring(0, targetStart) + linked + textarea.value.substring(targetEnd);
            textarea.selectionStart = targetStart;
            textarea.selectionEnd = targetStart + linked.length;
        }

        if (btn) {
            btn.textContent = origText;
            btn.disabled = false;
        }
    }

    if (elements.autolinkBtn) {
        elements.autolinkBtn.addEventListener('click', runAutoLink);
    }

    if (elements.recentBtn) {
        elements.recentBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            toggleRecentDropdown();
        });
    }

    // Close the recent dropdown when clicking anywhere outside it
    document.addEventListener('click', (e) => {
        if (elements.recentWrap && !elements.recentWrap.contains(e.target)) {
            closeRecentDropdown();
        }
    });

    let focusedPanel = 'notes'; // 'notes' | 'tags'

    function setFocusedPanel(panel) {
        const prev = focusedPanel;
        focusedPanel = panel;
        if (panel === 'notes') {
            // Clear tag focus highlights from the panel being left
            if (prev === 'tags') setTagFocused(null);
            // Move cursor to current note, or first note if none selected
            setNoteFocused(currentNoteId ?? allNotes[0]?.id ?? null);
        } else if (panel === 'tags') {
            // Clear note focus highlights from the panel being left
            if (prev === 'notes') setNoteFocused(null);
            setTagFocused(currentTag);
        }
    }

    // Keyboard navigation
    document.addEventListener('keydown', (e) => {
        // Allow all keys to work normally in textareas and inputs
        if (e.target.tagName === 'TEXTAREA' || e.target.tagName === 'INPUT') {
            // Stop propagation so parent handlers don't intercept arrow keys
            e.stopPropagation();
            return;
        }

        if (!isEditing && e.key === 'ArrowLeft' && focusedPanel === 'notes') {
            e.preventDefault();
            setFocusedPanel('tags');
            return;
        }

        if (e.key === 'ArrowRight' && focusedPanel === 'tags') {
            e.preventDefault();
            setFocusedPanel('notes');
            return;
        }

        // Up/Down: move keyboard cursor without committing selection
        if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
            e.preventDefault();

            if (focusedPanel === 'tags' && elements.tagList) {
                const items = Array.from(elements.tagList.querySelectorAll('.tag-item'));
                if (items.length === 0) return;
                const curTag = focusedTagTag ?? currentTag;
                const activeIdx = items.findIndex(el => el.dataset.tag === curTag);
                const nextIdx = e.key === 'ArrowUp'
                    ? (activeIdx <= 0 ? items.length - 1 : activeIdx - 1)
                    : (activeIdx >= items.length - 1 ? 0 : activeIdx + 1);
                setTagFocused(items[nextIdx].dataset.tag);
                return;
            }

            if (focusedPanel === 'notes' && !isEditing) {
                if (allNotes.length === 0) return;
                const curId = focusedNoteId ?? currentNoteId;
                const currentIndex = allNotes.findIndex(n => n.id === curId);
                const nextIndex = e.key === 'ArrowUp'
                    ? (currentIndex <= 0 ? allNotes.length - 1 : currentIndex - 1)
                    : (currentIndex >= allNotes.length - 1 ? 0 : currentIndex + 1);
                setNoteFocused(allNotes[nextIndex].id);
            }
        }

        // Enter: commit the focused selection
        if (e.key === 'Enter' && !isEditing) {
            if (focusedPanel === 'tags' && focusedTagTag != null && focusedTagTag !== currentTag) {
                e.preventDefault();
                setTagFilter(focusedTagTag);
                return;
            }
            if (focusedPanel === 'notes' && focusedNoteId != null && focusedNoteId !== currentNoteId) {
                e.preventDefault();
                selectNote(focusedNoteId);

            }
        }
    }, true);

    // File import
    const importZone = document.getElementById('notes-import');
    if (importZone) {
        importZone.addEventListener('dragover', (e) => {
            e.preventDefault();
            importZone.classList.add('dragover');
        });

        importZone.addEventListener('dragleave', () => {
            importZone.classList.remove('dragover');
        });

        importZone.addEventListener('drop', (e) => {
            e.preventDefault();
            importZone.classList.remove('dragover');
            handleFileImport(e.dataTransfer.files);
        });

        importZone.addEventListener('click', () => {
            const input = document.createElement('input');
            input.type = 'file';
            input.multiple = true;
            input.accept = '.md';
            input.onchange = (e) => handleFileImport(e.target.files);
            input.click();
        });
    }

    // Helper functions
    function escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    // Dynamically size the page to fit the visible notes list area
    let resizeDebounce = null;

    function recalcPageSize() {
        if (!elements.list) return;
        const containerHeight = elements.list.clientHeight;
        if (containerHeight <= 0) return;
        // Measure the height of a rendered item; fall back to a CSS-derived estimate
        const sampleItem = elements.list.querySelector('.notes-item');
        const itemHeight = sampleItem ? sampleItem.offsetHeight : 64;
        const newSize = Math.max(1, Math.floor(containerHeight / itemHeight));
        if (newSize !== pageSize) {
            pageSize = newSize;
            currentPage = 1;
            loadNotes();
        }
    }

    if (elements.list && typeof ResizeObserver !== 'undefined') {
        new ResizeObserver(() => {
            clearTimeout(resizeDebounce);
            resizeDebounce = setTimeout(recalcPageSize, 100);
        }).observe(elements.list);
    }

    // Initialize
    setFocusedPanel('notes');
    loadNotes();
    loadTagCounts();
    elements.view.innerHTML = '<div class="notes-view-empty">Select a note or create a new one</div>';
})();
