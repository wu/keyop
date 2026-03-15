// Tasks Web UI
// Displays tasks scheduled for today or completed today, grouped by tags

import {formatAge} from '/js/time-formatter.js';
import {ServiceFilterNav} from '/js/service-filter-nav.js';

let elements = {
    container: null,
    viewList: null,
    tagList: null,
    tasksList: null,
    searchInput: null,
    newTaskInput: null,
    taskEditorModal: null,
    taskEditorClose: null,
    taskEditorCancel: null,
    taskEditorSave: null,
    taskTitle: null,
    taskColor: null,
    taskColorHex: null,
    taskScheduledDate: null,
    taskScheduledTime: null,
    taskHasScheduledTime: null,
    taskScheduledTimeGroup: null,
    taskRecurrenceType: null,
    taskRecurrenceInterval: null,
    taskTags: null,
    recurrenceWeeklyDays: null,
    dayCheckboxes: null
};

let state = {
    currentView: 'today',
    currentFilter: 'all',
    tasks: [],
    tagCounts: {},
    collapsedSections: {}, // Track which sections are collapsed
    searchQuery: '', // Track current search query
    currentEditingTask: null // Task currently being edited
};

let navController = null;

async function loadTasks() {
    try {
        const response = await fetch('/api/tabs/tasks/action/fetch-tasks', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({view: state.currentView})
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();

        if (result.error) {
            console.error('[tasks] Error in result:', result.error);
            elements.tasksList.innerHTML = `<div class="error">Error: ${escapeHtml(result.error)}</div>`;
            return;
        }

        state.tasks = result.tasks || [];
        state.tagCounts = result.tagCounts || {};

        // Update tag filter list
        updateTagList();

        // Display tasks
        displayTasks();

        // Setup navigation after DOM is ready
        if (!navController) {
            setupNavigation();
        }
    } catch (err) {
        console.error('[tasks] Exception in loadTasks:', err);
        elements.tasksList.innerHTML = `<div class="error">Failed to load tasks: ${escapeHtml(err.message)}</div>`;
    }
}

function setupNavigation() {
    navController = new ServiceFilterNav({
        container: elements.container,
        itemSelector: '.task-item',
        serviceSelector: '.tag-item',
        selectedClass: 'task-selected',
        markedClass: 'task-marked',
        onStateChange: (state) => {
            // Handle state changes if needed
        }
    });

    // Override the service filter to use tags
    navController.applyServiceFilter = function () {
        const allItems = this.getItems();
        allItems.forEach(item => {
            if (this.selectedService === 'all') {
                item.style.display = '';
            } else {
                // Check if any of the task's tags match the selected tag
                const allTags = item.dataset.allTags || '';
                const hasTag = allTags.split(',').map(t => t.trim()).includes(this.selectedService);
                item.style.display = hasTag ? '' : 'none';
            }
        });
    };

    // Override getVisibleItems to use data-all-tags instead of data-serviceName
    navController.getVisibleItems = function () {
        return this.getItems().filter(item => {
            if (this.selectedService === 'all') return true;
            const allTags = item.dataset.allTags || '';
            return allTags.split(',').map(t => t.trim()).includes(this.selectedService);
        });
    };

    // Override handleEnter to apply tag filter instead of marking items
    const originalHandleEnter = navController.handleEnter.bind(navController);
    navController.handleEnter = function () {
        // For tasks, handleServicesKeydown already applied the filter, so nothing to do here
    };

    // Override handleServicesKeydown to use data-tag instead of data-service
    const originalHandleServicesKeydown = navController.handleServicesKeydown.bind(navController);
    navController.handleServicesKeydown = function (e, services) {
        if (e.key === 'ArrowDown') {
            e.preventDefault();
            if (this.selectedServiceIndex < services.length - 1) {
                this.selectServiceInMenu(this.selectedServiceIndex + 1);
            }
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            if (this.selectedServiceIndex > 0) {
                this.selectServiceInMenu(this.selectedServiceIndex - 1);
            }
        } else if (e.key === 'ArrowRight') {
            e.preventDefault();
            // Switch focus from services to items
            this.focusOnServices = false;
            services.forEach(s => s.classList.remove('menu-selected'));
            const visibleItems = this.getVisibleItems();
            if (visibleItems.length > 0) {
                this.selectItem(0);
            }
            this.updateState();
        } else if (e.key === 'Enter' || e.key === ' ') {
            e.preventDefault();
            // Select this tag and apply filter
            const tag = services[this.selectedServiceIndex];
            if (tag) {
                const tagName = tag.dataset.tag;  // Use data-tag instead of data-service
                this.selectedService = tagName;
                this.selectedIndex = -1;
                this.applyServiceFilter();

                // Update UI to show this tag is active
                services.forEach(s => s.classList.remove('active'));
                tag.classList.add('active');
                this.updateState();

                // Also update our state
                state.currentFilter = tagName;
            }
        }
    };

    // Override the handleServiceItemClick to filter tasks
    navController.handleServiceItemClick = function (serviceItem) {
        const tag = serviceItem.dataset.tag;
        if (!tag) {
            console.warn('[ServiceFilterNav] No tag data attribute found');
            return;
        }

        this.selectedService = tag;
        this.selectedIndex = -1;
        this.focusOnServices = false;
        this.applyServiceFilter();

        // Update UI to show this tag is active
        const tags = this.getServiceItems();
        tags.forEach(t => t.classList.remove('active'));
        serviceItem.classList.add('active');
        this.updateState();

        // Also update our state
        state.currentFilter = tag;
    };
}

function updateTagList() {
    // Preserve "all" item
    const allItem = elements.tagList.querySelector('[data-tag="all"]');
    if (allItem) {
        const count = state.tasks.length;
        allItem.innerHTML = `all <span class="service-count">${count}</span>`;
    }

    // Remove existing tag items (keep "all")
    const tagItems = elements.tagList.querySelectorAll('[data-tag]:not([data-tag="all"])');
    tagItems.forEach(item => item.remove());

    // Add tags sorted by count
    const sortedTags = Object.entries(state.tagCounts)
        .sort((a, b) => b[1] - a[1])
        .map(([tag, count]) => ({tag, count}));

    for (const {tag, count} of sortedTags) {
        const item = document.createElement('div');
        item.className = 'tag-item';
        item.dataset.tag = tag;
        item.innerHTML = `${tag} <span class="service-count">${count}</span>`;
        elements.tagList.appendChild(item);
    }
}

function displayTasks() {
    if (state.tasks.length === 0) {
        elements.tasksList.innerHTML = '<div class="no-tasks">No tasks scheduled or completed today</div>';
        return;
    }

    // Group tasks by tag first
    const groupedByTag = {};

    for (const task of state.tasks) {
        // Get task tags
        const tags = task.tags ? task.tags.split(',').map(t => t.trim()).filter(t => t) : [];
        if (tags.length === 0) tags.push('untagged');

        // Add task to each of its tags
        for (const tag of tags) {
            if (!groupedByTag[tag]) {
                groupedByTag[tag] = [];
            }
            groupedByTag[tag].push(task);
        }
    }

    // Filter by current tag to get tasks to display
    let tasksToShow = [];
    if (state.currentFilter === 'all') {
        tasksToShow = state.tasks;
    } else if (groupedByTag[state.currentFilter]) {
        tasksToShow = groupedByTag[state.currentFilter];
    }

    // Apply search filter to current view
    if (state.searchQuery) {
        tasksToShow = tasksToShow.filter(task => {
            const titleMatch = task.title.toLowerCase().includes(state.searchQuery);
            const tagsMatch = task.tags && task.tags.toLowerCase().includes(state.searchQuery);
            return titleMatch || tagsMatch;
        });
    }

    // For 'recent' view, show all tasks in a single list sorted by updated_at
    if (state.currentView === 'recent') {
        tasksToShow.sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));
        elements.tasksList.innerHTML = tasksToShow.map(task => createTaskElement(task)).join('');
    } else {
        // Now group by completion status and category
        const todayIncomplete = tasksToShow.filter(t => !t.done && (!t.category || t.category === 'today'));
        const pastIncomplete = tasksToShow.filter(t => !t.done && t.category === 'past');
        const completed = tasksToShow.filter(t => t.done);

        // Sort incomplete by scheduled date (earliest first)
        todayIncomplete.sort((a, b) => new Date(a.scheduledAt) - new Date(b.scheduledAt));
        pastIncomplete.sort((a, b) => new Date(a.scheduledAt) - new Date(b.scheduledAt));

        // Sort completed by completed date (most recent first)
        completed.sort((a, b) => new Date(b.completedAt) - new Date(a.completedAt));

        // Build HTML with completion status groups and collapsible headers
        let html = '';

        // Incomplete tasks section (for today)
        if (todayIncomplete.length > 0) {
            const sectionId = 'incomplete-section';
            const isCollapsed = state.collapsedSections && state.collapsedSections[sectionId];
            html += `<div class="task-group">
                    <div class="task-group-header collapsible" data-section="${sectionId}">
                        <span class="collapse-icon">${isCollapsed ? '▶' : '▼'}</span>
                        Incomplete (${todayIncomplete.length})
                    </div>
                    <div class="task-group-items ${isCollapsed ? 'collapsed' : ''}">
                        ${todayIncomplete.map(task => createTaskElement(task)).join('')}
                    </div>
                </div>`;
        }

        // Past incomplete tasks section (only for today view)
        if (pastIncomplete.length > 0) {
            const sectionId = 'past-section';
            const isCollapsed = state.collapsedSections && state.collapsedSections[sectionId];
            html += `<div class="task-group">
                    <div class="task-group-header collapsible" data-section="${sectionId}">
                        <span class="collapse-icon">${isCollapsed ? '▶' : '▼'}</span>
                        Past (${pastIncomplete.length})
                    </div>
                    <div class="task-group-items ${isCollapsed ? 'collapsed' : ''}">
                        ${pastIncomplete.map(task => createTaskElement(task)).join('')}
                    </div>
                </div>`;
        }

        // Completed tasks section
        if (completed.length > 0) {
            const sectionId = 'completed-section';
            const isCollapsed = state.collapsedSections && state.collapsedSections[sectionId];
            html += `<div class="task-group">
                    <div class="task-group-header collapsible" data-section="${sectionId}">
                        <span class="collapse-icon">${isCollapsed ? '▶' : '▼'}</span>
                        Completed (${completed.length})
                    </div>
                    <div class="task-group-items ${isCollapsed ? 'collapsed' : ''}">
                        ${completed.map(task => createTaskElement(task)).join('')}
                    </div>
                </div>`;
        }

        elements.tasksList.innerHTML = html;

        // Add event listeners to collapsible headers
        document.querySelectorAll('.task-group-header.collapsible').forEach(header => {
            header.addEventListener('click', (e) => {
                e.preventDefault();
                const sectionId = header.dataset.section;
                if (!state.collapsedSections) state.collapsedSections = {};
                state.collapsedSections[sectionId] = !state.collapsedSections[sectionId];

                const itemsDiv = header.nextElementSibling;
                itemsDiv.classList.toggle('collapsed');

                const icon = header.querySelector('.collapse-icon');
                icon.textContent = state.collapsedSections[sectionId] ? '▶' : '▼';
            });
        });
    }

    // Add event listeners to task items
    document.querySelectorAll('.task-item').forEach(item => {
        const checkbox = item.querySelector('.task-checkbox');
        if (checkbox) {
            checkbox.addEventListener('click', async (e) => {
                e.stopPropagation();
                const taskID = parseInt(item.dataset.taskId);
                await toggleTask(taskID);
            });
        }

        // Click on task title or content to open editor
        item.addEventListener('click', (e) => {
            if (e.target !== checkbox && !e.target.closest('.task-checkbox')) {
                const taskID = parseInt(item.dataset.taskId);
                const task = state.tasks.find(t => t.id === taskID);
                if (task) {
                    openTaskEditor(task);
                }
            }
        });
    });
}

