// Journal tab functionality
function getLocalDateString() {
    const now = new Date();
    const year = now.getFullYear();
    const month = String(now.getMonth() + 1).padStart(2, '0');
    const day = String(now.getDate()).padStart(2, '0');
    return `${year}-${month}-${day}`;
}

let journalState = {
    currentDate: getLocalDateString(),
    isEditing: false,
    originalContent: ''
};

// Refresh the list of available dates
async function refreshJournalDates() {
    try {
        const response = await fetch('/api/tabs/journal/action/get-dates', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
        });

        if (!response.ok) return;

        const data = await response.json();
        const dates = data.dates || [];
        dates.sort().reverse(); // Sort newest first

        renderDateList(dates);
    } catch (err) {
        console.error('[journal] Failed to load dates:', err);
    }
}

// Render the date list in the calendar
function renderDateList(dates) {
    const dateList = document.getElementById('journal-date-list');
    if (!dateList) {
        return;
    }

    dateList.innerHTML = '';

    dates.forEach(date => {
        const button = document.createElement('button');
        button.className = 'journal-date-btn';
        button.setAttribute('data-date', date);
        if (date === journalState.currentDate) {
            button.classList.add('active');
        }

        const dateObj = new Date(date + 'T00:00:00');
        const formatted = dateObj.toLocaleDateString('en-US', {
            weekday: 'short',
            month: 'short',
            day: 'numeric'
        });

        button.textContent = formatted;
        button.onclick = () => selectDate(date);
        dateList.appendChild(button);
    });
}

// Select a date and load its entry
async function selectDate(date) {
    if (journalState.isEditing) {
        const confirmSave = confirm('You have unsaved changes. Do you want to save before switching dates?');
        if (confirmSave) {
            await saveJournalEntry();
        } else {
            journalState.isEditing = false;
            toggleEditMode();
        }
    }

    journalState.currentDate = date;
    updateDateButtons();
    await loadJournalEntry(date);
}

// Load a journal entry
async function loadJournalEntry(date) {
    try {
        const response = await fetch('/api/tabs/journal/action/get-entry', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({date})
        });

        if (!response.ok) {
            console.error('[journal] Failed to load entry, response:', response.status);
            return;
        }

        const data = await response.json();
        displayJournalEntry(data.content);
    } catch (err) {
        console.error('[journal] Failed to load entry:', err);
    }
}

// Display the journal entry (rendered markdown)
function displayJournalEntry(content) {
    const viewContainer = document.getElementById('journal-view');
    const editContainer = document.getElementById('journal-edit');

    if (journalState.isEditing) {
        if (editContainer) {
            const textarea = editContainer.querySelector('textarea');
            if (textarea) {
                textarea.value = content;
            }
        }
    } else {
        if (viewContainer) {
            // Use backend markdown rendering
            renderMarkdownFromBackend(content, (html) => {
                viewContainer.innerHTML = html;
                journalState.originalContent = content;
            });
        }
    }
}

// Render markdown using the backend goldmark renderer
async function renderMarkdownFromBackend(content, callback) {
    try {
        if (!content) {
            callback('<p><em>No entries yet.</em></p>');
            return;
        }

        const response = await fetch('/api/tabs/journal/action/render-markdown', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({content})
        });

        if (!response.ok) {
            console.error('[journal] Failed to render markdown:', response.status);
            callback('<p><em>Failed to render content.</em></p>');
            return;
        }

        const data = await response.json();
        callback(data.html || '<p><em>No entries yet.</em></p>');
    } catch (err) {
        console.error('[journal] Error rendering markdown:', err);
        callback('<p><em>Failed to render content.</em></p>');
    }
}

// Escape HTML special characters
function escapeHtml(text) {
    const map = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#039;'
    };
    return text.replace(/[&<>"']/g, m => map[m]);
}

// Toggle edit mode
function toggleEditMode() {
    journalState.isEditing = !journalState.isEditing;

    const viewContainer = document.getElementById('journal-view');
    const editContainer = document.getElementById('journal-edit');
    const editBtn = document.getElementById('journal-edit-btn');
    const saveBtn = document.getElementById('journal-save-btn');
    const cancelBtn = document.getElementById('journal-cancel-btn');

    if (journalState.isEditing) {
        if (viewContainer) viewContainer.style.display = 'none';
        if (editContainer) editContainer.style.display = 'block';
        if (editBtn) editBtn.style.display = 'none';
        if (saveBtn) saveBtn.style.display = 'inline-block';
        if (cancelBtn) cancelBtn.style.display = 'inline-block';

        // Load content into textarea
        const textarea = editContainer?.querySelector('textarea');
        if (textarea) {
            textarea.value = journalState.originalContent;
            textarea.focus();
        }
    } else {
        if (viewContainer) viewContainer.style.display = 'block';
        if (editContainer) editContainer.style.display = 'none';
        if (editBtn) editBtn.style.display = 'inline-block';
        if (saveBtn) saveBtn.style.display = 'none';
        if (cancelBtn) cancelBtn.style.display = 'none';
    }
}

// Cancel editing without saving
function cancelEdit() {
    journalState.isEditing = false;
    toggleEditMode();
    displayJournalEntry(journalState.originalContent);
}

// Save the journal entry
async function saveJournalEntry() {
    try {
        const editContainer = document.getElementById('journal-edit');
        const textarea = editContainer?.querySelector('textarea');
        const content = textarea?.value || '';


        const response = await fetch('/api/tabs/journal/action/save-entry', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                date: journalState.currentDate,
                content
            })
        });

        if (!response.ok) {
            console.error('[journal] Save failed with status:', response.status);
            alert('Failed to save journal entry');
            return;
        }

        const result = await response.json();

        // Update state AFTER toggling to ensure correct final state
        journalState.originalContent = content;

        // Exit edit mode (this will toggle isEditing)
        if (journalState.isEditing) {
            toggleEditMode();
        }

        // Display the saved content (with markdown rendering)
        displayJournalEntry(content);

        // Refresh dates in case this is a new entry
        refreshJournalDates();
    } catch (err) {
        console.error('[journal] Failed to save entry:', err);
        alert('Failed to save journal entry');
    }
}

// Update date button highlighting
function updateDateButtons() {
    const buttons = document.querySelectorAll('.journal-date-btn');
    buttons.forEach(btn => {
        btn.classList.remove('active');
        if (btn.getAttribute('data-date') === journalState.currentDate) {
            btn.classList.add('active');
        }
    });
}

// Export init function for the web UI
export async function init(container) {

    // Set up event listeners for buttons
    const editBtn = document.getElementById('journal-edit-btn');
    const saveBtn = document.getElementById('journal-save-btn');
    const cancelBtn = document.getElementById('journal-cancel-btn');


    if (editBtn) {
        editBtn.addEventListener('click', () => {
            toggleEditMode();
        });
    }
    if (saveBtn) {
        saveBtn.addEventListener('click', saveJournalEntry);
    }
    if (cancelBtn) {
        cancelBtn.addEventListener('click', cancelEdit);
    }

    // Prevent tab navigation from capturing arrow keys and enter in textarea
    const textarea = document.getElementById('journal-textarea');
    if (textarea) {
        textarea.addEventListener('keydown', (e) => {
            // Allow arrow keys, enter, and other normal textarea operations
            if (['ArrowUp', 'ArrowDown', 'ArrowLeft', 'ArrowRight', 'Enter', 'Backspace', 'Delete', 'Tab'].includes(e.key)) {
                e.stopPropagation();
            }
        });
    }

    // Load initial data
    await refreshJournalDates();
    await loadJournalEntry(journalState.currentDate);
    updateDateButtons();
}
