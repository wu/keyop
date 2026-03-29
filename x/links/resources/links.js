let linksContainer = null;
let currentLinks = [];
let currentFilter = {
    search: '',
    tag: '',
    sort: 'date-desc'
};
let editingLinkId = null;
let searchTimeout = null;
let currentOffset = 0;
let totalLinksCount = 0;
const PAGE_SIZE = 100;

// Keyboard navigation state
let focusedLinkId = null;  // keyboard cursor — not yet committed
let focusedTag = null;  // keyboard cursor for tags — not yet committed
let focusedPanel = null;  // which panel has keyboard focus: 'links', 'tags', or null

const TAB_ID = 'links';

export async function init(container) {
    linksContainer = container;
    setupEventListeners();
    await loadLinks();
    await loadTagCounts();
    // Don't initialize focusedPanel here - it will be set when the tab is activated
}

function setupEventListeners() {
    const addBtn = document.getElementById('links-add-btn');
    const submitBtn = document.getElementById('links-submit-btn');
    const cancelBtn = document.getElementById('links-cancel-btn');
    const searchInput = document.getElementById('links-search');
    const clearSearchBtn = document.getElementById('links-clear-search');
    const sortDropdown = document.getElementById('links-sort');
    const tagFilterInput = document.getElementById('links-tag-filter');

    if (!addBtn) return;

    addBtn.addEventListener('click', () => {
        const form = document.getElementById('links-add-form');
        form.style.display = form.style.display === 'none' ? 'block' : 'none';
        if (form.style.display === 'block') {
            document.getElementById('links-url-input').focus();
        }
    });

    submitBtn.addEventListener('click', handleSubmit);
    cancelBtn.addEventListener('click', () => {
        document.getElementById('links-add-form').style.display = 'none';
        clearForm();
    });

    // Debounced search with 300ms delay
    searchInput.addEventListener('input', (e) => {
        currentFilter.search = e.target.value;
        clearSearchBtn.style.display = currentFilter.search ? 'block' : 'none';

        clearTimeout(searchTimeout);
        searchTimeout = setTimeout(() => {
            currentOffset = 0;
            loadLinks();
            loadTagCounts();
        }, 300);
    });

    clearSearchBtn.addEventListener('click', () => {
        searchInput.value = '';
        currentFilter.search = '';
        clearSearchBtn.style.display = 'none';
        clearTimeout(searchTimeout);
        currentOffset = 0;
        loadLinks();
        loadTagCounts();
        searchInput.focus();
    });

    sortDropdown.addEventListener('change', (e) => {
        currentFilter.sort = e.target.value;
        currentOffset = 0;
        loadLinks();
    });

    // Tag filter with debouncing
    if (tagFilterInput) {
        let tagFilterTimeout = null;
        const clearTagFilterBtn = document.getElementById('links-clear-tag-filter');

        tagFilterInput.addEventListener('input', (e) => {
            const filterText = e.target.value.toLowerCase();
            clearTagFilterBtn.style.display = filterText ? 'block' : 'none';
            clearTimeout(tagFilterTimeout);

            tagFilterTimeout = setTimeout(() => {
                const tagItems = document.querySelectorAll('#links-container .tag-item');
                for (const item of tagItems) {
                    const tagName = item.querySelector('.tag-name').textContent.toLowerCase();
                    const matches = tagName.includes(filterText);
                    item.style.display = matches ? 'flex' : 'none';
                }
            }, 100);
        });

        clearTagFilterBtn.addEventListener('click', () => {
            tagFilterInput.value = '';
            clearTagFilterBtn.style.display = 'none';
            const tagItems = document.querySelectorAll('#links-container .tag-item');
            for (const item of tagItems) {
                item.style.display = 'flex';
            }
            tagFilterInput.focus();
        });
    }

    // Keyboard navigation
    document.addEventListener('keydown', (e) => {
        // Only handle keyboard nav if links tab is active
        const tabContent = document.getElementById(`tab-content-${TAB_ID}`);
        if (!tabContent || !tabContent.classList.contains('active')) {
            return;
        }

        // Initialize focusedPanel when tab becomes active (first time only)
        if (focusedPanel == null) {
            focusedPanel = 'links';
        }

        const searchInput = document.getElementById('links-search');
        const tagFilterInput = document.getElementById('links-tag-filter');

        // Special case: down arrow in search field moves focus to link list
        if (e.key === 'ArrowDown' && e.target === searchInput) {
            e.preventDefault();
            searchInput.blur();  // Remove focus from search input
            setFocusedPanel('links');
            if (focusedLinkId == null && currentLinks.length > 0) {
                setLinkFocused(currentLinks[0].id);
            }
            return;
        }

        // Special case: down arrow in tag filter moves focus to tag list
        if (e.key === 'ArrowDown' && e.target === tagFilterInput) {
            e.preventDefault();
            tagFilterInput.blur();  // Remove focus from tag filter input
            setFocusedPanel('tags');
            const allTags = getVisibleTags();
            if (focusedTag == null && allTags.length > 0) {
                setTagFocused(allTags[0].tag);
            }
            return;
        }

        // Allow all keys to work normally in other textareas and inputs
        if (e.target.tagName === 'TEXTAREA' || e.target.tagName === 'INPUT') {
            e.stopPropagation();
            return;
        }

        // Left/Right: switch between panels
        if (e.key === 'ArrowLeft' && focusedPanel === 'links') {
            e.preventDefault();
            setFocusedPanel('tags');
            return;
        }

        if (e.key === 'ArrowRight' && focusedPanel === 'tags') {
            e.preventDefault();
            setFocusedPanel('links');
            return;
        }

        // Up/Down: move keyboard cursor without committing selection
        if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
            e.preventDefault();
            if (focusedPanel === 'links') {
                const handled = handleLinkNavigation(e.key === 'ArrowUp' ? -1 : 1);
                if (!handled) {
                    // At top of list and pressing up - no wrapping, let focus escape to tab bar
                    if (e.key === 'ArrowUp') {
                        focusedLinkId = null;
                        e.preventDefault();
                        // Let the parent handle moving focus to tab bar
                        return;
                    }
                }
            } else if (focusedPanel === 'tags') {
                const handled = handleTagNavigation(e.key === 'ArrowUp' ? -1 : 1);
                if (!handled) {
                    // At top of tags and pressing up - escape to tab bar
                    if (e.key === 'ArrowUp') {
                        focusedTag = null;
                        e.preventDefault();
                        return;
                    }
                }
            }
            return;
        }

        // Typing: move focus to search and type there
        if (e.key.length === 1 && !e.ctrlKey && !e.metaKey) {
            // Only if we have keyboard focus in the tab (not focused on a control)
            if (focusedPanel || focusedLinkId || focusedTag) {
                const searchInput = document.getElementById('links-search');
                if (searchInput) {
                    e.preventDefault();
                    searchInput.focus();
                    // Insert the character into the search input
                    const currentVal = searchInput.value;
                    const cursorPos = searchInput.selectionStart;
                    searchInput.value = currentVal.slice(0, cursorPos) + e.key + currentVal.slice(cursorPos);
                    searchInput.selectionStart = searchInput.selectionEnd = cursorPos + 1;
                    // Trigger input event for debounced search
                    searchInput.dispatchEvent(new Event('input', {bubbles: true}));
                }
                return;
            }
        }

        // Enter: commit the focused selection
        if (e.key === 'Enter') {
            if (focusedPanel === 'links' && focusedLinkId != null) {
                e.preventDefault();
                // Open the link in a new tab without leaving the links tab
                const linkElement = document.querySelector(`[data-link-id="${focusedLinkId}"]`);
                if (linkElement) {
                    const link = linkElement.querySelector('a');
                    if (link) {
                        // Open in new tab/window using target="_blank" without clicking
                        window.open(link.href, '_blank', 'noopener,noreferrer');
                    }
                }
            } else if (focusedPanel === 'tags' && focusedTag != null && focusedTag !== currentFilter.tag) {
                e.preventDefault();
                window.linksFilterByTag(focusedTag);
            }
        }
    }, true);

    // Reset keyboard focus when tab is hidden
    const tabContent = document.getElementById(`tab-content-${TAB_ID}`);
    if (tabContent) {
        const observer = new MutationObserver(() => {
            if (!tabContent.classList.contains('active') && focusedPanel != null) {
                // Tab is being hidden, clear the focused panel
                focusedPanel = null;
                clearLinkFocusDisplay();
                clearTagFocusDisplay();
            }
        });
        observer.observe(tabContent, {attributes: true, attributeFilter: ['class']});
    }
}