function createTaskElement(task) {
    const checkboxClass = task.done ? 'task-checkbox checked' : 'task-checkbox';
    const taskClass = task.done ? 'task-item completed' : 'task-item';
    const flagClass = task.flag ? ' flagged' : '';

    let priority = '';
    if (task.importance > 0 || task.urgency > 0) {
        priority = `<span class="task-priority">!${Math.max(task.importance, task.urgency)}</span>`;
    }

    let recurring = '';
    if (task.recurring || task.recurrence) {
        recurring = `<span class="task-recurring" title="This task recurs">↻</span>`;
    }

    // Show time based on current view
    let timeDisplay = '';
    if (state.currentView === 'recent') {
        // For recent view, show time since last update
        const timeAgo = formatAge(task.updatedAt);
        timeDisplay = `<div class="task-time recent-time">${timeAgo}</div>`;
    } else {
        // For other views, show time for incomplete tasks (until/since scheduled) or completed tasks (completed ago)
        if (!task.done && task.scheduledAt) {
            if (task.hasScheduledTime) {
                const scheduled = new Date(task.scheduledAt);
                const now = new Date();
                const isPast = scheduled < now;
                const timeClass = isPast ? 'past' : 'future';
                const timeAgo = formatAge(task.scheduledAt);

                // Display local time for clarity
                const hours = scheduled.getHours();
                const minutes = String(scheduled.getMinutes()).padStart(2, '0');
                const ampm = hours >= 12 ? 'pm' : 'am';
                const displayHours = hours % 12 || 12;
                const localTime = `${displayHours}:${minutes}${ampm}`;

                timeDisplay = `<div class="task-time ${timeClass}">${localTime} (${timeAgo})</div>`;
            } else {
                // Just a date without time - show the date or nothing
                const datePart = task.scheduledAt.split('T')[0];
                timeDisplay = `<div class="task-time">${datePart}</div>`;
            }
        } else if (task.done && task.completedAt) {
            const scheduled = new Date(task.scheduledAt);
            const timeAgo = formatAge(task.completedAt);

            let localTime = '';
            if (task.hasScheduledTime) {
                const hours = scheduled.getHours();
                const minutes = String(scheduled.getMinutes()).padStart(2, '0');
                const ampm = hours >= 12 ? 'pm' : 'am';
                const displayHours = hours % 12 || 12;
                localTime = `${displayHours}:${minutes}${ampm} - `;
            }

            timeDisplay = `<div class="task-time completed-time">${localTime}${timeAgo}</div>`;
        }
    }

    // Get task tags for filtering
    const taskTags = task.tags ? task.tags.split(',').map(t => t.trim()).filter(t => t) : [];
    const primaryTag = taskTags.length > 0 ? taskTags[0] : 'untagged';

    // Add inline style for task color if available
    const colorStyle = task.color ? ` style="border-left: 4px solid ${task.color}; padding-left: 8px;"` : '';

    return `<div class="${taskClass}${flagClass}" data-task-id="${task.id}" data-tag="${primaryTag}" data-all-tags="${escapeHtml(taskTags.join(',') || 'untagged')}"${colorStyle}>
            <div class="task-checkbox" title="Toggle task completion"></div>
            <div class="task-content">
                <div class="task-title">${escapeHtml(task.title)}</div>
                ${timeDisplay}
                ${task.tags ? `<div class="task-tags">${task.tags.split(',').map(t => `<span class="tag-badge">${escapeHtml(t.trim())}</span>`).join('')}</div>` : ''}
            </div>
            ${recurring}
            ${priority}
            <div class="task-status">${task.done ? '✓' : ''}</div>
        </div>`;
}

