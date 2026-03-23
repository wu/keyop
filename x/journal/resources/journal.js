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
// If today's date does not have a file, create one automatically and refresh.
async function refreshJournalDates() {
    try {
        const response = await fetch('/api/tabs/journal/action/get-dates', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
        });

        if (!response.ok) return;

        let data = await response.json();
        let dates = data.dates || [];
        dates.sort().reverse(); // Sort newest first

        renderDateList(dates);

        // If current date is missing, create it and refresh the list so it can be selected.
        if (!dates.includes(journalState.currentDate)) {
            try {
                const createResp = await fetch('/api/tabs/journal/action/save-entry', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({date: journalState.currentDate, content: ''})
                });
                if (createResp.ok) {
                    // Re-fetch dates and re-render
                    const response2 = await fetch('/api/tabs/journal/action/get-dates', {
                        method: 'POST',
                        headers: {'Content-Type': 'application/json'},
                        body: JSON.stringify({})
                    });
                    if (response2.ok) {
                        const data2 = await response2.json();
                        const dates2 = data2.dates || [];
                        dates2.sort().reverse();
                        renderDateList(dates2);
                    }
                } else {
                    console.error('[journal] Failed to create today entry', createResp.status);
                }
            } catch (createErr) {
                console.error('[journal] Error creating today entry:', createErr);
            }
        }

    } catch (err) {
        console.error('[journal] Failed to load dates:', err);
    }
}

// Render the date list in the calendar (nested: year -> month -> day)
function renderDateList(dates) {
    const dateList = document.getElementById('journal-date-list');
    if (!dateList) {
        return;
    }

    dateList.innerHTML = '';

    // Group dates by year and month
    const years = {};
    dates.forEach(d => {
        if (!d) return;
        const parts = d.split('-');
        if (parts.length < 3) return;
        const y = parts[0];
        const m = parts[1];
        if (!years[y]) years[y] = {};
        if (!years[y][m]) years[y][m] = [];
        years[y][m].push(d);
    });

    // Determine current year/month so we can expand them by default
    const cur = new Date(journalState.currentDate + 'T00:00:00');
    const currentYear = String(cur.getFullYear());
    const currentMonth = String(cur.getMonth() + 1).padStart(2, '0');

    // Sort years descending (newest first)
    const yearKeys = Object.keys(years).sort((a, b) => Number(b) - Number(a));

    yearKeys.forEach(year => {
        const yearContainer = document.createElement('div');
        yearContainer.className = 'journal-year';
        yearContainer.setAttribute('data-year', year);

        const yearBtn = document.createElement('button');
        yearBtn.className = 'journal-year-btn';
        yearBtn.type = 'button';
        yearBtn.textContent = year;
        yearBtn.setAttribute('aria-expanded', 'false');
        yearBtn.onclick = () => {
            const collapsed = yearContainer.classList.toggle('collapsed');
            yearBtn.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
        };

        yearContainer.appendChild(yearBtn);

        const monthList = document.createElement('div');
        monthList.className = 'journal-month-list';

        // Expand current year by default
        if (year === currentYear) {
            yearContainer.classList.remove('collapsed');
            yearBtn.setAttribute('aria-expanded', 'true');
        } else {
            yearContainer.classList.add('collapsed');
            yearBtn.setAttribute('aria-expanded', 'false');
        }

        // Sort months descending
        const monthKeys = Object.keys(years[year]).sort((a, b) => Number(b) - Number(a));
        monthKeys.forEach(month => {
            const monthContainer = document.createElement('div');
            monthContainer.className = 'journal-month';
            monthContainer.setAttribute('data-month', month);

            const monthBtn = document.createElement('button');
            monthBtn.className = 'journal-month-btn';
            monthBtn.type = 'button';
            // Month label like 'Mar' (short)
            const mDate = new Date(Number(year), Number(month) - 1, 1);
            const monthLabel = mDate.toLocaleString('en-US', {month: 'short'});
            monthBtn.textContent = monthLabel;
            monthBtn.setAttribute('aria-expanded', 'false');
            monthBtn.onclick = () => {
                const collapsed = monthContainer.classList.toggle('collapsed');
                monthBtn.setAttribute('aria-expanded', collapsed ? 'false' : 'true');
            };

            // Expand current month for current year
            if (year === currentYear && month === currentMonth) {
                monthContainer.classList.remove('collapsed');
                monthBtn.setAttribute('aria-expanded', 'true');
            } else {
                monthContainer.classList.add('collapsed');
                monthBtn.setAttribute('aria-expanded', 'false');
            }

            monthContainer.appendChild(monthBtn);

            const dayList = document.createElement('div');
            dayList.className = 'journal-day-list';

            // Sort days descending
            const dayDates = years[year][month].sort().reverse();
            dayDates.forEach(date => {
                const button = document.createElement('button');
                button.className = 'journal-date-btn';
                button.setAttribute('data-date', date);
                button.type = 'button';

                const dateObj = new Date(date + 'T00:00:00');
                const yyyy = dateObj.getFullYear();
                const mm = String(dateObj.getMonth() + 1).padStart(2, '0');
                const dd = String(dateObj.getDate()).padStart(2, '0');
                const weekday = dateObj.toLocaleDateString('en-US', {weekday: 'short'}).toLowerCase();
                const formatted = `${yyyy}.${mm}.${dd} ${weekday}`;

                button.textContent = formatted;
                if (date === journalState.currentDate) {
                    button.classList.add('active');
                    // ensure parents are expanded
                    yearContainer.classList.remove('collapsed');
                    monthContainer.classList.remove('collapsed');
                    yearBtn.setAttribute('aria-expanded', 'true');
                    monthBtn.setAttribute('aria-expanded', 'true');
                }

                button.onclick = () => selectDate(date);
                dayList.appendChild(button);
            });

            monthContainer.appendChild(dayList);
            monthList.appendChild(monthContainer);
        });

        yearContainer.appendChild(monthList);
        dateList.appendChild(yearContainer);
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
            updateEditModeUI();
        }
    }

    journalState.currentDate = date;
    updateDateButtons();
    showEntryPanel();
    await loadJournalEntry(date);
}