function setFocusedPanel(panel) {
    const prev = focusedPanel;
    focusedPanel = panel;
    if (panel === 'links') {
        // Clear visual focus from tags without clearing focusedTag state
        if (prev === 'tags') clearTagFocusDisplay();
        // Restore focus to previous link or first link
        const idToFocus = focusedLinkId ?? currentLinks[0]?.id ?? null;
        setLinkFocused(idToFocus);
    } else if (panel === 'tags') {
        // Clear visual focus from links without clearing focusedLinkId state
        if (prev === 'links') clearLinkFocusDisplay();
        // Restore focus to previous tag or current filter tag
        const allTags = getVisibleTags();
        const tagToFocus = focusedTag ?? currentFilter.tag ?? (allTags.length > 0 ? allTags[0].tag : null);
        setTagFocused(tagToFocus);
    }
}

function clearLinkFocusDisplay() {
    // Clear visual focus without changing focusedLinkId state
    const items = document.querySelectorAll('#links-container .links-item');
    items.forEach(el => el.classList.remove('kbd-focused'));
}

function clearTagFocusDisplay() {
    // Clear visual focus without changing focusedTag state
    const tagItems = document.querySelectorAll('#links-container .tag-item');
    tagItems.forEach(el => el.classList.remove('kbd-focused'));
}

