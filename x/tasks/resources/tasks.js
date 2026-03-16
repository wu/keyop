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
    currentEditingTask: null, // Task currently being edited
    expandedParents: new Set() // Track which parent task UUIDs are expanded
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

        // Re-expand subtasks that were previously expanded
        restoreExpandedSubtasks();

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

    // Attach listeners and enhance items for subtasks
    attachTaskItemListeners(elements.tasksList);
    // Add subtask toggle icons and attach their handlers
    enhanceTaskItemsForSubtasks();
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

    // Add new subtask button if this task has subtasks (i.e., it's a parent)
    const addSubtaskBtn = task.hasSubtasks && task.uuid ? `<button class="task-add-subtask" title="Add subtask" data-task-uuid="${task.uuid}">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="12" y1="5" x2="12" y2="19"></line>
                    <line x1="5" y1="12" x2="19" y2="12"></line>
                </svg>
            </button>` : '';

    return `<div class="${taskClass}${flagClass}" data-task-id="${task.id}" data-task-uuid="${task.uuid || ''}" data-tag="${primaryTag}" data-all-tags="${escapeHtml(taskTags.join(',') || 'untagged')}"${colorStyle}>
            <div class="${checkboxClass}" title="Toggle task completion"></div>
            <div class="task-content">
                <div class="task-title">${escapeHtml(task.title)}</div>
                ${timeDisplay}
                ${task.tags ? `<div class="task-tags">${task.tags.split(',').map(t => `<span class="tag-badge">${escapeHtml(t.trim())}</span>`).join('')}</div>` : ''}
            </div>
            ${recurring}
            ${priority}
            <div class="task-status">${task.done ? '✓' : ''}</div>
            ${addSubtaskBtn}
            <button class="task-delete" title="Delete task" data-task-id="${task.id}">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                    <polyline points="3 6 5 6 21 6"></polyline>
                    <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                    <line x1="10" y1="11" x2="10" y2="17"></line>
                    <line x1="14" y1="11" x2="14" y2="17"></line>
                </svg>
            </button>
        </div>`;
}

async function loadSubtasksForParent(uuid, parentEl) {
    if (!uuid || !parentEl) return;
    // If already loaded, toggle visibility and update state
    let next = parentEl.nextElementSibling;
    if (next && next.classList && next.classList.contains('subtasks-container')) {
        const isHidden = next.style.display === 'none';
        next.style.display = isHidden ? 'flex' : 'none';
        const icon = parentEl.querySelector('.subtask-toggle');
        if (icon) icon.textContent = isHidden ? '▼' : '▶';
        // Update state
        if (isHidden) {
            state.expandedParents.add(uuid);
        } else {
            state.expandedParents.delete(uuid);
        }
        return;
    }

    try {
        console.log('[tasks] Fetching subtasks for parent UUID:', uuid);
        const resp = await fetch('/api/tabs/tasks/action/fetch-subtasks', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({parentUuid: uuid})
        });
        if (!resp.ok) {
            console.error('[tasks] fetch-subtasks http error', resp.status);
            return;
        }
        const result = await resp.json();
        console.log('[tasks] fetch-subtasks response:', result);
        if (result.error) {
            console.error('[tasks] fetch-subtasks error', result.error);
            return;
        }
        const childTasks = result.tasks || [];
        console.log('[tasks] Received', childTasks.length, 'subtasks');
        if (childTasks.length === 0) {
            console.warn('[tasks] No subtasks returned from server');
        }
        const container = document.createElement('div');
        container.className = 'subtasks-container';
        container.dataset.parentId = parentEl.dataset.taskId;
        try {
            container.innerHTML = childTasks.map((t, idx) => {
                console.log('[tasks] Creating element for subtask', idx, t);
                return createTaskElement(t);
            }).join('');
        } catch (renderErr) {
            console.error('[tasks] Error rendering subtasks:', renderErr);
            return;
        }
        parentEl.parentNode.insertBefore(container, parentEl.nextSibling);
        console.log('[tasks] Inserted subtasks container with', childTasks.length, 'tasks');
        const icon = parentEl.querySelector('.subtask-toggle');
        if (icon) icon.textContent = '▼';
        // Mark this parent as expanded in state
        state.expandedParents.add(uuid);
        attachTaskItemListeners(container);
        setupSubtaskDragAndDrop(container, uuid);
    } catch (err) {
        console.error('[tasks] loadSubtasksForParent error', err);
    }
}

