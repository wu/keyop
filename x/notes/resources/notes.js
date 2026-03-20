(() => {
    let currentNoteId = null;
    let isEditing = false;
    let allNotes = [];
    let currentSearch = '';

    const elements = {
        search: document.getElementById('notes-search'),
        searchContent: document.getElementById('notes-search-content'),
        newBtn: document.getElementById('notes-new-btn'),
        backBtn: document.getElementById('notes-back-btn'),
        list: document.getElementById('notes-list'),
        view: document.getElementById('notes-view'),
        edit: document.getElementById('notes-edit'),
        title: document.getElementById('notes-title'),
        content: document.getElementById('notes-content'),
        tags: document.getElementById('notes-tags'),
        editBtn: document.getElementById('notes-edit-btn'),
        saveBtn: document.getElementById('notes-save-btn'),
        deleteBtn: document.getElementById('notes-delete-btn'),
        cancelBtn: document.getElementById('notes-cancel-btn'),
        importZone: document.getElementById('notes-import-zone'),
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
        const result = await callAction('get-notes', {
            search: currentSearch,
            limit: 100,
            search_content: elements.searchContent ? elements.searchContent.checked : false,
        });
        if (result && result.notes) {
            allNotes = result.notes;
            renderNotesList();
        }
    }

    if (elements.searchContent) {
        elements.searchContent.addEventListener('change', (e) => {
            loadNotes();
        });
    }

    function renderNotesList() {
        elements.list.innerHTML = '';
        allNotes.forEach(note => {
            const item = document.createElement('div');
            item.className = 'notes-item' + (note.id === currentNoteId ? ' active' : '');
            item.innerHTML = `
                <div class="notes-item-title">${escapeHtml(note.title)}</div>
                <div class="notes-item-meta">${formatAge(note.updated_at)}</div>
            `;
            item.onclick = () => selectNote(note.id);
            elements.list.appendChild(item);
        });
    }

    async function selectNote(id) {
        if (isEditing) {
            if (!confirm('Discard changes?')) return;
        }

        currentNoteId = id;
        isEditing = false;
        showNotePanel();
        updateEditMode();
        renderNotesList();

        const result = await callAction('get-note', {id});
        if (result) {
            elements.view.innerHTML = '';
            elements.title.value = result.title || '';
            elements.content.value = result.content || '';
            elements.tags.value = result.tags || '';

            // Show the full modified time above the note
            const timestamp = document.createElement('div');
            timestamp.className = 'notes-view-timestamp';
            timestamp.textContent = 'Modified: ' + formatFullDate(result.updated_at);
            elements.view.appendChild(timestamp);

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

                    // Search for note with this title
                    const searchResult = await callAction('get-notes', {search: pageName, limit: 100});
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
            content: elements.content.value,
            tags: elements.tags.value,
        });

        if (result) {
            isEditing = false;
            updateEditMode();
            loadNotes();
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
            elements.view.innerHTML = '<div class="notes-view-empty">Select a note or create a new one</div>';
        }
    }

    async function createNewNote() {
        const title = prompt('Note title:');
        if (!title) return;

        const result = await callAction('create-note', {title, content: '', tags: ''});
        if (result && result.id) {
            await loadNotes();
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
        }
    }

    // Event listeners
    elements.search.addEventListener('input', (e) => {
        currentSearch = e.target.value;
        loadNotes();
    });

    elements.newBtn.addEventListener('click', createNewNote);
    elements.editBtn.addEventListener('click', startEdit);
    elements.saveBtn.addEventListener('click', saveNote);
    elements.cancelBtn.addEventListener('click', cancelEdit);
    elements.deleteBtn.addEventListener('click', deleteCurrentNote);

    // Keyboard navigation
    document.addEventListener('keydown', (e) => {
        // Allow all keys to work normally in textareas and inputs
        if (e.target.tagName === 'TEXTAREA' || e.target.tagName === 'INPUT') {
            // Stop propagation so parent handlers don't intercept arrow keys
            e.stopPropagation();
            return;
        }

        // In view mode, use arrow keys to navigate notes
        if (!isEditing && (e.key === 'ArrowUp' || e.key === 'ArrowDown')) {
            e.preventDefault();

            if (allNotes.length === 0) return;

            const currentIndex = allNotes.findIndex(n => n.id === currentNoteId);
            let nextIndex = currentIndex;

            if (e.key === 'ArrowUp') {
                nextIndex = currentIndex <= 0 ? allNotes.length - 1 : currentIndex - 1;
            } else if (e.key === 'ArrowDown') {
                nextIndex = currentIndex >= allNotes.length - 1 ? 0 : currentIndex + 1;
            }

            selectNote(allNotes[nextIndex].id);
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

    // Initialize
    loadNotes();
    elements.view.innerHTML = '<div class="notes-view-empty">Select a note or create a new one</div>';
})();