function getVisibleTags() {
    const tagItems = document.querySelectorAll('#links-container .tag-item');
    return Array.from(tagItems)
        .filter(el => el.style.display !== 'none')
        .map(el => ({tag: el.dataset.tag, element: el}));
}

function handleLinkNavigation(direction) {
    const items = document.querySelectorAll('#links-container .links-item');
    if (items.length === 0) return false;

    // If no focus yet, start at first item on down, last on up
    if (focusedLinkId == null) {
        if (direction > 0 && currentLinks.length > 0) {
            setLinkFocused(currentLinks[0].id);
            return true;
        } else if (direction < 0 && currentLinks.length > 0) {
            setLinkFocused(currentLinks[currentLinks.length - 1].id);
            return true;
        }
        return false;
    }

    const currentIndex = currentLinks.findIndex(l => l.id === focusedLinkId);

    // Don't wrap - check if at boundary
    if (direction < 0 && currentIndex <= 0) {
        return false;  // At top, can't go up
    }
    if (direction > 0 && currentIndex >= currentLinks.length - 1) {
        return false;  // At bottom, can't go down
    }

    const nextIndex = direction > 0 ? currentIndex + 1 : currentIndex - 1;
    setLinkFocused(currentLinks[nextIndex].id);
    return true;  // Successfully navigated
}

function setLinkFocused(id) {
    focusedLinkId = id;
    const items = document.querySelectorAll('#links-container .links-item');
    items.forEach((el, i) => {
        const link = currentLinks[i];
        if (!link) return;
        el.classList.toggle('kbd-focused', id !== null && link.id === id);
    });
    if (id !== null) {
        const idx = currentLinks.findIndex(l => l.id === id);
        if (idx !== -1) {
            const item = document.querySelectorAll('#links-container .links-item')[idx];
            if (item) item.scrollIntoView({block: 'nearest'});
        }
    }
}

function handleTagNavigation(direction) {
    const visibleTags = getVisibleTags();
    if (visibleTags.length === 0) return false;

    const curTag = focusedTag ?? currentFilter.tag;
    const currentIndex = visibleTags.findIndex(t => t.tag === curTag);

    // Don't wrap - check if at boundary
    if (direction < 0 && currentIndex <= 0) {
        return false;  // At top, can't go up
    }
    if (direction > 0 && currentIndex >= visibleTags.length - 1) {
        return false;  // At bottom, can't go down
    }

    const nextIndex = direction > 0 ? currentIndex + 1 : currentIndex - 1;
    setTagFocused(visibleTags[nextIndex].tag);
    return true;  // Successfully navigated
}