function restoreExpandedSubtasks() {
    // Re-expand all subtasks that were previously expanded
    if (state.expandedParents.size === 0) return;

    state.expandedParents.forEach(parentUuid => {
        // Find the parent task element by UUID
        const parentEl = Array.from(document.querySelectorAll('.task-item')).find(el => {
            return el.dataset.taskUuid === parentUuid;
        });

        if (parentEl) {
            console.log('[tasks] Restoring expanded state for parent:', parentUuid);
            loadSubtasksForParent(parentUuid, parentEl);
        }
    });
}

function attachTaskItemListeners(container) {
    container = container || document;
    container.querySelectorAll('.task-item').forEach(item => {
        const checkbox = item.querySelector('.task-checkbox');
        if (checkbox) {
            checkbox.onclick = async (e) => {
                e.stopPropagation();
                const taskID = parseInt(item.dataset.taskId);
                await toggleTask(taskID);
            };
        }
        const toggle = item.querySelector('.subtask-toggle');
        if (toggle) {
            toggle.onclick = (e) => {
                e.stopPropagation();
                const uuid = toggle.dataset.uuid;
                if (!uuid) return;
                loadSubtasksForParent(uuid, item);
            };
        }
        const deleteBtn = item.querySelector('.task-delete');
        if (deleteBtn) {
            deleteBtn.onclick = async (e) => {
                e.stopPropagation();
                const taskID = parseInt(deleteBtn.dataset.taskId);
                await deleteTask(taskID);
            };
        }
        const addSubtaskBtn = item.querySelector('.task-add-subtask');
        if (addSubtaskBtn) {
            addSubtaskBtn.onclick = async (e) => {
                e.stopPropagation();
                const uuid = addSubtaskBtn.dataset.taskUuid;
                if (uuid) await createNewSubtask(uuid);
            };
        }
        item.onclick = (e) => {
            if (e.target.closest('.task-checkbox') || e.target.closest('.subtask-toggle') || e.target.closest('.task-delete') || e.target.closest('.task-add-subtask')) return;
            const taskID = parseInt(item.dataset.taskId);
            const task = state.tasks.find(t => t.id === taskID);
            if (task) openTaskEditor(task);
        };
    });
}

function enhanceTaskItemsForSubtasks() {
    if (!elements.tasksList) return;
    for (const task of state.tasks) {
        if (!task.hasSubtasks || !task.uuid) continue;
        const el = elements.tasksList.querySelector(`.task-item[data-task-id="${task.id}"]`);
        if (!el) continue;
        if (el.querySelector('.subtask-toggle')) continue; // already added
        const checkbox = el.querySelector('.task-checkbox');
        const toggle = document.createElement('span');
        toggle.className = 'subtask-toggle';
        toggle.dataset.uuid = task.uuid;
        toggle.textContent = '▶';
        toggle.onclick = (e) => {
            e.stopPropagation();
            loadSubtasksForParent(task.uuid, el);
        };
        if (checkbox && checkbox.parentNode) {
            checkbox.parentNode.insertBefore(toggle, checkbox.nextSibling);
        } else {
            el.insertBefore(toggle, el.firstChild);
        }
    }
}