async function toggleTask(taskID) {
    try {
        const response = await fetch('/api/tabs/tasks/action/toggle-task', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({taskID})
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();

        if (result.error) {
            console.error('Error toggling task:', result.error);
            return;
        }

        // Reload tasks
        await loadTasks();
    } catch (err) {
        console.error('Failed to toggle task:', err);
    }
}

function escapeHtml(text) {
    const map = {
        '&': '&amp;',
        '<': '&lt;',
        '>': '&gt;',
        '"': '&quot;',
        "'": '&#039;'
    };
    return text.replace(/[&<>"']/g, c => map[c]);
}

function setupViewSwitching() {
    const viewItems = document.querySelectorAll('.view-item');
    viewItems.forEach(item => {
        item.addEventListener('click', async (e) => {
            e.preventDefault();
            const newView = item.dataset.view;
            if (newView && newView !== state.currentView) {
                state.currentView = newView;

                // Update active state
                viewItems.forEach(v => v.classList.remove('active'));
                item.classList.add('active');

                // Reload tasks
                await loadTasks();
            }
        });
    });
}

function setupSearchInput() {
    elements.searchInput = document.getElementById('task-search');
    if (elements.searchInput) {
        elements.searchInput.addEventListener('input', (e) => {
            state.searchQuery = e.target.value.toLowerCase();
            displayTasks();
        });
    }
}

function setupNewTaskInput() {
    elements.newTaskInput = document.getElementById('new-task-input');
    if (elements.newTaskInput) {
        elements.newTaskInput.addEventListener('keypress', async (e) => {
            if (e.key === 'Enter') {
                const title = e.target.value.trim();
                if (title) {
                    await createTask(title);
                    e.target.value = '';
                }
            }
        });
    }
}

async function createTask(title) {
    try {
        // Create a local date for "today" and send it as an ISO string
        const today = new Date();
        // Reset time to midnight for date-only task (by default) in local timezone
        const localToday = new Date(today.getFullYear(), today.getMonth(), today.getDate(), 0, 0, 0, 0);

        const response = await fetch('/api/tabs/tasks/action/create-task', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                title,
                scheduledAt: localToday.toISOString(),
                hasScheduledTime: false
            })
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();

        if (result.error) {
            console.error('[tasks] Error creating task:', result.error);
            alert('Error creating task: ' + result.error);
        } else {
            // Reload tasks to show the new one
            await loadTasks();
        }
    } catch (err) {
        console.error('[tasks] Exception creating task:', err);
        alert('Failed to create task: ' + err.message);
    }
}

// Initialize on module load
elements.container = document.getElementById('tasks-container');
elements.viewList = document.querySelector('.view-list');
elements.tagList = document.querySelector('.tag-list');
elements.tasksList = document.getElementById('tasks-list');
elements.newTaskInput = document.getElementById('new-task-input');

// Setup view switching
setupViewSwitching();

// Setup search input
setupSearchInput();

// Setup new task input
setupNewTaskInput();

// Initialize task editor
initializeTaskEditor();

// Load tasks on startup
loadTasks();

// Refresh tasks every 30 seconds
setInterval(loadTasks, 30000);

// Export init function for app.js
export function init(container) {
    elements.container = container.closest('#tasks-container') || document.getElementById('tasks-container');
    elements.viewList = container.querySelector('.view-list');
    elements.tagList = container.querySelector('.tag-list');
    elements.tasksList = container.querySelector('#tasks-list');
    elements.searchInput = container.querySelector('#task-search');
}

// Export navigation functions for app.js
export function focusItems() {
    // Focus on the first task
    if (navController) {
        navController.focusOnServices = false;
        const visibleItems = navController.getVisibleItems();
        if (visibleItems.length > 0) {
            // Set selectedIndex to -1 so that when the ServiceFilterNav keyboard handler runs
            // and processes the same arrow event, it will select item 0 (not move to item 1)
            navController.selectedIndex = -1;
        }
    }
}

export function canReturnToTabs() {
    // Can return to tabs if:
    // - Focus is on services/tags (not on items)
    // - OR focus is on first item
    if (!navController) return true;
    if (navController.focusOnServices) return true;
    return navController.selectedIndex <= 0;
}

// Task Editor Dialog Functions
function initializeTaskEditor() {
    elements.taskEditorModal = document.getElementById('task-editor-modal');
    elements.taskEditorClose = document.getElementById('task-editor-close');
    elements.taskEditorCancel = document.getElementById('task-editor-cancel');
    elements.taskEditorSave = document.getElementById('task-editor-save');
    elements.taskTitle = document.getElementById('task-title');
    elements.taskColor = document.getElementById('task-color');
    elements.taskColorHex = document.getElementById('task-color-hex');
    elements.taskScheduledDate = document.getElementById('task-scheduled-date');
    elements.taskScheduledTime = document.getElementById('task-scheduled-time');
    elements.taskHasScheduledTime = document.getElementById('task-has-scheduled-time');
    elements.taskScheduledTimeGroup = document.getElementById('task-scheduled-time-group');
    elements.taskRecurrenceType = document.getElementById('task-recurrence-type');
    elements.taskRecurrenceInterval = document.getElementById('task-recurrence-interval');
    elements.taskTags = document.getElementById('task-tags');
    elements.recurrenceWeeklyDays = document.getElementById('recurrence-weekly-days');
    elements.dayCheckboxes = document.querySelectorAll('.day-checkbox');

    // Event listeners
    elements.taskEditorClose.addEventListener('click', closeTaskEditor);
    elements.taskEditorCancel.addEventListener('click', closeTaskEditor);
    elements.taskEditorSave.addEventListener('click', saveTask);
    elements.taskRecurrenceType.addEventListener('change', updateRecurrenceUI);
    elements.taskHasScheduledTime.addEventListener('change', updateScheduledTimeUI);

    // Color picker sync
    elements.taskColor.addEventListener('input', (e) => {
        elements.taskColorHex.value = e.target.value.toUpperCase();
    });
    elements.taskColorHex.addEventListener('input', (e) => {
        let hex = e.target.value.trim();
        if (!hex.startsWith('#')) {
            hex = '#' + hex;
        }
        if (/^#[0-9A-Fa-f]{6}$/.test(hex)) {
            elements.taskColor.value = hex;
        }
    });

    // Handle keyboard shortcuts (Enter to save, Escape to cancel)
    document.addEventListener('keydown', (e) => {
        if (!elements.taskEditorModal.classList.contains('active')) {
            return;
        }
        if (e.key === 'Enter') {
            // Don't intercept Enter if focus is on a select/textarea (future-proofing)
            // For now, all inputs are text/color/number/date/time, so Enter saves
            e.preventDefault();
            saveTask();
        } else if (e.key === 'Escape') {
            e.preventDefault();
            closeTaskEditor();
        }
    });
}

function updateScheduledTimeUI() {
    if (elements.taskHasScheduledTime.checked) {
        elements.taskScheduledTimeGroup.style.display = 'block';
    } else {
        elements.taskScheduledTimeGroup.style.display = 'none';
        elements.taskScheduledTime.value = '';
    }
}

function openTaskEditor(task) {
    state.currentEditingTask = task;

    // Populate form
    elements.taskTitle.value = task.title || '';

    // Handle color: use black (#000000) if not set, otherwise use the stored color
    const color = task.color && task.color.trim() ? task.color : '#000000';
    const colorHex = task.color && task.color.trim() ? task.color.toUpperCase() : '';
    elements.taskColor.value = color;
    elements.taskColorHex.value = colorHex;

    elements.taskTags.value = task.tags || '';

    // Support both new "pattern" object and legacy DB fields (recurrence, recurrence_days, recurrence_x)
    const recurrenceType = (task.pattern && task.pattern.type) || task.recurrence || task.recurrenceType || '';
    elements.taskRecurrenceType.value = recurrenceType || '';
    const recurrenceInterval = (task.pattern && task.pattern.interval) || task.recurrence_x || task.recurrenceInterval || 1;
    elements.taskRecurrenceInterval.value = recurrenceInterval;

    // Set scheduled date/time
    if (task.scheduledAt) {
        const scheduledDate = new Date(task.scheduledAt);

        // Set date input from the local date part (YYYY-MM-DD)
        const year = scheduledDate.getFullYear();
        const month = String(scheduledDate.getMonth() + 1).padStart(2, '0');
        const day = String(scheduledDate.getDate()).padStart(2, '0');
        elements.taskScheduledDate.value = `${year}-${month}-${day}`;

        // Use the explicit hasScheduledTime boolean from the API
        const hasTime = task.hasScheduledTime === true || task.hasScheduledTime === 1;
        elements.taskHasScheduledTime.checked = hasTime;
        updateScheduledTimeUI();

        if (hasTime) {
            // Use local time to match what user sees in the task list (via formatAge)
            const hours = String(scheduledDate.getHours()).padStart(2, '0');
            const minutes = String(scheduledDate.getMinutes()).padStart(2, '0');
            elements.taskScheduledTime.value = `${hours}:${minutes}`;
        }
    } else {
        elements.taskHasScheduledTime.checked = false;
        updateScheduledTimeUI();
    }

    // Set weekly days if applicable
    if (recurrenceType === 'weekly') {
        // Prefer pattern.daysOfWeek when available, otherwise parse recurrence_days CSV
        let days = [];
        if (task.pattern && Array.isArray(task.pattern.daysOfWeek)) {
            days = task.pattern.daysOfWeek.map(d => parseInt(d, 10));
        } else if (task.recurrence_days) {
            days = task.recurrence_days.split(',').map(s => parseInt(s.trim(), 10)).filter(n => !isNaN(n));
        }
        elements.dayCheckboxes.forEach(checkbox => {
            checkbox.checked = days.indexOf(parseInt(checkbox.value, 10)) !== -1;
        });
    } else {
        elements.dayCheckboxes.forEach(checkbox => {
            checkbox.checked = false;
        });
    }

    updateRecurrenceUI();
    elements.taskEditorModal.classList.add('active');
}

function closeTaskEditor() {
    elements.taskEditorModal.classList.remove('active');
    state.currentEditingTask = null;
}

function updateRecurrenceUI() {
    const recurrenceType = elements.taskRecurrenceType.value;
    if (recurrenceType === 'weekly') {
        elements.recurrenceWeeklyDays.style.display = 'block';
    } else {
        elements.recurrenceWeeklyDays.style.display = 'none';
    }
}

async function saveTask() {
    if (!state.currentEditingTask) return;

    const daysOfWeek = [];
    if (elements.taskRecurrenceType.value === 'weekly') {
        elements.dayCheckboxes.forEach(checkbox => {
            if (checkbox.checked) {
                daysOfWeek.push(parseInt(checkbox.value));
            }
        });
    }

    const pattern = elements.taskRecurrenceType.value ? {
        type: elements.taskRecurrenceType.value,
        interval: parseInt(elements.taskRecurrenceInterval.value),
        daysOfWeek: daysOfWeek,
        dayOfMonth: 1,
        month: 3
    } : null;

    // Only include time if hasScheduledTime is checked
    let scheduledDateISO = '';
    if (elements.taskScheduledDate.value) {
        if (elements.taskHasScheduledTime.checked && elements.taskScheduledTime.value) {
            // Create a Date object from the local inputs and get ISO string
            const [year, month, day] = elements.taskScheduledDate.value.split('-').map(Number);
            const [hours, minutes] = elements.taskScheduledTime.value.split(':').map(Number);
            const localDateTime = new Date(year, month - 1, day, hours, minutes);
            scheduledDateISO = localDateTime.toISOString();
        } else {
            // Date-only: parse as local midnight and get ISO string
            const [year, month, day] = elements.taskScheduledDate.value.split('-').map(Number);
            const localDate = new Date(year, month - 1, day, 0, 0, 0);
            scheduledDateISO = localDate.toISOString();
        }
    }

    try {
        const response = await fetch('/api/tabs/tasks/action/update-task', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                taskId: state.currentEditingTask.id,
                title: elements.taskTitle.value,
                color: elements.taskColor.value,
                tags: elements.taskTags.value,
                scheduledAt: scheduledDateISO,
                hasScheduledTime: elements.taskHasScheduledTime.checked,
                pattern: pattern
            })
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();

        if (result.error) {
            console.error('[tasks] Error saving task:', result.error);
            alert('Error saving task: ' + result.error);
            return;
        }

        closeTaskEditor();
        await loadTasks();
    } catch (err) {
        console.error('[tasks] Error saving task:', err);
        alert('Error saving task: ' + err.message);
    }
}