function setTagFocused(tag) {
    focusedTag = tag;
    const tagItems = document.querySelectorAll('#links-container .tag-item');
    tagItems.forEach((el) => {
        if (el.style.display !== 'none') {
            el.classList.toggle('kbd-focused', tag !== null && el.dataset.tag === tag);
            el.classList.toggle('active', currentFilter.tag === el.dataset.tag);
        }
    });
    if (tag !== null) {
        const tagEl = document.querySelector(`[data-tag="${tag}"]`);
        if (tagEl && tagEl.style.display !== 'none') {
            tagEl.scrollIntoView({block: 'nearest'});
        }
    }
}

async function callAction(action, params) {
    try {
        const response = await fetch(`/api/tabs/${TAB_ID}/action/${action}`, {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(params),
        });

        if (!response.ok) {
            throw new Error(`HTTP ${response.status}: ${response.statusText}`);
        }

        return await response.json();
    } catch (err) {
        console.error(`Action ${action} failed:`, err);
        throw err;
    }
}

async function loadLinks() {
    const params = {
        search: currentFilter.search,
        tag: currentFilter.tag,
        sort: currentFilter.sort,
        limit: PAGE_SIZE,
        offset: currentOffset
    };

    try {
        const response = await callAction('list-links', params);
        const links = response.links || [];
        totalLinksCount = response.total || 0;
        currentLinks = links;
        focusedLinkId = null;  // Clear focus when loading new set of links
        renderLinksList(links);
        renderPaginationControls();
    } catch (err) {
        const container = document.getElementById('links-list');
        if (container) {
            container.innerHTML = '<div class="links-empty">Error loading links</div>';
        }
    }
}

function renderLinksList(links) {
    const container = document.getElementById('links-list');
    if (!links || links.length === 0) {
        container.innerHTML = '<div class="links-empty">No links found</div>';
        return;
    }

    let html = '<div class="links-items">';
    for (const link of links) {
        const tagsHTML = link.tags
            ? link.tags.split(',').filter(t => t.trim()).map(tag => {
                const trimmedTag = tag.trim();
                return `<span class="tag" onclick="window.linksFilterByTag('${escapeJs(trimmedTag)}');">${escapeHtml(trimmedTag)}</span>`;
            }).join('')
            : '';

        const displayName = link.name || link.domain || 'Untitled';
        let faviconContainerHTML = `<div class="links-favicon-container"><span class="links-favicon-placeholder">🔗</span></div>`;
        if (link.favicon_path) {
            // Render with a data attribute instead of onerror for safer handling
            faviconContainerHTML = `<div class="links-favicon-container" data-favicon-url="${escapeHtml(link.favicon_path)}"><img src="${escapeHtml(link.favicon_path)}" alt="favicon" class="links-favicon"></div>`;
        }

        const age = formatAge(link.created_at);

        // Note button with note/document icon - greyed out if no notes
        const hasNotes = link.notes && link.notes.trim().length > 0;
        const noteBtnClass = hasNotes ? 'links-note-btn' : 'links-note-btn links-note-btn-empty';
        const noteBtn = `<button class="${noteBtnClass}" title="View/Edit note" onclick="window.linksShowNote('${escapeJs(link.id)}');"><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"></path><polyline points="14 2 14 8 20 8"></polyline><line x1="12" y1="13" x2="18" y2="13"></line><line x1="12" y1="17" x2="18" y2="17"></line></svg></button>`;

        // Checkbox for bulk selection
        const isSelected = selectedLinkIds.has(link.id);
        const checkboxClass = isSelected ? 'checked' : '';
        const checkboxHTML = `<input type="checkbox" class="links-item-checkbox" ${isSelected ? 'checked' : ''} onchange="window.linksToggleSelection('${escapeJs(link.id)}');" title="Select for bulk operations">`;

        html += `
            <div class="links-item" data-link-id="${escapeHtml(link.id)}">
                <div class="links-item-header">
                    ${checkboxHTML}
                    ${faviconContainerHTML}
                    <div class="links-item-title">
                        <div class="links-item-name">
                            <a href="${escapeHtml(link.url)}" target="_blank" rel="noopener noreferrer">${escapeHtml(displayName)}</a>
                        </div>
                        <div class="links-item-domain">${escapeHtml(link.domain)}</div>
                    </div>
                    ${tagsHTML ? `<div class="links-item-tags-inline">${tagsHTML}</div>` : ''}
                    ${age ? `<span class="links-item-age-inline">${age}</span>` : ''}
                    ${noteBtn}
                    <button class="links-edit-btn" title="Edit" onclick="window.linksOpenEditModal('${escapeJs(link.id)}');"><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"></path><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4 9.5-9.5z"></path></svg></button>
                    <button class="links-delete-btn" title="Delete" onclick="window.linksDeleteLink('${escapeJs(link.id)}');"><svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2"><polyline points="3 6 5 6 21 6"></polyline><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path><line x1="10" y1="11" x2="10" y2="17"></line><line x1="14" y1="11" x2="14" y2="17"></line></svg></button>
                </div>
            </div>
        `;
    }
    html += '</div>';
    container.innerHTML = html;

    // Handle failed favicon images
    const faviconImages = container.querySelectorAll('.links-favicon');
    for (const img of faviconImages) {
        img.addEventListener('error', function () {
            this.style.display = 'none';
        });
    }
}