function setupSubtaskDragAndDrop(container, parentUuid) {
    let draggedItem = null;
    let draggedFromIndex = null;

    const items = container.querySelectorAll('.task-item');

    // Helper: Calculate boundary between incomplete and complete tasks dynamically
    function getIncompleteBoundary() {
        let boundary = 0;
        container.querySelectorAll('.task-item').forEach((item, idx) => {
            if (!item.classList.contains('completed')) {
                boundary = idx + 1;
            }
        });
        return boundary;
    }

    // Helper: Update visual divider position
    function updateDivider() {
        container.querySelectorAll('.task-item').forEach(item => {
            item.style.borderBottomWidth = '';
            item.style.borderBottomStyle = '';
            item.style.borderBottomColor = '';
        });

        const boundary = getIncompleteBoundary();
        const allItems = container.querySelectorAll('.task-item');
        if (boundary > 0 && boundary < allItems.length) {
            allItems[boundary - 1].style.borderBottomWidth = '2px';
            allItems[boundary - 1].style.borderBottomStyle = 'dashed';
            allItems[boundary - 1].style.borderBottomColor = '#999';
        }
    }

    items.forEach(item => {
        item.draggable = true;
        item.style.cursor = 'grab';

        item.addEventListener('dragstart', (e) => {
            draggedItem = item;
            draggedFromIndex = Array.from(items).indexOf(item);
            item.style.opacity = '0.5';
            item.style.cursor = 'grabbing';
            e.dataTransfer.effectAllowed = 'move';
            e.dataTransfer.setData('text/html', item.innerHTML);
            console.log('[tasks] Started dragging subtask from index', draggedFromIndex);
        });

        item.addEventListener('dragend', (e) => {
            item.style.opacity = '1';
            item.style.cursor = 'grab';
            draggedItem = null;
            draggedFromIndex = null;
        });

        item.addEventListener('dragover', (e) => {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';

            // Visual feedback: highlight drop target
            if (item !== draggedItem) {
                item.style.borderTop = '2px solid #0066cc';
            }
        });

        item.addEventListener('dragleave', (e) => {
            item.style.borderTop = '';
        });

        item.addEventListener('drop', async (e) => {
            e.preventDefault();
            item.style.borderTop = '';

            if (!draggedItem || draggedItem === item) return;

            const currentIndex = Array.from(container.querySelectorAll('.task-item')).indexOf(item);
            const draggedDone = draggedItem.classList.contains('completed');
            const targetDone = item.classList.contains('completed');
            const incompleteBoundary = getIncompleteBoundary();

            // Validate constraints: can't drag incomplete below completed or vice versa
            if (!draggedDone && currentIndex >= incompleteBoundary) {
                console.warn('[tasks] Cannot drag incomplete task below completed tasks');
                alert('Cannot drag incomplete tasks below completed tasks');
                return;
            }
            if (draggedDone && currentIndex < incompleteBoundary) {
                console.warn('[tasks] Cannot drag completed task above incomplete tasks');
                alert('Cannot drag completed tasks above incomplete tasks');
                return;
            }

            // Reorder in DOM
            if (draggedFromIndex < currentIndex) {
                item.parentNode.insertBefore(draggedItem, item.nextSibling);
            } else {
                item.parentNode.insertBefore(draggedItem, item);
            }

            // Get task ID and call API
            const taskId = parseInt(draggedItem.dataset.taskId);
            const newIndex = Array.from(container.querySelectorAll('.task-item')).indexOf(draggedItem);

            console.log('[tasks] Reordering subtask', taskId, 'to position', newIndex);

            try {
                const response = await fetch('/api/tabs/tasks/action/reorder-subtask', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({
                        taskId,
                        newPosition: newIndex,
                        parentUuid
                    })
                });

                if (!response.ok) throw new Error(`HTTP ${response.status}`);
                const result = await response.json();

                if (result.status === 'error' || result.error) {
                    console.error('[tasks] Reorder failed:', result.error || 'Unknown error');
                    alert('Reorder failed: ' + (result.error || 'Unknown error'));
                    // Reload subtasks to restore proper order
                    loadTasks();
                    return;
                }

                console.log('[tasks] Reorder successful');
                updateDivider();
            } catch (err) {
                console.error('[tasks] Reorder error:', err);
                alert('Failed to reorder: ' + err.message);
                // Reload subtasks to restore proper order
                loadTasks();
            }
        });
    });

    // Add initial visual divider
    updateDivider();
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

        if (result.status === 'error' || result.error) {
            const errorMsg = result.error || 'Error toggling task';
            console.error('Error toggling task:', errorMsg);
            alert(errorMsg);
            return;
        }

        // Update task state locally
        const taskIndex = state.tasks.findIndex(t => t.id === taskID);
        if (taskIndex === -1) {
            console.warn('[tasks] Task not found in state:', taskID);
            await loadTasks();
            return;
        }

        const task = state.tasks[taskIndex];
        const wasComplete = task.done;
        task.done = !task.done;

        // If this task has incomplete subtasks and was marked complete, revert it
        if (task.done && task.hasSubtasks) {
            const incompleteSubtasks = state.tasks.filter(t => t.subtask_parent_uuid === task.uuid && !t.done);
            if (incompleteSubtasks.length > 0) {
                // Revert the local change and show error
                task.done = wasComplete;
                alert('Cannot mark parent task complete until all subtasks are complete');
                return;
            }
        }

        // Reorganize tasks list sections when completion status changes
        reorganizeTaskSections();

    } catch (err) {
        console.error('Failed to toggle task:', err);
        alert('Failed to toggle task: ' + err.message);
        // Reload on network errors
        await loadTasks();
    }
}