function showEntryPanel() {
    const c = document.getElementById('journal-container');
    if (c) c.classList.add('entry-selected');
}

function showListPanel() {
    const c = document.getElementById('journal-container');
    if (c) c.classList.remove('entry-selected');
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
    updateEditModeUI();
}

function updateEditModeUI() {
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
    updateEditModeUI();
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

            // Ensure parent month/year are expanded so the selected date is visible
            const monthContainer = btn.closest('.journal-month');
            const yearContainer = btn.closest('.journal-year');
            if (monthContainer) {
                monthContainer.classList.remove('collapsed');
                const monthBtn = monthContainer.querySelector('.journal-month-btn');
                if (monthBtn) monthBtn.setAttribute('aria-expanded', 'true');
            }
            if (yearContainer) {
                yearContainer.classList.remove('collapsed');
                const yearBtn = yearContainer.querySelector('.journal-year-btn');
                if (yearBtn) yearBtn.setAttribute('aria-expanded', 'true');
            }

            // Keep selected date visible
            try {
                btn.scrollIntoView({block: 'nearest', inline: 'nearest'});
            } catch (e) {
            }
        }
    });
}

// Export focusItems and canReturnToTabs for app-level keyboard navigation
export function focusItems() {
    const buttons = Array.from(document.querySelectorAll('.journal-date-btn')).filter(b => b.offsetParent !== null);
    if (!buttons || buttons.length === 0) return;
    const curIdx = buttons.findIndex(b => b.getAttribute('data-date') === journalState.currentDate || b.classList.contains('active'));
    const btn = (curIdx >= 0) ? buttons[curIdx] : buttons[0];
    if (btn) {
        const date = btn.getAttribute('data-date');
        if (date && date !== journalState.currentDate) {
            // Use selectDate to ensure unsaved edits are handled
            selectDate(date);
        }
        try {
            btn.focus();
        } catch (err) {
        }
    }
}

export function canReturnToTabs() {
    const buttons = Array.from(document.querySelectorAll('.journal-date-btn')).filter(b => b.offsetParent !== null);
    if (!buttons || buttons.length === 0) return true;
    const currentIndex = buttons.findIndex(b => b.getAttribute('data-date') === journalState.currentDate || b.classList.contains('active'));
    return currentIndex <= 0; // at top or no selection
}

// Export init function for the web UI
export async function init(container) {

    // Set up event listeners for buttons
    const backBtn = document.getElementById('journal-back-btn');
    const editBtn = document.getElementById('journal-edit-btn');
    const saveBtn = document.getElementById('journal-save-btn');
    const cancelBtn = document.getElementById('journal-cancel-btn');

    if (backBtn) {
        backBtn.addEventListener('click', () => {
            if (journalState.isEditing) {
                journalState.isEditing = false;
                updateEditModeUI();
            }
            showListPanel();
        });
    }
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

    // Keyboard navigation for date list (up/down arrows)
    if (!journalState.keyboardBound) {
        document.addEventListener('keydown', (e) => {
            // Ignore when editing or when focus is on an input/textarea/contenteditable
            if (journalState.isEditing) return;
            const tag = e.target && e.target.tagName ? e.target.tagName.toUpperCase() : '';
            if (tag === 'TEXTAREA' || tag === 'INPUT' || e.target.isContentEditable) return;

            if (e.key === 'ArrowUp' || e.key === 'ArrowDown') {
                // Determine visible buttons only (respect collapsed months)
                const buttons = Array.from(document.querySelectorAll('.journal-date-btn')).filter(b => b.offsetParent !== null);
                if (!buttons || buttons.length === 0) return;

                const currentIndex = buttons.findIndex(b => b.getAttribute('data-date') === journalState.currentDate || b.classList.contains('active'));
                let nextIndex = currentIndex;

                if (e.key === 'ArrowUp') {
                    if (currentIndex === -1) {
                        nextIndex = 0; // default to first
                        e.preventDefault();
                    } else if (currentIndex > 0) {
                        e.preventDefault();
                        nextIndex = currentIndex - 1;
                    } else {
                        // At top of list: allow event to bubble so app-level navigation can return focus to tabs
                        return;
                    }
                } else { // ArrowDown
                    if (currentIndex === -1) {
                        nextIndex = 0;
                        e.preventDefault();
                    } else {
                        e.preventDefault();
                        nextIndex = currentIndex >= buttons.length - 1 ? 0 : currentIndex + 1;
                    }
                }

                const nextBtn = buttons[nextIndex];
                if (nextBtn) {
                    const date = nextBtn.getAttribute('data-date');
                    if (date) {
                        selectDate(date);
                        try {
                            nextBtn.focus();
                        } catch (err) {
                        }
                    }
                }
            }
        }, true);
        journalState.keyboardBound = true;
    }

    // Load initial data
    await refreshJournalDates();
    await loadJournalEntry(journalState.currentDate);
    updateDateButtons();
}