async function loadTagCounts() {
    const params = {search: currentFilter.search};

    try {
        const response = await callAction('get-tag-counts', params);
        renderTagSidebar(response.counts || {});
    } catch (err) {
        console.error('Failed to load tag counts:', err);
        const container = document.getElementById('links-tag-list');
        if (container) {
            container.innerHTML = '<div class="tag-item tag-item-disabled">All (0)</div>';
        }
    }
}

function renderTagSidebar(counts) {
    const container = document.getElementById('links-tag-list');

    if (!counts || Object.keys(counts).length === 0) {
        container.innerHTML = '<div class="tag-item" data-tag="" onclick="window.linksFilterByTag(\'\');"><span class="tag-name">ALL</span><span class="tag-count">0</span></div>';
        return;
    }

    // Add "All" as the first item with correct total count
    const allCount = counts.all || 0;
    const isAllSelected = currentFilter.tag === '';
    let html = `<div class="tag-item${isAllSelected ? ' selected' : ''}" data-tag="" onclick="window.linksFilterByTag('');"><span class="tag-name">ALL</span><span class="tag-count">${allCount}</span></div>`;

    // Sort tags by count (descending)
    const sortedTags = Object.entries(counts)
        .filter(([tag]) => tag !== 'all')
        .sort((a, b) => b[1] - a[1]);

    for (const [tag, count] of sortedTags) {
        const tagUpper = tag.toUpperCase();
        const isSelected = currentFilter.tag === tag ? ' selected' : '';
        html += `<div class="tag-item${isSelected}" data-tag="${escapeHtml(tag)}" onclick="window.linksFilterByTag('${escapeJs(tag)}');"><span class="tag-name">${escapeHtml(tagUpper)}</span><span class="tag-count">${count}</span></div>`;
    }
    container.innerHTML = html;
}

function renderPaginationControls() {
    const container = document.getElementById('links-list');
    if (!container) return;

    const hasNextPage = currentOffset + PAGE_SIZE < totalLinksCount;
    const hasPrevPage = currentOffset > 0;

    if (!hasNextPage && !hasPrevPage) {
        // Single page, no pagination needed
        return;
    }

    let paginationHtml = '<div class="links-pagination">';

    if (hasPrevPage) {
        paginationHtml += `<button class="links-pagination-btn" onclick="window.linksPreviousPage();">← Previous</button>`;
    }

    const currentPage = Math.floor(currentOffset / PAGE_SIZE) + 1;
    const totalPages = Math.ceil(totalLinksCount / PAGE_SIZE);
    paginationHtml += `<span class="links-pagination-info">Page ${currentPage} of ${totalPages}</span>`;

    if (hasNextPage) {
        paginationHtml += `<button class="links-pagination-btn" onclick="window.linksNextPage();">Next →</button>`;
    }

    paginationHtml += '</div>';

    const existingPagination = container.querySelector('.links-pagination');
    if (existingPagination) {
        existingPagination.remove();
    }

    container.insertAdjacentHTML('beforeend', paginationHtml);
}