function reorganizeTaskSections() {
    // Reorganize tasks into their proper sections based on done status
    const tasksToShow = state.tasks.filter(t => !t.subtask_parent_uuid);

    if (state.currentView === 'recent') {
        tasksToShow.sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));
        elements.tasksList.innerHTML = tasksToShow.map(task => createTaskElement(task)).join('');
    } else {
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
                const taskId = parseInt(item.dataset.taskId);
                await toggleTask(taskId);
            });
        }

        // Subtasks expand/collapse
        const subtaskToggle = item.querySelector('.subtask-toggle');
        if (subtaskToggle) {
            subtaskToggle.addEventListener('click', async (e) => {
                e.stopPropagation();
                const parentUuid = item.dataset.taskUuid;
                const isExpanded = state.expandedParents.has(parentUuid);

                if (isExpanded) {
                    state.expandedParents.delete(parentUuid);
                    const subtasksContainer = item.querySelector('.subtasks-container');
                    if (subtasksContainer) {
                        subtasksContainer.remove();
                    }
                    subtaskToggle.classList.remove('expanded');
                } else {
                    state.expandedParents.add(parentUuid);
                    subtaskToggle.classList.add('expanded');
                    await loadSubtasksForParent(item, parentUuid);
                }
            });
        }

        // Edit task on double click
        item.addEventListener('dblclick', (e) => {
            if (e.target.closest('.task-checkbox') || e.target.closest('.subtask-toggle')) return;
            const taskId = parseInt(item.dataset.taskId);
            const task = state.tasks.find(t => t.id === taskId);
            if (task) {
                openTaskEditor(task);
            }
        });
    });
}

export function onMessage(msg) {
    // Handle SSE messages for task updates
    if (!msg || !msg.channelName) return;

    // Listen for task-related messages
    if (msg.channelName !== 'tasks' && msg.serviceType !== 'tasks') return;

    console.log('[tasks] SSE message received:', msg);

    // Parse task details from Body if it's JSON
    let taskDetails = {};
    if (msg.body) {
        try {
            taskDetails = typeof msg.body === 'string' ? JSON.parse(msg.body) : msg.body;
        } catch (e) {
            console.warn('[tasks] Failed to parse message body:', msg.body);
        }
    }

    // Merge message details with task data
    const taskMsg = {...msg, ...taskDetails};

    // Message format: { event: 'taskUpdated', taskId: X, done: true/false, ... }
    if (msg.event === 'taskUpdated' || msg.event === 'task-updated') {
        handleTaskUpdate(taskMsg);
    } else if (msg.event === 'taskCreated' || msg.event === 'task-created') {
        // Reload on new task creation to get it in the right position
        loadTasks();
    } else if (msg.event === 'taskDeleted' || msg.event === 'task-deleted') {
        // Reload on task deletion
        loadTasks();
    }
}

function handleTaskUpdate(msg) {
    if (!msg.taskId) return;

    const taskIndex = state.tasks.findIndex(t => t.id === msg.taskId);
    if (taskIndex === -1) {
        console.warn('[tasks] Task not found in state:', msg.taskId);
        loadTasks();
        return;
    }

    const task = state.tasks[taskIndex];
    const oldDone = task.done;

    // Update task state
    if (msg.done !== undefined) task.done = msg.done;
    if (msg.title !== undefined) task.title = msg.title;
    if (msg.tags !== undefined) task.tags = msg.tags;
    if (msg.color !== undefined) task.color = msg.color;
    if (msg.scheduledAt !== undefined) task.scheduledAt = msg.scheduledAt;

    // Find and update the DOM element
    const taskEl = document.querySelector(`.task-item[data-task-id="${msg.taskId}"]`);
    if (taskEl) {
        if (task.done) {
            taskEl.classList.add('completed');
        } else {
            taskEl.classList.remove('completed');
        }

        // Update checkbox visual state
        const checkbox = taskEl.querySelector('.task-checkbox');
        if (checkbox) {
            if (task.done) {
                checkbox.classList.add('checked');
            } else {
                checkbox.classList.remove('checked');
            }
        }

        // Update title if changed
        if (msg.title !== undefined) {
            const titleEl = taskEl.querySelector('.task-title');
            if (titleEl) {
                titleEl.textContent = task.title;
            }
        }

        console.log('[tasks] Updated task via SSE:', msg.taskId, 'done:', task.done);

        // If done status changed, reorder within subtasks container if applicable
        if (oldDone !== task.done && task.subtask_parent_uuid) {
            reorderSubtasksInContainer(taskEl);
        }
    }
}