window.linksNextPage = function () {
    if (currentOffset + PAGE_SIZE < totalLinksCount) {
        currentOffset += PAGE_SIZE;
        loadLinks();
        loadTagCounts();
        setTimeout(() => {
            const listContainer = document.getElementById('links-list');
            if (listContainer) {
                listContainer.scrollTop = 0;
            }
        }, 0);
    }
};

window.linksPreviousPage = function () {
    if (currentOffset > 0) {
        currentOffset = Math.max(0, currentOffset - PAGE_SIZE);
        loadLinks();
        loadTagCounts();
        setTimeout(() => {
            const listContainer = document.getElementById('links-list');
            if (listContainer) {
                listContainer.scrollTop = 0;
            }
        }, 0);
    }
};

async function handleSubmit() {
    const urlInput = document.getElementById('links-url-input').value.trim();
    const nameInput = document.getElementById('links-name-input').value.trim();
    const notesInput = document.getElementById('links-notes-input').value.trim();
    const tagsInput = document.getElementById('links-tags-input').value.trim();
    const dateInput = document.getElementById('links-created-at-input').value.trim();
    const iconFile = document.getElementById('links-icon-input').files[0];

    if (!urlInput) {
        alert('URL is required');
        return;
    }

    // Check if it's bulk input (contains newlines or pipes)
    if (urlInput.includes('\n') || urlInput.includes('|')) {
        await handleBulkImport(urlInput, tagsInput, dateInput);
    } else {
        try {
            let iconPath = null;
            if (iconFile) {
                iconPath = await uploadIcon(iconFile);
            }

            await callAction('add-link', {
                url: urlInput,
                name: nameInput,
                notes: notesInput,
                tags: tagsInput,
                created_at: localInputToISO(dateInput),
                icon_path: iconPath
            });
            clearForm();
            document.getElementById('links-add-form').style.display = 'none';
            await loadLinks();
            await loadTagCounts();
        } catch (err) {
            alert('Error adding link: ' + err.message);
        }
    }
}

async function handleBulkImport(text, tags, dateInput) {
    try {
        const result = await callAction('bulk-import', {
            text: text,
            tags: tags,
            created_at: localInputToISO(dateInput)
        });
        let message = `Imported ${result.imported} links`;
        if (result.failed > 0) {
            message += `\n\nFailed to import ${result.failed} URLs:\n\n`;
            if (result.failed_items && result.failed_items.length > 0) {
                const items = result.failed_items.slice(0, 10); // Show first 10
                message += items.map(item => `• ${item.url}\n  Reason: ${item.reason}`).join('\n');
                if (result.failed_items.length > 10) {
                    message += `\n\n... and ${result.failed_items.length - 10} more`;
                }
            }
        }
        alert(message);
        clearForm();
        document.getElementById('links-add-form').style.display = 'none';
        await loadLinks();
        await loadTagCounts();
    } catch (err) {
        alert('Error importing links: ' + err.message);
    }
}

function clearForm() {
    document.getElementById('links-url-input').value = '';
    document.getElementById('links-name-input').value = '';
    document.getElementById('links-notes-input').value = '';
    document.getElementById('links-tags-input').value = '';
    document.getElementById('links-created-at-input').value = '';
    document.getElementById('links-icon-input').value = '';
}