function reorderSubtasksInContainer(taskEl) {
    // Find the subtasks container
    const container = taskEl.closest('.subtasks-container');
    if (!container) return;

    // Find all subtask items in this container
    const subtaskItems = Array.from(container.querySelectorAll('.task-item'));
    if (subtaskItems.length === 0) return;

    // Get all tasks for this subtask group to access completion times
    const parentUuid = taskEl.closest('.task-item[data-task-uuid]')?.dataset.taskUuid;

    // Sort: incomplete first (done=0), then completed (done=1)
    subtaskItems.sort((a, b) => {
        const aTaskId = parseInt(a.dataset.taskId);
        const bTaskId = parseInt(b.dataset.taskId);

        const aTask = state.tasks.find(t => t.id === aTaskId);
        const bTask = state.tasks.find(t => t.id === bTaskId);

        if (!aTask || !bTask) return 0;

        // Sort by done status (incomplete first)
        if (aTask.done !== bTask.done) {
            return (aTask.done ? 1 : 0) - (bTask.done ? 1 : 0);
        }

        // For incomplete tasks, sort by scheduled_date ASC (earliest first)
        if (!aTask.done && !bTask.done) {
            const aDate = aTask.scheduledAt ? new Date(aTask.scheduledAt).getTime() : 0;
            const bDate = bTask.scheduledAt ? new Date(bTask.scheduledAt).getTime() : 0;
            return aDate - bDate;
        }

        // For completed tasks, sort by completed_at DESC (most recent first)
        if (aTask.done && bTask.done) {
            const aCompleted = aTask.completedAt ? new Date(aTask.completedAt).getTime() : 0;
            const bCompleted = bTask.completedAt ? new Date(bTask.completedAt).getTime() : 0;
            return bCompleted - aCompleted; // DESC order
        }

        return 0;
    });

    // Rebuild container with sorted items
    subtaskItems.forEach(item => {
        container.appendChild(item);
    });

    console.log('[tasks] Reordered subtasks in container');
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
        const response = await fetch('/api/tabs/tasks/action/create-task', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                title,
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

async function deleteTask(taskID) {
    if (!confirm('Delete this task?')) {
        return;
    }

    try {
        const response = await fetch('/api/tabs/tasks/action/delete-task', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({taskId: taskID})
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();

        if (result.error) {
            console.error('[tasks] Error deleting task:', result.error);
            alert('Error deleting task: ' + result.error);
        } else {
            // Reload tasks to remove it
            await loadTasks();
        }
    } catch (err) {
        console.error('[tasks] Exception deleting task:', err);
        alert('Failed to delete task: ' + err.message);
    }
}

async function createNewSubtask(parentUUID) {
    const title = prompt('Enter subtask title:');
    if (!title || title.trim() === '') {
        return;
    }

    try {
        const response = await fetch('/api/tabs/tasks/action/create-subtask', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({
                parentUuid: parentUUID,
                title: title.trim()
            })
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();

        if (result.error) {
            console.error('[tasks] Error creating subtask:', result.error);
            alert('Error creating subtask: ' + result.error);
        } else {
            // Find the parent task item and reload its subtasks
            const parentTaskEl = document.querySelector(`.task-item[data-task-uuid="${parentUUID}"]`);
            if (parentTaskEl) {
                // Clear subtasks container and reload
                const container = parentTaskEl.nextElementSibling;
                if (container && container.classList.contains('subtasks-container')) {
                    container.remove();
                }
                // Reload subtasks
                await loadSubtasksForParent(parentUUID, parentTaskEl);
            }
        }
    } catch (err) {
        console.error('[tasks] Exception creating subtask:', err);
        alert('Failed to create subtask: ' + err.message);
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