function formatAge(isoDateStr) {
    if (!isoDateStr) return '';

    const created = new Date(isoDateStr);
    const now = new Date();
    let ms = now - created;

    // Handle case where date is in the future or very small diff
    if (ms < 0) ms = 0;

    const seconds = Math.floor(ms / 1000);
    const minutes = Math.floor(seconds / 60);
    const hours = Math.floor(minutes / 60);
    const days = Math.floor(hours / 24);

    const parts = [];
    if (days > 0) {
        parts.push(`${days}d`);
        if (hours % 24 > 0) {
            parts.push(`${hours % 24}h`);
        }
    } else if (hours > 0) {
        parts.push(`${hours}h`);
        if (minutes % 60 > 0) {
            parts.push(`${minutes % 60}m`);
        }
    } else if (minutes > 0) {
        parts.push(`${minutes}m`);
        if (seconds % 60 > 0) {
            parts.push(`${seconds % 60}s`);
        }
    } else {
        parts.push(`${seconds}s`);
    }

    return parts.slice(0, 2).join(' ');
}

function dateToLocalInput(isoStr) {
    if (!isoStr) return '';
    const d = new Date(isoStr);
    // Convert UTC date to local time format for datetime-local input
    // datetime-local expects YYYY-MM-DDTHH:mm in local time, not UTC
    const year = d.getFullYear();
    const month = String(d.getMonth() + 1).padStart(2, '0');
    const day = String(d.getDate()).padStart(2, '0');
    const hours = String(d.getHours()).padStart(2, '0');
    const minutes = String(d.getMinutes()).padStart(2, '0');
    return `${year}-${month}-${day}T${hours}:${minutes}`;
}

function localInputToISO(localStr) {
    if (!localStr) return new Date().toISOString();
    // datetime-local input value is in local time
    // Convert to ISO string in UTC
    const d = new Date(localStr + ':00');
    return d.toISOString();
}

function escapeHtml(text) {
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function escapeJs(text) {
    return text.replace(/'/g, "\\'").replace(/"/g, '\\"');
}

window.linksFilterByTag = function (tag) {
    currentFilter.tag = tag;
    currentOffset = 0;
    focusedLinkId = null;  // Clear focus when changing filter
    focusedTag = null;
    loadLinks();
    loadTagCounts();
};

window.linksDeleteLink = async function (id) {
    if (!confirm('Delete this link?')) return;
    try {
        await callAction('delete-link', {id: id});
        await loadLinks();
        await loadTagCounts();
    } catch (err) {
        alert('Error deleting link: ' + err.message);
    }
};

window.linksOpenEditModal = async function (id) {
    try {
        // Fetch the link data by loading all links and finding the one with this ID
        const response = await callAction('list-links', {limit: 1000, offset: 0});
        const link = response.links.find(l => l.id === id);

        if (!link) {
            alert('Link not found');
            return;
        }

        editingLinkId = id;

        // Populate the modal with link data
        document.getElementById('links-edit-url').value = link.url;
        document.getElementById('links-edit-name').value = link.name || '';
        document.getElementById('links-edit-tags').value = link.tags || '';
        document.getElementById('links-edit-created-at').value = dateToLocalInput(link.created_at);
        // Clear the icon file input
        document.getElementById('links-edit-icon').value = '';

        // Show modal
        document.getElementById('links-edit-modal').classList.add('active');
    } catch (err) {
        alert('Error opening edit dialog: ' + err.message);
    }
};

window.linksCloseEditModal = function () {
    document.getElementById('links-edit-modal').classList.remove('active');
    editingLinkId = null;
};

window.linksSaveEdit = async function () {
    const url = document.getElementById('links-edit-url').value.trim();
    const name = document.getElementById('links-edit-name').value.trim();
    const tags = document.getElementById('links-edit-tags').value.trim();
    const createdAtLocal = document.getElementById('links-edit-created-at').value;
    const iconFile = document.getElementById('links-edit-icon').files[0];

    if (!url) {
        alert('URL is required');
        return;
    }

    try {
        let iconPath = null;
        if (iconFile) {
            iconPath = await uploadIcon(iconFile);
        }

        const updateData = {
            id: editingLinkId,
            url: url,
            name: name,
            tags: tags,
            created_at: localInputToISO(createdAtLocal)
        };

        if (iconPath) {
            updateData.icon_path = iconPath;
        }

        await callAction('update-link', updateData);

        window.linksCloseEditModal();
        await loadLinks();
        await loadTagCounts();
    } catch (err) {
        alert('Error saving link: ' + err.message);
    }
};

async function uploadIcon(file) {
    const formData = new FormData();
    formData.append('icon', file);

    const response = await fetch('/api/links/upload-icon', {
        method: 'POST',
        body: formData
    });

    if (!response.ok) {
        const error = await response.text();
        throw new Error(error || 'Failed to upload icon');
    }

    const data = await response.json();
    return data.path;
}

window.linksShowNote = function (id) {
    // Fetch link and show note in modal
    callAction('list-links', {limit: 1000, offset: 0}).then(response => {
        const link = response.links.find(l => l.id === id);
        if (link) {
            editingLinkId = id;
            document.getElementById('links-note-textarea').value = link.notes || '';
            document.getElementById('links-note-modal').classList.add('active');
        }
    });
};

window.linksCloseNoteModal = function () {
    document.getElementById('links-note-modal').classList.remove('active');
    editingLinkId = null;
};

window.linksSaveNote = async function () {
    const note = document.getElementById('links-note-textarea').value.trim();

    try {
        await callAction('update-link', {
            id: editingLinkId,
            note: note
        });

        window.linksCloseNoteModal();
        await loadLinks();
    } catch (err) {
        alert('Error saving note: ' + err.message);
    }
};

// Bulk tag functionality
let selectedLinkIds = new Set();

window.linksToggleSelection = function (id) {
    if (selectedLinkIds.has(id)) {
        selectedLinkIds.delete(id);
    } else {
        selectedLinkIds.add(id);
    }
    updateSelectedCount();
    renderLinksList(currentLinks);
};

function updateSelectedCount() {
    document.getElementById('links-bulk-tag-count').textContent = selectedLinkIds.size;
}

window.linksSelectAll = function () {
    if (currentLinks && currentLinks.length > 0) {
        for (const link of currentLinks) {
            selectedLinkIds.add(link.id);
        }
        updateSelectedCount();
        renderLinksList(currentLinks);
    }
};

window.linksUnselectAll = function () {
    selectedLinkIds.clear();
    updateSelectedCount();
    renderLinksList(currentLinks);
};

window.linksOpenBulkTagModal = function () {
    if (selectedLinkIds.size === 0) {
        alert('Please select at least one link');
        return;
    }
    document.getElementById('links-bulk-tag-modal').classList.add('active');
    document.getElementById('links-bulk-tag-input').value = '';
    document.getElementById('links-bulk-tag-input').focus();
    // Set to add by default
    document.getElementById('links-tag-action-add').checked = true;
    updateBulkTagSubmitButton();
};

window.linkCloseBulkTagModal = function () {
    document.getElementById('links-bulk-tag-modal').classList.remove('active');
};

function updateBulkTagSubmitButton() {
    const action = document.querySelector('input[name="tag-action"]:checked')?.value || 'add';
    const btn = document.getElementById('links-bulk-tag-submit');
    btn.textContent = action === 'add' ? 'Add Tag' : 'Remove Tag';
}

window.linksSaveBulkTag = async function () {
    const tag = document.getElementById('links-bulk-tag-input').value.trim();
    if (!tag) {
        alert('Tag name is required');
        return;
    }

    const action = document.querySelector('input[name="tag-action"]:checked')?.value || 'add';

    try {
        const result = await callAction('bulk-add-tag', {
            tag: tag,
            action: action,
            ids: Array.from(selectedLinkIds)
        });

        selectedLinkIds.clear();
        updateSelectedCount();
        window.linkCloseBulkTagModal();
        await loadLinks();
        await loadTagCounts();
    } catch (err) {
        alert('Error managing tags: ' + err.message);
    }
};

// Add event listeners to radio buttons to update button text
document.addEventListener('DOMContentLoaded', function () {
    const radioButtons = document.querySelectorAll('input[name="tag-action"]');
    radioButtons.forEach(btn => {
        btn.addEventListener('change', updateBulkTagSubmitButton);
    });
});

