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
    expandedParents: new Set(), // Track which parent task UUIDs are expanded
    tagsViewFilter: 'all', // Track the selected tag filter in tags view
    inProgress: {} // Map of taskId -> { startedAt: number|null, accumulatedMs: number, running: boolean }
};

let navController = null;
const refreshingParentSubtasks = new Set();

// Formatting helpers for in-progress timers
function formatDurationMs(ms) {
    ms = ms || 0;
    const totalSeconds = Math.floor(ms / 1000);
    const hours = Math.floor(totalSeconds / 3600);
    const minutes = Math.floor((totalSeconds % 3600) / 60);
    const seconds = totalSeconds % 60;
    if (hours > 0) return `${hours}:${String(minutes).padStart(2, '0')}:${String(seconds).padStart(2, '0')}`;
    return `${minutes}:${String(seconds).padStart(2, '0')}`;
}

function loadInProgressState() {
    try {
        const raw = localStorage.getItem('tasks-inprogress');
        if (raw) {
            state.inProgress = JSON.parse(raw) || {};
            // Normalize numeric keys to strings
            const normalized = {};
            Object.keys(state.inProgress).forEach(k => {
                normalized[String(k)] = state.inProgress[k];
            });
            state.inProgress = normalized;
        }
    } catch (err) {
        console.error('[tasks] Failed to load in-progress state:', err);
    }
}

function saveInProgressState() {
    try {
        localStorage.setItem('tasks-inprogress', JSON.stringify(state.inProgress || {}));
    } catch (err) {
        console.error('[tasks] Failed to save in-progress state:', err);
    }
}

function anyRunningInProgress() {
    return Object.values(state.inProgress || {}).some(v => v && v.running);
}

let inProgressTimerId = null;
function ensureInProgressTimer() {
    if (inProgressTimerId) return;
    if (!anyRunningInProgress()) return;
    inProgressTimerId = setInterval(updateAllInProgressDisplays, 1000);
    updateAllInProgressDisplays();
}

function stopInProgressTimer() {
    if (inProgressTimerId) {
        clearInterval(inProgressTimerId);
        inProgressTimerId = null;
    }
}

function updateAllInProgressDisplays() {
    const els = document.querySelectorAll('.task-inprogress-time');
    els.forEach(el => {
        const id = el.dataset.taskId;
        const info = state.inProgress && state.inProgress[id];
        if (!info) { el.textContent = ''; return; }
        const accum = info.accumulatedMs || 0;
        const extra = (info.running && info.startedAt) ? (Date.now() - info.startedAt) : 0;
        el.textContent = formatDurationMs(accum + extra);
    });
    // Update icons
    const btns = document.querySelectorAll('.task-inprogress-btn');
    btns.forEach(b => {
        const id = b.dataset.taskId;
        const info = state.inProgress && state.inProgress[id];
        b.textContent = (info && info.running) ? '⏸' : '▶';
    });
    if (!anyRunningInProgress()) stopInProgressTimer();
}

async function toggleInProgress(taskId) {
	const id = String(taskId);
	const cur = state.inProgress && state.inProgress[id];
	const start = !(cur && cur.running);
	try {
		const response = await fetch('/api/tabs/tasks/action/set-in-progress', {
			method: 'POST',
			headers: {'Content-Type': 'application/json'},
			body: JSON.stringify({ taskId: parseInt(taskId), start })
		});
		if (!response.ok) throw new Error(`HTTP ${response.status}`);
		const res = await response.json();
		if (res.error) throw new Error(res.error);
		if (start) {
			state.inProgress[id] = state.inProgress[id] || {startedAt: null, accumulatedMs: 0, running: false};
			state.inProgress[id].startedAt = Date.parse(res.inProgressStartedAt);
			state.inProgress[id].running = true;
		} else {
			state.inProgress[id] = state.inProgress[id] || {startedAt: null, accumulatedMs: 0, running: false};
			state.inProgress[id].accumulatedMs = (res.inProgressTotalSeconds || 0) * 1000;
			state.inProgress[id].startedAt = null;
			state.inProgress[id].running = false;
		}
		saveInProgressState();
		ensureInProgressTimer();
        if (state.currentView === 'tags') {
            displayTagsView();
        } else {
            renderTasksView();
        }
	} catch (err) {
		console.error('[tasks] toggleInProgress error:', err);
	}
}

async function stopInProgress(taskId) {
	const id = String(taskId);
	try {
		const response = await fetch('/api/tabs/tasks/action/set-in-progress', {
			method: 'POST',
			headers: {'Content-Type': 'application/json'},
			body: JSON.stringify({ taskId: parseInt(taskId), start: false })
		});
		if (!response.ok) throw new Error(`HTTP ${response.status}`);
		const res = await response.json();
		if (res.error) throw new Error(res.error);
		state.inProgress[id] = state.inProgress[id] || {startedAt: null, accumulatedMs: 0, running: false};
		state.inProgress[id].accumulatedMs = (res.inProgressTotalSeconds || 0) * 1000;
		state.inProgress[id].startedAt = null;
		state.inProgress[id].running = false;
		saveInProgressState();
		try { updateAllInProgressDisplays(); } catch (err) { console.error('[tasks] updateAllInProgressDisplays error:', err); }
        if (state.currentView === 'tags') {
            displayTagsView();
        } else {
            renderTasksView();
        }
	} catch (err) {
		console.error('[tasks] stopInProgress error:', err);
	}
}

function clearLocalInProgress(taskId, totalSeconds) {
    const id = String(taskId);
    if (!state.inProgress) state.inProgress = {};
    const prev = state.inProgress[id] || {startedAt: null, accumulatedMs: 0, running: false};
    if (typeof totalSeconds === 'number' && Number.isFinite(totalSeconds)) {
        prev.accumulatedMs = totalSeconds * 1000;
    }
    prev.startedAt = null;
    prev.running = false;
    state.inProgress[id] = prev;
    saveInProgressState();
    try {
        updateAllInProgressDisplays();
    } catch (err) {
        console.error('[tasks] updateAllInProgressDisplays error:', err);
    }
}

async function loadTasks() {
    try {
        // If tags view is selected, load tags view instead
        if (state.currentView === 'tags') {

            await loadTagsView();
            return;
        }


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

	// Seed in-progress state from server-provided fields (if present)
	if (!state.inProgress) state.inProgress = {};
	if (Array.isArray(state.tasks)) {
		for (const t of state.tasks) {
			if (t.inProgress || t.inProgressStartedAt || t.inProgressTotalSeconds) {
				const id = String(t.id);
				const prev = state.inProgress[id] || { startedAt: null, accumulatedMs: 0, running: false };
				prev.accumulatedMs = (t.inProgressTotalSeconds || 0) * 1000;
				prev.startedAt = t.inProgressStartedAt ? Date.parse(t.inProgressStartedAt) : null;
				prev.running = !!t.inProgress;
				state.inProgress[id] = prev;
			}
		}
		saveInProgressState();
	}

        // Update tag filter list (hide 'all' filter in tags view)
        updateTagList();

        // Display tasks
        renderTasksView();

        // Setup navigation after DOM is ready
        if (!navController) {
            setupNavigation();
        }
    } catch (err) {
        console.error('[tasks] Exception in loadTasks:', err);
        elements.tasksList.innerHTML = `<div class="error">Failed to load tasks: ${escapeHtml(err.message)}</div>`;
    }
}

async function loadTagsView() {
    try {

        
        // Always fetch with empty tag filter to get ALL tasks for the tags view
        // Only filter by tag if the user has explicitly selected a tag
        let tagFilter = '';
        if (state.currentFilter && state.currentFilter !== 'all') {
            tagFilter = state.currentFilter;
        }
        

        
        const response = await fetch('/api/tabs/tasks/action/fetch-tags-view', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({tag: tagFilter})
        });

        if (!response.ok) throw new Error(`HTTP ${response.status}`);
        const result = await response.json();
        


        if (result.error) {
            console.error('[tasks] Error loading tags view:', result.error);
            elements.tasksList.innerHTML = `<div class="error">Error: ${escapeHtml(result.error)}</div>`;
            return;
        }

        state.tagsViewTasks = result.tasks || [];

        
        displayTagsView();
        
        // Update tag list only when showing all tasks (no filter)
        // When filtering by tag, keep the tag list showing all tags
        if (!tagFilter) {
            state.tagsViewAllTasks = state.tagsViewTasks;
        }

        // Update the tag list so counts reflect the tags view (use cached all-tasks if available)
        updateTagList();
        
        // Highlight the selected tag
        const tagItems = document.querySelectorAll('.tag-item');
        tagItems.forEach(item => {
            item.classList.remove('active');
            if (item.dataset.tag === state.currentFilter) {
                item.classList.add('active');
            }
        });
        
        setupTagsDragAndDrop();

        // Setup navigation after DOM is ready
        if (!navController) {
            setupNavigation();
        }
        
        // Restore expanded parent tasks
        restoreExpandedTagsViewParents();
    } catch (err) {
        console.error('[tasks] Exception in loadTagsView:', err);
        elements.tasksList.innerHTML = `<div class="error">Failed to load tags view: ${escapeHtml(err.message)}</div>`;
    }
}

function restoreExpandedTagsViewParents() {
    // Re-expand parent tasks that were previously expanded
    if (state.expandedParents.size === 0) return;
    
    const container = document.querySelector('.tags-view-container');
    if (!container) return;
    
    state.expandedParents.forEach(parentUuid => {
        // Find the parent task element by UUID in tags view
        const parentEl = container.querySelector(`.task-item[data-task-uuid="${parentUuid}"]`);
        if (parentEl) {

            loadSubtasksForParent(parentUuid, parentEl);
        }
    });
}

function displayTagsView() {
    if (!state.tagsViewTasks || state.tagsViewTasks.length === 0) {
        elements.tasksList.innerHTML = '<div class="no-tasks">No parent tasks</div>';
        return;
    }

    // Separate incomplete and completed tasks
    const incomplete = state.tagsViewTasks.filter(t => !t.done);
    const completed = state.tagsViewTasks.filter(t => t.done);
    
    // Sort completed tasks by completion time (most recent first)
    completed.sort((a, b) => new Date(b.completedAt) - new Date(a.completedAt));

    let html = '<div class="tags-view-container">';
    
    // Incomplete section
    if (incomplete.length > 0) {
        html += '<div class="tags-incomplete-section">';
        html += incomplete.map(task => createParentTaskElement(task)).join('');
        html += '</div>';
    }

    // Completed section
    if (completed.length > 0) {
        html += '<div class="tags-completed-section">';
        html += '<div class="tags-completed-section-header">Completed</div>';
        html += completed.map(task => createParentTaskElement(task)).join('');
        html += '</div>';
    }

    html += '</div>';
    elements.tasksList.innerHTML = html;
    
    // Enhance parent tasks with subtask toggle
    enhanceTagsViewTasksForSubtasks();
    
    // Setup event handlers for parent tasks in tags view
    setupTagsViewEventHandlers();
    
    // Also attach standard listeners to parent tasks for better compatibility
    const container = document.querySelector('.tags-view-container');
    if (container) {
        attachTaskItemListenersForTagsView(container);
    }
}

function enhanceTagsViewTasksForSubtasks() {
    const container = document.querySelector('.tags-view-container');
    if (!container) return;
    
    // For each parent task that has subtasks, add a toggle
    for (const task of state.tagsViewTasks) {
        if (!task.hasSubtasks || !task.uuid) continue;
        
        const el = container.querySelector(`.task-item[data-task-id="${task.id}"]`);
        if (!el) continue;
        
        // If a real toggle already exists, skip
        if (el.querySelector('.subtask-toggle')) continue;
        
        // Create toggle span
        const checkbox = el.querySelector('.task-checkbox');
        const toggle = document.createElement('span');
        toggle.className = 'subtask-toggle';
        toggle.dataset.uuid = task.uuid;
        toggle.textContent = '▶';
        toggle.style.cursor = 'pointer';
        toggle.onclick = (e) => {
            e.stopPropagation();
            loadSubtasksForParent(task.uuid, el);
        };
        
        // If there's a placeholder, replace it; otherwise insert before checkbox
        const placeholder = el.querySelector('.subtask-toggle-placeholder');
        if (placeholder && placeholder.parentNode) {
            placeholder.parentNode.replaceChild(toggle, placeholder);
        } else if (checkbox && checkbox.parentNode) {
            checkbox.parentNode.insertBefore(toggle, checkbox);
        } else {
            el.insertBefore(toggle, el.firstChild);
        }
    }
}

function setupTagsViewEventHandlers() {
    const container = document.querySelector('.tags-view-container');
    if (!container) return;
    
    // Handle delete button
    container.querySelectorAll('.task-delete').forEach(btn => {
        btn.addEventListener('click', async (e) => {
            e.preventDefault();
            e.stopPropagation();
            
            const taskId = parseInt(btn.dataset.taskId);
            if (!taskId || !confirm('Are you sure you want to delete this task?')) return;
            
            try {
                const response = await fetch('/api/tabs/tasks/action/delete-task', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({taskId})
                });
                
                if (!response.ok) throw new Error(`HTTP ${response.status}`);
                const result = await response.json();
                
                if (result.error) {
                    alert(`Delete failed: ${result.error}`);
                } else {
                    await loadTagsView();
                }
            } catch (err) {
                console.error('Delete error:', err);
                alert('Failed to delete task');
            }
        });
    });
    
    // Handle edit button
    container.querySelectorAll('.task-edit-btn').forEach(btn => {
        btn.addEventListener('click', async (e) => {
            e.preventDefault();
            e.stopPropagation();
            
            const taskId = parseInt(btn.dataset.taskId);
            const task = state.tagsViewTasks.find(t => t.id === taskId);
            if (!task) return;
            
            // Load task editor with this task
            editTask(task);
        });
    });
    
    // Handle checkbox for marking tasks done/incomplete
    container.querySelectorAll('.task-checkbox').forEach(checkbox => {
        checkbox.addEventListener('click', async (e) => {
            e.preventDefault();
            e.stopPropagation();
            
            const taskId = parseInt(checkbox.dataset.taskId);
            const task = state.tagsViewTasks.find(t => t.id === taskId);
            if (!task) return;
            
            // Toggle done status
            try {
                const newDone = !task.done;
                const response = await fetch('/api/tabs/tasks/action/toggle-task', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({taskId, done: newDone})
                });
                
                if (!response.ok) throw new Error(`HTTP ${response.status}`);
                const result = await response.json();
                
                if (result.error) {
                    console.error('Toggle error:', result.error);
                } else {
                    if (newDone) clearLocalInProgress(taskId);
                    await loadTagsView();
                }
            } catch (err) {
                console.error('Toggle error:', err);
            }
        });
    });
}

function createParentTaskElement(task) {
    const checkboxClass = task.done ? 'task-checkbox checked' : 'task-checkbox';
    const taskClass = task.done ? 'task-item completed' : 'task-item';
    const uuid = task.uuid || '';
    const color = task.color || '#808080';
    const colorStyle = task.color ? ` style="border-left: 4px solid ${color}; padding-left: 8px;"` : '';
    
    let recurring = '';
    if (task.recurring || task.pattern) {
        recurring = `<span class="task-recurring" title="This task recurs">↻</span>`;
    }

    // For tags view, just show the date if scheduled
    let timeDisplay = '';
    if (!task.done && task.scheduledAt) {
        const datePart = task.scheduledAt.split('T')[0];
        timeDisplay = `<div class="task-time">${datePart}</div>`;
    }
    
    // Get task tags for display
    const taskTags = task.tags ? task.tags.split(',').map(t => t.trim()).filter(t => t) : [];
    const tagsHtml = task.tags ? `<div class="task-tags">${task.tags.split(',').map(t => `<span class="tag-badge">${escapeHtml(t.trim())}</span>`).join('')}</div>` : '';

    // Add subtask button for top-level tasks (those without a parentUuid)
    const addSubtaskBtn = (task.uuid && !task.parentUuid) ? `<button class="task-add-subtask" title="Add subtask" data-task-uuid="${task.uuid}">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="12" y1="5" x2="12" y2="19"></line>
                    <line x1="5" y1="12" x2="19" y2="12"></line>
                </svg>
            </button>` : '';

    return `<div class="${taskClass} draggable-parent" data-task-id="${task.id}" data-task-uuid="${uuid}"${colorStyle} draggable="true">
            <span class="subtask-toggle-placeholder"></span>
            <div class="${checkboxClass}" title="Toggle task completion" data-task-id="${task.id}"></div>
            <div class="task-content">
                <div class="task-title-row">
                    <div class="task-title">${escapeHtml(task.title)}</div>
                    ${tagsHtml}
                </div>
                ${timeDisplay}
            </div>
            ${recurring}
            <div class="task-status">${task.done ? '✓' : ''}</div>
            <div class="task-actions">
                ${addSubtaskBtn}
                <button class="task-inprogress-btn" data-task-id="${task.id}" title="Toggle in progress">${state.inProgress[task.id] && state.inProgress[task.id].running ? '⏸' : '▶'}</button>
                <div class="task-inprogress-time" data-task-id="${task.id}">${state.inProgress && state.inProgress[task.id] ? formatDurationMs(state.inProgress[task.id].accumulatedMs + (state.inProgress[task.id].running ? (Date.now() - state.inProgress[task.id].startedAt) : 0)) : ''}</div>
                <button class="task-edit-btn" data-task-id="${task.id}" title="Edit">✎</button>
                <button class="task-delete" title="Delete task" data-task-id="${task.id}">
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                        <polyline points="3 6 5 6 21 6"></polyline>
                        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                        <line x1="10" y1="11" x2="10" y2="17"></line>
                        <line x1="14" y1="11" x2="14" y2="17"></line>
                    </svg>
                </button>
            </div>
        </div>`;
}

function setupTagsDragAndDrop() {
    const container = document.querySelector('.tags-view-container');
    if (!container) return;

    const incompleteSection = container.querySelector('.tags-incomplete-section');
    const completedSection = container.querySelector('.tags-completed-section');

    // Use arrays for faster iteration and to avoid repeated querySelectorAll calls during drag
    const itemNodes = Array.from(container.querySelectorAll('.task-item.draggable-parent'));
    let draggedItem = null;
    let rafId = null;
    let lastDragEvent = null;
    let cachedIncItems = [];
    let cachedCompItems = [];
    let currentHighlight = null;

    function clearHighlight() {
        if (currentHighlight) {
            currentHighlight.classList.remove('drag-over-top', 'drag-over-bottom');
            currentHighlight = null;
        }
    }

    itemNodes.forEach(item => {
        item.addEventListener('dragstart', (e) => {
            draggedItem = item;
            item.classList.add('dragging');
            // Cache lists for faster hit-testing during drag
            cachedIncItems = incompleteSection ? Array.from(incompleteSection.querySelectorAll('.task-item.draggable-parent')) : [];
            cachedCompItems = completedSection ? Array.from(completedSection.querySelectorAll('.task-item.draggable-parent')) : [];
            lastDragEvent = null;
            if (rafId) { cancelAnimationFrame(rafId); rafId = null; }
            e.dataTransfer.effectAllowed = 'move';
        });

        item.addEventListener('dragend', (e) => {
            item.classList.remove('dragging');
            clearHighlight();
            if (rafId) { cancelAnimationFrame(rafId); rafId = null; }
            cachedIncItems = [];
            cachedCompItems = [];
            draggedItem = null;
        });

        item.addEventListener('drop', async (e) => {
            e.preventDefault();
            if (!draggedItem || draggedItem === item) return;

            const draggedInIncomplete = cachedIncItems && cachedIncItems.includes(draggedItem);
            const targetInIncomplete = cachedIncItems && cachedIncItems.includes(item);
            if (draggedInIncomplete !== targetInIncomplete) {
                alert('Cannot move incomplete tasks below completed tasks or vice versa');
                clearHighlight();
                return;
            }

            const rect = item.getBoundingClientRect();
            const insertBefore = (e.clientY - rect.top) < (rect.height / 2);

            // Move parent task and any associated subtasks container together in the DOM
            const subtasksContainer = draggedItem.nextElementSibling;
            const hasSubtasksContainer = subtasksContainer && subtasksContainer.classList.contains('subtasks-container');

            if (insertBefore) {
                item.parentNode.insertBefore(draggedItem, item);
            } else {
                item.parentNode.insertBefore(draggedItem, item.nextSibling);
            }
            if (hasSubtasksContainer) {
                draggedItem.parentNode.insertBefore(subtasksContainer, draggedItem.nextSibling);
            }

            clearHighlight();

            // Recompute the new position after DOM update
            const newItemsArray = Array.from(container.querySelectorAll('.task-item.draggable-parent'));
            const newPosition = newItemsArray.indexOf(draggedItem);

            try {
                const response = await fetch('/api/tabs/tasks/action/reorder-parent', {
                    method: 'POST',
                    headers: {'Content-Type': 'application/json'},
                    body: JSON.stringify({
                        taskId: parseInt(draggedItem.dataset.taskId),
                        newPosition: newPosition
                    })
                });

                if (!response.ok) throw new Error(`HTTP ${response.status}`);
                const result = await response.json();

                if (result.error) {
                    alert(`Reorder failed: ${result.error}`);
                    await loadTagsView();
                }
            } catch (err) {
                console.error('Reorder error:', err);
                alert('Failed to reorder tasks');
                await loadTagsView();
            }
        });
    });

    // Container-level dragover/drop with requestAnimationFrame throttling for responsiveness
    container.addEventListener('dragover', (e) => {
        e.preventDefault();
        if (!draggedItem) return;
        lastDragEvent = e;
        if (rafId) return; // already scheduled

        rafId = requestAnimationFrame(() => {
            rafId = null;
            const ev = lastDragEvent;
            lastDragEvent = null;
            if (!ev) return;

            const el = document.elementFromPoint(ev.clientX, ev.clientY);
            if (!el) {
                clearHighlight();
                return;
            }

            let resolvedTarget = el.closest('.task-item.draggable-parent');
            let insertBefore = true;

            const overIncomplete = incompleteSection && incompleteSection.contains(el);
            const overCompleted = completedSection && completedSection.contains(el);

            if (!resolvedTarget) {
                if (overIncomplete) {
                    if (!cachedIncItems || cachedIncItems.length === 0) { clearHighlight(); return; }
                    resolvedTarget = cachedIncItems[cachedIncItems.length - 1];
                    insertBefore = false;
                } else if (overCompleted) {
                    if (!cachedCompItems || cachedCompItems.length === 0) { clearHighlight(); return; }
                    resolvedTarget = cachedCompItems[cachedCompItems.length - 1];
                    insertBefore = false;
                } else {
                    clearHighlight();
                    return;
                }
            } else {
                if (resolvedTarget === draggedItem) { clearHighlight(); return; }
                const rect = resolvedTarget.getBoundingClientRect();
                insertBefore = (ev.clientY - rect.top) < (rect.height / 2);
            }

            // Prevent cross-section dragging
            const draggedInIncomplete = cachedIncItems && cachedIncItems.includes(draggedItem);
            const targetInIncomplete = cachedIncItems && cachedIncItems.includes(resolvedTarget);
            if (draggedInIncomplete !== targetInIncomplete) { clearHighlight(); return; }

            if (currentHighlight && currentHighlight !== resolvedTarget) {
                currentHighlight.classList.remove('drag-over-top', 'drag-over-bottom');
            }

            // Always remove any existing positional classes on the resolved target before adding the desired one
            resolvedTarget.classList.remove('drag-over-top', 'drag-over-bottom');
            currentHighlight = resolvedTarget;
            currentHighlight.classList.add(insertBefore ? 'drag-over-top' : 'drag-over-bottom');
        });
    });

    container.addEventListener('drop', async (e) => {
        e.preventDefault();
        if (!draggedItem) return;
        const el = document.elementFromPoint(e.clientX, e.clientY);
        const itemAtPoint = el ? el.closest('.task-item.draggable-parent') : null;

        // If an item handler will manage the drop (we dropped over an item), let it handle it
        if (itemAtPoint) {
            clearHighlight();
            return;
        }

        if (currentHighlight) {
            const insertBefore = currentHighlight.classList.contains('drag-over-top');
            if (insertBefore) {
                currentHighlight.parentNode.insertBefore(draggedItem, currentHighlight);
            } else {
                currentHighlight.parentNode.insertBefore(draggedItem, currentHighlight.nextSibling);
            }
        } else {
            // Append to appropriate section under cursor using cached lists
            const overIncomplete = incompleteSection && incompleteSection.contains(el);
            const overCompleted = completedSection && completedSection.contains(el);
            if (overIncomplete) {
                const itemsInInc = cachedIncItems && cachedIncItems.length ? cachedIncItems : Array.from(incompleteSection.querySelectorAll('.task-item.draggable-parent'));
                if (itemsInInc.length > 0) {
                    const last = itemsInInc[itemsInInc.length - 1];
                    last.parentNode.insertBefore(draggedItem, last.nextSibling);
                } else {
                    incompleteSection.appendChild(draggedItem);
                }
            } else if (overCompleted) {
                const itemsInComp = cachedCompItems && cachedCompItems.length ? cachedCompItems : Array.from(completedSection.querySelectorAll('.task-item.draggable-parent'));
                if (itemsInComp.length > 0) {
                    const last = itemsInComp[itemsInComp.length - 1];
                    last.parentNode.insertBefore(draggedItem, last.nextSibling);
                } else {
                    completedSection.appendChild(draggedItem);
                }
            } else {
                clearHighlight();
                return;
            }
        }

        // Move any subtasks container that followed the dragged item
        const subtasksContainer = draggedItem.nextElementSibling;
        if (subtasksContainer && subtasksContainer.classList.contains('subtasks-container')) {
            draggedItem.parentNode.insertBefore(subtasksContainer, draggedItem.nextSibling);
        }

        clearHighlight();
        if (rafId) { cancelAnimationFrame(rafId); rafId = null; }
        cachedIncItems = [];
        cachedCompItems = [];

        // Recompute the new position after DOM update
        const newItemsArray = Array.from(container.querySelectorAll('.task-item.draggable-parent'));
        const newPosition = newItemsArray.indexOf(draggedItem);

        try {
            const response = await fetch('/api/tabs/tasks/action/reorder-parent', {
                method: 'POST',
                headers: {'Content-Type': 'application/json'},
                body: JSON.stringify({
                    taskId: parseInt(draggedItem.dataset.taskId),
                    newPosition: newPosition
                })
            });

            if (!response.ok) throw new Error(`HTTP ${response.status}`);
            const result = await response.json();

            if (result.error) {
                alert(`Reorder failed: ${result.error}`);
                await loadTagsView();
            }
        } catch (err) {
            console.error('Reorder error:', err);
            alert('Failed to reorder tasks');
            await loadTagsView();
        }
    });

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

    function isNavigableTaskItem(item) {
        if (!item) return false;
        if (item.style.display === 'none') return false;
        if (item.closest('.task-group-items.collapsed, .subtasks-completed-items.collapsed')) return false;

        let node = item;
        while (node && node !== elements.container) {
            if (!(node instanceof HTMLElement)) break;
            if (node.style.display === 'none') return false;
            node = node.parentElement;
        }

        return true;
    }

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
            if (!isNavigableTaskItem(item)) return false;
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

    const originalHandleItemsKeydown = navController.handleItemsKeydown.bind(navController);
    navController.handleItemsKeydown = function (e) {
        const activeElement = document.activeElement;
        if (activeElement instanceof HTMLElement && activeElement.matches('input, textarea, select, [contenteditable="true"]')) {
            return;
        }

        const visibleItems = this.getVisibleItems();
        const selectedItem = this.selectedIndex >= 0 && this.selectedIndex < visibleItems.length
            ? visibleItems[this.selectedIndex]
            : null;

        if (selectedItem && (e.key === 'ArrowRight' || e.key === 'ArrowLeft')) {
            const parentUuid = selectedItem.dataset.taskUuid;
            const hasToggle = !!selectedItem.querySelector('.subtask-toggle');
            const isExpanded = !!(parentUuid && state.expandedParents.has(parentUuid));

            if (e.key === 'ArrowRight' && parentUuid && hasToggle && !isExpanded) {
                e.preventDefault();
                loadSubtasksForParent(parentUuid, selectedItem);
                return;
            }

            if (e.key === 'ArrowLeft' && parentUuid && hasToggle && isExpanded) {
                e.preventDefault();
                loadSubtasksForParent(parentUuid, selectedItem);
                return;
            }
        }

        originalHandleItemsKeydown(e);
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
    // Hide "all" item when in tags view
    const allItem = elements.tagList.querySelector('[data-tag="all"]');
    if (state.currentView === 'tags') {
        if (allItem) {
            allItem.style.display = 'none';
        }
        // In tags view, show unique tags from parent tasks
        updateTagsViewTagList();
    } else {
        if (allItem) {
            allItem.style.display = '';
            const count = state.tasks.length;
            // Clear and build label + count elements to avoid innerHTML and ensure escaping
            allItem.innerHTML = '';
            const allLabel = document.createElement('span');
            allLabel.className = 'tag-label';
            allLabel.textContent = 'all';
            const allCount = document.createElement('span');
            allCount.className = 'service-count';
            allCount.textContent = count;
            allItem.appendChild(allLabel);
            allItem.appendChild(allCount);
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
            // Build safe label + count nodes
            const label = document.createElement('span');
            label.className = 'tag-label';
            label.textContent = tag;
            const countSpan = document.createElement('span');
            countSpan.className = 'service-count';
            countSpan.textContent = count;
            item.appendChild(label);
            item.appendChild(countSpan);
            elements.tagList.appendChild(item);
        }
    }
}

function updateTagsViewTagList() {
    // Build tag list from parent tasks only. Prefer the cached full tags view task list if available.
    const tagCounts = {};
    const sourceTasks = (state.tagsViewAllTasks && state.tagsViewAllTasks.length) ? state.tagsViewAllTasks : (state.tagsViewTasks || []);

    if (sourceTasks && sourceTasks.length > 0) {
        sourceTasks.forEach(task => {
            const tags = task.tags ? task.tags.split(',').map(t => t.trim()).filter(t => t) : [];
            if (tags.length === 0) {
                tagCounts['untagged'] = (tagCounts['untagged'] || 0) + 1;
            } else {
                tags.forEach(tag => {
                    tagCounts[tag] = (tagCounts[tag] || 0) + 1;
                });
            }
        });
    }

    // Remove existing tag items
    const tagItems = elements.tagList.querySelectorAll('[data-tag]:not([data-tag="all"])');
    tagItems.forEach(item => item.remove());

    // Add tags sorted by count
    const sortedTags = Object.entries(tagCounts)
        .sort((a, b) => b[1] - a[1])
        .map(([tag, count]) => ({tag, count}));

    for (const {tag, count} of sortedTags) {
        const item = document.createElement('div');
        item.className = 'tag-item';
        if (state.currentFilter === tag) {
            item.classList.add('active');
        }
        item.dataset.tag = tag;
        // Build safe label + count nodes
        const label = document.createElement('span');
        label.className = 'tag-label';
        label.textContent = tag;
        const countSpan = document.createElement('span');
        countSpan.className = 'service-count';
        countSpan.textContent = count;
        item.appendChild(label);
        item.appendChild(countSpan);
        elements.tagList.appendChild(item);
    }
}

function isTopLevelTask(task) {
    return !(task && (task.subtask_parent_uuid || task.parentUuid));
}

function getTaskParentUuid(task) {
    return (task && (task.subtask_parent_uuid || task.parentUuid)) || '';
}

function renderTasksView() {
    displayTasks();
    restoreExpandedSubtasks();
}

async function refreshParentSubtasks(parentUuid) {
    if (!parentUuid || refreshingParentSubtasks.has(parentUuid)) return false;
    const parentEl = document.querySelector(`.task-item[data-task-uuid="${parentUuid}"]`);
    if (!parentEl) return false;

    refreshingParentSubtasks.add(parentUuid);
    try {
        const container = parentEl.nextElementSibling;
        if (container && container.classList && container.classList.contains('subtasks-container')) {
            container.remove();
        }
        state.expandedParents.add(parentUuid);
        await loadSubtasksForParent(parentUuid, parentEl);
        return true;
    } finally {
        refreshingParentSubtasks.delete(parentUuid);
    }
}

function displayTasks() {
    if (state.tasks.length === 0) {
        elements.tasksList.innerHTML = '<div class="no-tasks">No tasks scheduled or completed today</div>';
        return;
    }

    const topLevelTasks = (state.tasks || []).filter(isTopLevelTask);

    // Group tasks by tag first
    const groupedByTag = {};

    for (const task of topLevelTasks) {
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
        tasksToShow = topLevelTasks;
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
        // Determine in-progress tasks from local state (only currently running tasks)
        const inProgressIds = new Set(Object.keys(state.inProgress || {}).filter(k => {
            const v = state.inProgress[k];
            return v && v.running;
        }).map(k => String(k)));

        let inProgressTasks = [];
        if (state.currentView === 'today') {
            // Show all in-progress tasks regardless of schedule/tag
            inProgressTasks = topLevelTasks.filter(t => inProgressIds.has(String(t.id)));
        } else {
            inProgressTasks = tasksToShow.filter(t => inProgressIds.has(String(t.id)));
        }

        // Sort in-progress tasks by progress time (least time in progress first)
        inProgressTasks.sort((a, b) => {
            const infoA = state.inProgress[a.id] || {startedAt: null, accumulatedMs: 0};
            const infoB = state.inProgress[b.id] || {startedAt: null, accumulatedMs: 0};

            // Calculate total time in progress for each
            const timeA = infoA.accumulatedMs + (infoA.running && infoA.startedAt ? (Date.now() - infoA.startedAt) : 0);
            const timeB = infoB.accumulatedMs + (infoB.running && infoB.startedAt ? (Date.now() - infoB.startedAt) : 0);

            return timeA - timeB; // Ascending: least time first
        });

        const todayIncomplete = tasksToShow.filter(t => !t.done && (!t.category || t.category === 'today') && !inProgressIds.has(String(t.id)));
        const pastIncomplete = tasksToShow.filter(t => !t.done && t.category === 'past' && !inProgressIds.has(String(t.id)));
        const completed = tasksToShow.filter(t => t.done);

        // Sort incomplete by scheduled date (earliest first)
        todayIncomplete.sort((a, b) => new Date(a.scheduledAt) - new Date(b.scheduledAt));
        pastIncomplete.sort((a, b) => new Date(b.scheduledAt) - new Date(a.scheduledAt));

        // Sort completed by completed date (most recent first)
        completed.sort((a, b) => new Date(b.completedAt) - new Date(a.completedAt));

        // Build HTML with completion status groups and collapsible headers
        let html = '';

        // If we have in-progress tasks and are on the 'today' view, render them at the top
        if (inProgressTasks.length > 0 && state.currentView === 'today') {
            html += `<div class="task-group">
                    <div class="task-group-header">
                        In Progress (${inProgressTasks.length})
                    </div>
                    <div class="task-group-items">
                        ${inProgressTasks.map(task => createTaskElement(task)).join('')}
                    </div>
                </div>`;
        }

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
    const isInProgress = !!(task.inProgress || (state.inProgress && state.inProgress[task.id] && state.inProgress[task.id].running));

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

                if (isPast) {
                    timeDisplay = `<div class="task-time ${timeClass}"><span class="task-time-label">${localTime}</span> <span class="task-time-age">${timeAgo}</span></div>`;
                } else {
                    timeDisplay = `<div class="task-time ${timeClass}"><span class="task-time-label">${localTime}</span> <span class="task-time-age">${timeAgo}</span></div>`;
                }
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
    const tagsDisplay = task.tags ? `<div class="task-tags">${task.tags.split(',').map(t => `<span class="tag-badge">${escapeHtml(t.trim())}</span>`).join('')}</div>` : '';
    const metadataPrimary = timeDisplay || recurring
        ? `<div class="task-metadata-primary">${timeDisplay}${recurring}</div>`
        : '<div class="task-metadata-primary"></div>';
    const metadataDisplay = timeDisplay || recurring || tagsDisplay
        ? `<div class="task-metadata">${metadataPrimary}${tagsDisplay}</div>`
        : '';

    // Add inline style for task color if available
    const colorStyle = task.color ? ` style="border-left: 4px solid ${task.color}; padding-left: 8px;"` : '';

    // Add new subtask button if this is a top-level task with a UUID
    const addSubtaskBtn = (task.uuid && !task.parentUuid) ? `<button class="task-add-subtask" title="Add subtask" data-task-uuid="${task.uuid}">
                <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                    <line x1="12" y1="5" x2="12" y2="19"></line>
                    <line x1="5" y1="12" x2="19" y2="12"></line>
                </svg>
            </button>` : '';
    const inProgressIndicator = isInProgress ? '<span class="task-progress-indicator" title="In progress" aria-label="In progress"></span>' : '';

    return `<div class="${taskClass}${flagClass}" data-task-id="${task.id}" data-task-uuid="${task.uuid || ''}" data-tag="${primaryTag}" data-all-tags="${escapeHtml(taskTags.join(',') || 'untagged')}"${colorStyle}>
            <span class="subtask-toggle-placeholder"></span>
            <div class="${checkboxClass}" title="Toggle task completion" data-task-id="${task.id}"></div>
            <div class="task-content">
                <div class="task-title"><span class="task-title-text">${escapeHtml(task.title)}</span>${inProgressIndicator}</div>
                ${metadataDisplay}
            </div>
            ${priority}
            <div class="task-status">${task.done ? '✓' : ''}</div>
            <div class="task-actions">
                ${addSubtaskBtn}
                <button class="task-inprogress-btn" data-task-id="${task.id}" title="Toggle in progress">${state.inProgress[task.id] && state.inProgress[task.id].running ? '⏸' : '▶'}</button>
                <div class="task-inprogress-time" data-task-id="${task.id}">${state.inProgress && state.inProgress[task.id] ? formatDurationMs(state.inProgress[task.id].accumulatedMs + (state.inProgress[task.id].running ? (Date.now() - state.inProgress[task.id].startedAt) : 0)) : ''}</div>
                <form class="task-command-form" data-task-id="${task.id}"><input class="task-command-input" data-task-id="${task.id}" placeholder="cmd" /><button type="submit" class="task-command-submit" tabindex="-1" title="Run command">⏎</button></form>
                <button class="task-edit-btn" title="Edit task" data-task-id="${task.id}">✎</button>
                <button class="task-delete" title="Delete task" data-task-id="${task.id}">
                    <svg viewBox="0 0 24 24" width="16" height="16" fill="none" stroke="currentColor" stroke-width="2">
                        <polyline points="3 6 5 6 21 6"></polyline>
                        <path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6m3 0V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"></path>
                        <line x1="10" y1="11" x2="10" y2="17"></line>
                        <line x1="14" y1="11" x2="14" y2="17"></line>
                    </svg>
                </button>
            </div>
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

        if (result.error) {
            console.error('[tasks] fetch-subtasks error', result.error);
            return;
        }
        const childTasks = result.tasks || [];

        if (childTasks.length === 0) {
            console.warn('[tasks] No subtasks returned from server');
        }

        // Merge child tasks into global state.tasks for editing and lookups
        if (!Array.isArray(state.tasks)) state.tasks = [];
        for (const ct of childTasks) {
            const ctId = parseInt(ct.id);
            const existingIdx = state.tasks.findIndex(t => parseInt(t.id) === ctId);
            if (existingIdx === -1) {
                state.tasks.push(ct);
            } else {
                state.tasks[existingIdx] = {...state.tasks[existingIdx], ...ct};
            }
        }
        
        // Separate incomplete and completed subtasks
        const incomplete = childTasks.filter(t => !t.done);
        const completed = childTasks.filter(t => t.done);
        
        // Sort completed by completion time (most recent first)
        completed.sort((a, b) => new Date(b.completedAt) - new Date(a.completedAt));
        
        const container = document.createElement('div');
        container.className = 'subtasks-container';
        container.dataset.parentId = parentEl.dataset.taskId;
        
        try {
            let html = '';
            
            // Incomplete subtasks section (sortable)
            if (incomplete.length > 0) {
                html += '<div class="subtasks-incomplete-group">';
                html += incomplete.map((t, idx) => {

                    return createTaskElement(t);
                }).join('');
                html += '</div>';
            }
            
            // Completed subtasks section (collapsed by default)
            if (completed.length > 0) {
                html += `
                    <div class="subtasks-completed-group">
                        <div class="subtasks-completed-header collapsible" data-parent="${uuid}">
                            <span class="collapse-icon">▶</span> Completed (${completed.length})
                        </div>
                        <div class="subtasks-completed-items collapsed">
                `;
                html += completed.map((t, idx) => {
                    return createTaskElement(t);
                }).join('');
                html += `
                        </div>
                    </div>
                `;
            }
            
            container.innerHTML = html;
        } catch (renderErr) {
            console.error('[tasks] Error rendering subtasks:', renderErr);
            return;
        }
        parentEl.parentNode.insertBefore(container, parentEl.nextSibling);

        // Collapse completed subtasks by default; add header toggle handler
        const completedHeader = container.querySelector('.subtasks-completed-header');
        if (completedHeader) {
            completedHeader.addEventListener('click', (e) => {
                const itemsDiv = container.querySelector('.subtasks-completed-items');
                if (!itemsDiv) return;
                const collapsed = itemsDiv.classList.toggle('collapsed');
                const icon = completedHeader.querySelector('.collapse-icon');
                if (icon) icon.textContent = collapsed ? '▶' : '▼';
            });
        }

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

            loadSubtasksForParent(parentUuid, parentEl);
        }
    });
}

function attachTaskItemListeners(container) {
    container = container || document;
    container.querySelectorAll('.task-item').forEach(item => {
        const checkbox = item.querySelector('.task-checkbox');
        if (checkbox) {
            let togglePending = false;
            const handleToggle = async (e) => {
                e.preventDefault();
                e.stopPropagation();
                if (togglePending) return;
                togglePending = true;
                try {
                    const taskID = parseInt(item.dataset.taskId);
                    await toggleTask(taskID);
                } finally {
                    togglePending = false;
                }
            };
            checkbox.addEventListener('click', handleToggle);
            checkbox.addEventListener('touchend', handleToggle, {passive: false});
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

        const inProgressBtn = item.querySelector('.task-inprogress-btn');
        if (inProgressBtn) {
            inProgressBtn.onclick = (e) => {
                e.stopPropagation();
                const taskID = inProgressBtn.dataset.taskId;
                toggleInProgress(taskID);
            };
        }

        const editBtn = item.querySelector('.task-edit-btn');
        if (editBtn) {
            editBtn.onclick = (e) => {
                e.stopPropagation();
                const taskID = parseInt(editBtn.dataset.taskId);
                // Try to find the task in the usual places
                let task = (state.tasks || []).find(t => parseInt(t.id) === taskID);
                if (!task && state.tagsViewTasks) task = state.tagsViewTasks.find(t => parseInt(t.id) === taskID);
                if (task) {
                    openTaskEditor(task);
                } else {
                    // Fallback: try to fetch task details from server (best-effort)
                    (async () => {
                        try {
                            const resp = await fetch('/api/tabs/tasks/action/get-task', {
                                method: 'POST',
                                headers: {'Content-Type': 'application/json'},
                                body: JSON.stringify({taskId: taskID})
                            });
                            if (resp.ok) {
                                const res = await resp.json();
                                if (res.task) openTaskEditor(res.task);
                            }
                        } catch (err) {
                            console.error('[tasks] Failed to fetch task for editing:', err);
                        }
                    })();
                }
            };
        }

        const cmdInput = item.querySelector('.task-command-input');
        if (cmdInput) {
            cmdInput.addEventListener('keydown', async (e) => {
                if (e.key === 'Enter') {
                    e.stopPropagation();
                    e.preventDefault();
                    const val = cmdInput.value.trim();
                    if (!val) return;
                    try {
                        await processCommand(parseInt(item.dataset.taskId), val);
                        cmdInput.value = '';
                        // After processing, re-focus the (possibly replaced) command input (allow DOM update)
                        setTimeout(() => {
                            const sel = `.task-item[data-task-id="${item.dataset.taskId}"] .task-command-input, .draggable-parent[data-task-id="${item.dataset.taskId}"] .task-command-input`;
                            const newInput = document.querySelector(sel);
                            if (newInput) {
                                try {
                                    newInput.focus({preventScroll: true});
                                    newInput.setSelectionRange(newInput.value.length, newInput.value.length);
                                } catch (e) {
                                }
                            } else {
                                try {
                                    cmdInput.focus();
                                    cmdInput.setSelectionRange(cmdInput.value.length, cmdInput.value.length);
                                } catch (e) {
                                }
                            }
                        }, 50);
                    } catch (err) {
                        console.error('[tasks] Command execution failed:', err);
                        alert('Command failed: ' + (err.message || err));
                    }
                } else if (e.key === 'Tab') {
                    e.stopPropagation();
                    e.preventDefault();
                    // Move focus to next/previous visible command input
                    const allInputs = Array.from(document.querySelectorAll('.task-command-input'));
                    const visibleInputs = allInputs.filter(el => {
                        if (!el) return false;
                        if (el.offsetParent === null && el.getClientRects().length === 0) return false;
                        if (el.disabled) return false;
                        return true;
                    });
                    if (visibleInputs.length === 0) return;
                    let idx = visibleInputs.indexOf(cmdInput);
                    if (idx === -1 && cmdInput.dataset && cmdInput.dataset.taskId) {
                        idx = visibleInputs.findIndex(el => el.dataset && el.dataset.taskId === cmdInput.dataset.taskId);
                    }
                    if (idx === -1) idx = 0;
                    let nextIdx;
                    if (e.shiftKey) {
                        nextIdx = idx > 0 ? idx - 1 : visibleInputs.length - 1;
                    } else {
                        nextIdx = (idx + 1) % visibleInputs.length;
                    }
                    const nextInput = visibleInputs[nextIdx];
                    if (!nextInput) return;
                    try {
                        nextInput.focus({preventScroll: true});
                    } catch (err) {
                        try {
                            nextInput.focus();
                        } catch (err2) {
                        }
                    }
                    try {
                        nextInput.setSelectionRange(nextInput.value.length, nextInput.value.length);
                    } catch (err) {
                    }
                    try {
                        nextInput.click();
                    } catch (err) {
                    }
                    if (typeof navController !== 'undefined' && navController) {
                        try {
                            navController.focusOnServices = false;
                            navController.selectedIndex = -1;
                        } catch (e) {
                        }
                    }
                }
            });
        }

        item.onclick = (e) => {
            if (e.target.closest('.task-checkbox') || e.target.closest('.subtask-toggle') || e.target.closest('.task-delete') || e.target.closest('.task-add-subtask') || e.target.closest('.task-edit-btn') || e.target.closest('.task-command-input')) return;
            const taskID = parseInt(item.dataset.taskId);
            const task = (state.tasks || []).find(t => parseInt(t.id) === taskID);
            if (task) openTaskEditor(task);
        };
    });
}

function attachTaskItemListenersForTagsView(container) {
    container = container || document;
    container.querySelectorAll('.task-item').forEach(item => {
        const checkbox = item.querySelector('.task-checkbox');
        if (checkbox) {
            let togglePending = false;
            const handleToggle = async (e) => {
                e.preventDefault();
                e.stopPropagation();
                if (togglePending) return;
                togglePending = true;
                try {
                    const taskID = parseInt(item.dataset.taskId);
                    await toggleTaskInTagsView(taskID);
                } finally {
                    togglePending = false;
                }
            };
            checkbox.addEventListener('click', handleToggle);
            checkbox.addEventListener('touchend', handleToggle, {passive: false});
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

        const inProgressBtn = item.querySelector('.task-inprogress-btn');
        if (inProgressBtn) {
            inProgressBtn.onclick = (e) => {
                e.stopPropagation();
                const taskID = inProgressBtn.dataset.taskId;
                toggleInProgress(taskID);
            };
        }

        const editBtn = item.querySelector('.task-edit-btn');
        if (editBtn) {
            editBtn.onclick = (e) => {
                e.stopPropagation();
                const taskID = parseInt(editBtn.dataset.taskId);
                let task = state.tagsViewTasks.find(t => t.id === taskID);
                if (!task && state.tasks) task = (state.tasks || []).find(t => parseInt(t.id) === taskID);
                if (task) {
                    openTaskEditor(task);
                } else {
                    (async () => {
                        try {
                            const resp = await fetch('/api/tabs/tasks/action/get-task', {
                                method: 'POST',
                                headers: {'Content-Type': 'application/json'},
                                body: JSON.stringify({taskId: taskID})
                            });
                            if (resp.ok) {
                                const res = await resp.json();
                                if (res.task) openTaskEditor(res.task);
                            }
                        } catch (err) {
                            console.error('[tasks] Failed to fetch task for editing:', err);
                        }
                    })();
                }
            };
        }

        const cmdInput = item.querySelector('.task-command-input');
        if (cmdInput) {
            cmdInput.addEventListener('keydown', async (e) => {
                if (e.key === 'Enter') {
                    e.stopPropagation();
                    e.preventDefault();
                    const val = cmdInput.value.trim();
                    if (!val) return;
                    try {
                        await processCommand(parseInt(item.dataset.taskId), val);
                        cmdInput.value = '';
                        // After processing, re-focus the (possibly replaced) command input (allow DOM update)
                        setTimeout(() => {
                            const sel = `.task-item[data-task-id="${item.dataset.taskId}"] .task-command-input, .draggable-parent[data-task-id="${item.dataset.taskId}"] .task-command-input`;
                            const newInput = document.querySelector(sel);
                            if (newInput) {
                                try {
                                    newInput.focus({preventScroll: true});
                                    newInput.setSelectionRange(newInput.value.length, newInput.value.length);
                                } catch (e) {
                                }
                            } else {
                                try {
                                    cmdInput.focus();
                                    cmdInput.setSelectionRange(cmdInput.value.length, cmdInput.value.length);
                                } catch (e) {
                                }
                            }
                        }, 50);
                    } catch (err) {
                        console.error('[tasks] Command execution failed:', err);
                        alert('Command failed: ' + (err.message || err));
                    }
                } else if (e.key === 'Tab') {
                    e.stopPropagation();
                    e.preventDefault();
                    // Move focus to next/previous visible command input
                    const allInputs = Array.from(document.querySelectorAll('.task-command-input'));
                    const visibleInputs = allInputs.filter(el => {
                        if (!el) return false;
                        if (el.offsetParent === null && el.getClientRects().length === 0) return false;
                        if (el.disabled) return false;
                        return true;
                    });
                    if (visibleInputs.length === 0) return;
                    let idx = visibleInputs.indexOf(cmdInput);
                    if (idx === -1 && cmdInput.dataset && cmdInput.dataset.taskId) {
                        idx = visibleInputs.findIndex(el => el.dataset && el.dataset.taskId === cmdInput.dataset.taskId);
                    }
                    if (idx === -1) idx = 0;
                    let nextIdx;
                    if (e.shiftKey) {
                        nextIdx = idx > 0 ? idx - 1 : visibleInputs.length - 1;
                    } else {
                        nextIdx = (idx + 1) % visibleInputs.length;
                    }
                    const nextInput = visibleInputs[nextIdx];
                    if (!nextInput) return;
                    try {
                        nextInput.focus({preventScroll: true});
                    } catch (err) {
                        try {
                            nextInput.focus();
                        } catch (err2) {
                        }
                    }
                    try {
                        nextInput.setSelectionRange(nextInput.value.length, nextInput.value.length);
                    } catch (err) {
                    }
                    try {
                        nextInput.click();
                    } catch (err) {
                    }
                    if (typeof navController !== 'undefined' && navController) {
                        try {
                            navController.focusOnServices = false;
                            navController.selectedIndex = -1;
                        } catch (e) {
                        }
                    }
                }
            });
        }

        item.onclick = (e) => {
            if (e.target.closest('.task-checkbox') || e.target.closest('.subtask-toggle') || e.target.closest('.task-delete') || e.target.closest('.task-add-subtask') || e.target.closest('.task-edit-btn') || e.target.closest('.task-command-input')) return;
            const taskID = parseInt(item.dataset.taskId);
            const task = state.tagsViewTasks.find(t => t.id === taskID);
            if (task) openTaskEditor(task);
        };
    });
}

async function processCommand(taskId, commandText) {
    const cmdText = (commandText || '').trim();
    if (!cmdText) return;

    try {
        const response = await fetch('/api/tabs/tasks/action/run-task-command', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({taskId, command: cmdText, view: state.currentView})
        });
        // Read response body as text to provide better error messages on non-2xx responses
        const respText = await response.text();
        if (!response.ok) {
            let msg = `HTTP ${response.status}`;
            try {
                if (respText && respText.trim() !== '') {
                    const parsed = JSON.parse(respText);
                    if (parsed && (parsed.error || parsed.message)) {
                        msg = parsed.error || parsed.message;
                    } else {
                        msg = respText.trim();
                    }
                }
            } catch (e) {
                if (respText && respText.trim() !== '') msg = respText.trim();
            }
            throw new Error(msg);
        }
        let res = {};
        if (respText && respText.trim() !== '') {
            try {
                res = JSON.parse(respText);
            } catch (e) { /* ignore parse error */
            }
        }
        if (res.error) throw new Error(res.error);

        // In-progress time handling: update state.inProgress and refresh timer display
        if (Object.prototype.hasOwnProperty.call(res, 'inProgressTotalSeconds')) {
            const totalSeconds = res.inProgressTotalSeconds || 0;
            state.inProgress[taskId] = state.inProgress[taskId] || {startedAt: null, accumulatedMs: 0, running: false};
            // Reset accumulatedMs and update startedAt to now so timer starts from 0
            state.inProgress[taskId].accumulatedMs = totalSeconds * 1000;
            if (state.inProgress[taskId].running) {
                state.inProgress[taskId].startedAt = Date.now();
            }
            saveInProgressState();
            updateAllInProgressDisplays();
        }

        // Color handling: if server returned a color property (including empty to clear)
        if (Object.prototype.hasOwnProperty.call(res, 'color')) {
            const color = res.color || '';
            const idx = (state.tasks || []).findIndex(t => parseInt(t.id) === parseInt(taskId));
            if (idx !== -1) state.tasks[idx] = Object.assign({}, state.tasks[idx], {color});
            const idx2 = (state.tagsViewTasks || []).findIndex(t => parseInt(t.id) === parseInt(taskId));
            if (idx2 !== -1) state.tagsViewTasks[idx2] = Object.assign({}, state.tagsViewTasks[idx2], {color});
            const el = document.querySelector(`.task-item[data-task-id="${taskId}"]`) || document.querySelector(`.draggable-parent[data-task-id="${taskId}"]`);
            if (el) {
                if (color) {
                    el.style.borderLeft = `4px solid ${color}`;
                    el.style.paddingLeft = '8px';
                } else {
                    el.style.borderLeft = '';
                    el.style.paddingLeft = '';
                }
            }
        }

        // If server returned a full TaskRow (with scheduledAt/hasScheduledTime), merge and refresh
        if (Object.prototype.hasOwnProperty.call(res, 'scheduledAt')) {
            // If the task was running locally and the server no longer marks it as
            // in-progress (rescheduling stops the timer), clear the local timer state.
            const wasRunning = state.inProgress && state.inProgress[String(taskId)] && state.inProgress[String(taskId)].running;
            if (wasRunning && !res.inProgress) {
                clearLocalInProgress(taskId, typeof res.inProgressTotalSeconds === 'number' ? res.inProgressTotalSeconds : undefined);
            }

            const idx = (state.tasks || []).findIndex(t => parseInt(t.id) === parseInt(taskId));
            if (idx !== -1) {
                state.tasks[idx] = Object.assign({}, state.tasks[idx], res);
            }
            const idx2 = (state.tagsViewTasks || []).findIndex(t => parseInt(t.id) === parseInt(taskId));
            if (idx2 !== -1) {
                state.tagsViewTasks[idx2] = Object.assign({}, state.tagsViewTasks[idx2], res);
            }
            if (state.currentView === 'tags') {
                displayTagsView();
            } else {
                renderTasksView();
            }
            // After re-rendering the view, attempt to re-focus the command input for this task (allow DOM to update)
            setTimeout(() => {
                const sel = `.task-item[data-task-id="${taskId}"] .task-command-input, .draggable-parent[data-task-id="${taskId}"] .task-command-input`;
                const newInput = document.querySelector(sel);
                if (newInput) {
                    try {
                        // Try several focus strategies to ensure input receives keyboard events
                        try {
                            newInput.focus({preventScroll: true});
                        } catch (e) {
                            newInput.focus();
                        }
                        try {
                            newInput.setSelectionRange(newInput.value.length, newInput.value.length);
                        } catch (e) {
                        }
                        try {
                            newInput.click();
                        } catch (e) {
                        }
                        try {
                            newInput.dispatchEvent(new Event('focus'));
                        } catch (e) {
                        }
                        if (typeof navController !== 'undefined' && navController) {
                            try {
                                navController.focusOnServices = false;
                                navController.selectedIndex = -1;
                            } catch (e) {
                            }
                        }
                    } catch (e) {
                    }
                }
            }, 50);
        }


    } catch (err) {
        console.error('[tasks] processCommand error:', err);
        alert('Command failed: ' + (err.message || err));
        throw err;
    }
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
        // Replace placeholder if present, otherwise insert before checkbox
        const placeholder = el.querySelector('.subtask-toggle-placeholder');
        if (placeholder && placeholder.parentNode) {
            placeholder.parentNode.replaceChild(toggle, placeholder);
        } else if (checkbox && checkbox.parentNode) {
            checkbox.parentNode.insertBefore(toggle, checkbox);
        } else {
            el.insertBefore(toggle, el.firstChild);
        }
    }
}

function setupSubtaskDragAndDrop(container, parentUuid) {
    let draggedItem = null;
    let draggedFromIndex = null;

    // Only get incomplete task items - completed tasks should not be draggable
    const incompleteGroup = container.querySelector('.subtasks-incomplete-group');
    const items = incompleteGroup ? incompleteGroup.querySelectorAll('.task-item') : [];

    if (items.length === 0) {

        return;
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

        });

        item.addEventListener('dragend', (e) => {
            item.style.opacity = '1';
            item.style.cursor = 'grab';
            draggedItem = null;
            draggedFromIndex = null;
            // Remove any visual drop indicators
            items.forEach(it => it.classList.remove('drag-over-top', 'drag-over-bottom'));
        });

        item.addEventListener('dragover', (e) => {
            e.preventDefault();
            e.dataTransfer.dropEffect = 'move';

            if (item === draggedItem) return;

            // Clear positional indicators on sibling items
            items.forEach(it => it.classList.remove('drag-over-top', 'drag-over-bottom'));

            const rect = item.getBoundingClientRect();
            const offsetY = e.clientY - rect.top;
            const insertBefore = offsetY < (rect.height / 2);

            item.classList.add(insertBefore ? 'drag-over-top' : 'drag-over-bottom');
        });

        item.addEventListener('dragleave', (e) => {
            item.classList.remove('drag-over-top', 'drag-over-bottom');
        });

        item.addEventListener('drop', async (e) => {
            e.preventDefault();
            item.classList.remove('drag-over-top', 'drag-over-bottom');

            if (!draggedItem || draggedItem === item) return;

            const rect = item.getBoundingClientRect();
            const insertBefore = (e.clientY - rect.top) < (rect.height / 2);

            if (insertBefore) {
                item.parentNode.insertBefore(draggedItem, item);
            } else {
                item.parentNode.insertBefore(draggedItem, item.nextSibling);
            }

            // Get task ID and call API
            const taskId = parseInt(draggedItem.dataset.taskId);
            const newIndex = Array.from(incompleteGroup.querySelectorAll('.task-item')).indexOf(draggedItem);

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

                }

            } catch (err) {
                console.error('[tasks] Reorder error:', err);
                alert('Failed to reorder: ' + err.message);
                // Reload subtasks to restore proper order
                loadTasks();
            }
        });
    });
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

        // If in tags view, reload the tags view
        if (state.currentView === 'tags') {
            await loadTagsView();
            return;
        }

        // Update task state locally for other views
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
        if (task.done) {
            clearLocalInProgress(taskID);
        }
        const parentUuid = getTaskParentUuid(task);
        if (parentUuid && state.expandedParents.has(parentUuid)) {
            await refreshParentSubtasks(parentUuid);
            return;
        }
        renderTasksView();

    } catch (err) {
        console.error('Failed to toggle task:', err);
        alert('Failed to toggle task: ' + err.message);
        // Reload on network errors
        if (state.currentView === 'tags') {
            await loadTagsView();
        } else {
            await loadTasks();
        }
    }
}

async function toggleTaskInTagsView(taskID) {
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

        // Mark any in-progress tracking as stopped for this task
        clearLocalInProgress(taskID);
        // Reload the tags view
        await loadTagsView();

    } catch (err) {
        console.error('Failed to toggle task:', err);
        alert('Failed to toggle task: ' + err.message);
        await loadTagsView();
    }
}

function reorganizeTaskSections() {
    // Reorganize tasks into their proper sections based on done status
    const tasksToShow = state.tasks.filter(isTopLevelTask);

    if (state.currentView === 'recent') {
        tasksToShow.sort((a, b) => new Date(b.updatedAt) - new Date(a.updatedAt));
        elements.tasksList.innerHTML = tasksToShow.map(task => createTaskElement(task)).join('');
    } else {
        const todayIncomplete = tasksToShow.filter(t => !t.done && (!t.category || t.category === 'today'));
        const pastIncomplete = tasksToShow.filter(t => !t.done && t.category === 'past');
        const completed = tasksToShow.filter(t => t.done);

        // Sort incomplete by scheduled date (earliest first)
        todayIncomplete.sort((a, b) => new Date(a.scheduledAt) - new Date(b.scheduledAt));
        pastIncomplete.sort((a, b) => new Date(b.scheduledAt) - new Date(a.scheduledAt));

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
            let togglePending = false;
            const handleToggle = async (e) => {
                e.preventDefault();
                e.stopPropagation();
                if (togglePending) return;
                togglePending = true;
                try {
                    const taskId = parseInt(item.dataset.taskId);
                    await toggleTask(taskId);
                } finally {
                    togglePending = false;
                }
            };
            checkbox.addEventListener('click', handleToggle);
            checkbox.addEventListener('touchend', handleToggle, {passive: false});
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

    // Update task state in both regular and tags view tasks
    const updateTaskInList = (list) => {
        if (!Array.isArray(list)) return -1;
        const idx = list.findIndex(t => t.id === msg.taskId);
        if (idx !== -1) {
            const task = list[idx];
            if (msg.done !== undefined) task.done = msg.done;
            if (msg.title !== undefined) task.title = msg.title;
            if (msg.tags !== undefined) task.tags = msg.tags;
            if (msg.color !== undefined) task.color = msg.color;
            if (msg.scheduledAt !== undefined) task.scheduledAt = msg.scheduledAt;
        }
        return idx;
    };

    const taskIndex = updateTaskInList(state.tasks);
    updateTaskInList(state.tagsViewTasks);
    updateTaskInList(state.tagsViewAllTasks);

    if (taskIndex === -1 && (!state.tagsViewTasks || state.tagsViewTasks.findIndex(t => t.id === msg.taskId) === -1)) {
        console.warn('[tasks] Task not found in state:', msg.taskId);
        loadTasks();
        return;
    }

    const task = state.tasks[taskIndex] || state.tagsViewTasks.find(t => t.id === msg.taskId);
    const oldDone = task.done;

    // Update in-progress state if provided in the message
    if (msg.inProgress !== undefined) {
        const id = String(msg.taskId);
        if (!state.inProgress) state.inProgress = {};
        const prev = state.inProgress[id] || {startedAt: null, accumulatedMs: 0, running: false};
        if (msg.inProgress) {
            prev.startedAt = msg.inProgressStartedAt ? Date.parse(msg.inProgressStartedAt) : Date.now();
            prev.running = true;
        } else {
            prev.accumulatedMs = (msg.inProgressTotalSeconds || 0) * 1000;
            prev.startedAt = null;
            prev.running = false;
        }
        state.inProgress[id] = prev;
        saveInProgressState();
        ensureInProgressTimer();
        try { updateAllInProgressDisplays(); } catch (err) { console.error('[tasks] updateAllInProgressDisplays error:', err); }
    }

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

        // Update left stripe color on the task element
        if (task.color && task.color.trim()) {
            taskEl.style.borderLeft = `4px solid ${task.color}`;
            taskEl.style.paddingLeft = '8px';
        } else {
            taskEl.style.borderLeft = '';
            taskEl.style.paddingLeft = '';
        }

        // Update title if changed
        if (msg.title !== undefined) {
            const titleEl = taskEl.querySelector('.task-title');
            if (titleEl) {
                titleEl.textContent = task.title;
            }
        }



        // If task was marked done, clear in-progress state
        if (msg.done === true) {
            try {
                clearLocalInProgress(msg.taskId, msg.inProgressTotalSeconds);
            } catch (err) {
                console.error('[tasks] Failed to clear local in-progress on done:', err);
            }
        }

        // If done status changed for a subtask, refresh only that parent's subtask block.
        const parentUuid = getTaskParentUuid(task);
        if (oldDone !== task.done && parentUuid && state.expandedParents.has(parentUuid)) {
            refreshParentSubtasks(parentUuid);
        }
    }

    // If done status changed, we need to refresh the view to move the task to the correct section
    if (oldDone !== task.done) {
        if (state.currentView === 'tags') {
            displayTagsView();
        } else {
            renderTasksView();
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

function setupTagsViewFiltering() {
    // Listen for tag clicks and reload in tags view
    document.addEventListener('click', async (e) => {
        if (e.target.closest('.tag-item') && state.currentView === 'tags') {
            e.preventDefault();
            const tagItem = e.target.closest('.tag-item');
            if (!tagItem) return;
            
            const tag = tagItem.dataset.tag;
            if (!tag) return;
            
            // Update current filter and save it
            state.currentFilter = tag;
            state.tagsViewFilter = tag;
            
            // Update active state
            const tagItems = document.querySelectorAll('.tag-item');
            tagItems.forEach(t => t.classList.remove('active'));
            tagItem.classList.add('active');
            
            // Reload tags view
            await loadTagsView();
        }
    });
}

function setupViewSwitching() {
    const viewItems = document.querySelectorAll('.view-item');
    viewItems.forEach(item => {
        item.addEventListener('click', async (e) => {
            e.preventDefault();
            const newView = item.dataset.view;

            if (newView && newView !== state.currentView) {
                // If leaving tags view, save its current filter and reset to 'all'
                if (state.currentView === 'tags') {
                    state.tagsViewFilter = state.currentFilter;
                    state.currentFilter = 'all';
                }

                // Update the current view
                state.currentView = newView;

                // If entering tags view, restore previous tags filter
                if (newView === 'tags') {
                    state.currentFilter = state.tagsViewFilter || 'all';
                }

                // Update active state
                viewItems.forEach(v => v.classList.remove('active'));
                item.classList.add('active');

                // Reload tasks for the newly selected view
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
        // Build payload and include the currently-selected tag when in Tags view
        const payload = {title, hasScheduledTime: false};
        if (state.currentView === 'tags' && state.currentFilter && state.currentFilter !== 'all') {
            payload.tags = state.currentFilter;
        }

        const response = await fetch('/api/tabs/tasks/action/create-task', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(payload)
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
                title: title.trim(),
                // Explicitly ensure subtasks are created without a scheduled date/time
                scheduledAt: null,
                hasScheduledTime: false
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

// Setup tag filtering for tags view
setupTagsViewFiltering();

// Setup search input
setupSearchInput();

// Setup new task input
setupNewTaskInput();

// Initialize task editor
initializeTaskEditor();

// Load persisted in-progress state and start timer if needed
loadInProgressState();
ensureInProgressTimer();

// Load tasks on startup
loadTasks();

// Delegate Enter key handling for command inputs to ensure dynamically created inputs work.
// Use capture phase and a form submit fallback for cross-browser reliability.
(function () {
    function isEditableTarget(target) {
        if (!target || !(target instanceof HTMLElement)) return false;
        if (target.isContentEditable) return true;
        if (target.matches('input, textarea, select')) return true;
        return !!(target.closest && target.closest('input, textarea, select, [contenteditable="true"]'));
    }

    function isPrintableTaskCommandKey(e) {
        if (e.defaultPrevented) return false;
        if (e.ctrlKey || e.metaKey || e.altKey) return false;
        if (e.key === 'Enter' || e.key === 'Escape' || e.key === 'Tab') return false;
        if (e.key.startsWith('Arrow')) return false;
        return e.key.length === 1;
    }

    function getSelectedTaskCommandInput() {
        const selectedTask = document.querySelector('.task-item.task-selected');
        if (!selectedTask) return null;
        return selectedTask.querySelector('.task-command-input');
    }

    function handleCommandInputSubmit(inputEl) {
        return async function () {
            try {
                const val = (inputEl.value || '').trim();
                if (!val) return;
                const taskId = parseInt(inputEl.dataset.taskId);
                await processCommand(taskId, val);
                inputEl.value = '';
                // Focus the (possibly re-rendered) command input for this task (allow DOM update)
                setTimeout(() => {
                    const sel = `.task-item[data-task-id="${taskId}"] .task-command-input, .draggable-parent[data-task-id="${taskId}"] .task-command-input`;
                    const newInput = document.querySelector(sel);
                    if (newInput) {
                        try {
                            newInput.focus();
                            newInput.setSelectionRange(newInput.value.length, newInput.value.length);
                        } catch (e) {
                        }
                    } else {
                        try {
                            inputEl.focus();
                            inputEl.setSelectionRange(inputEl.value.length, inputEl.value.length);
                        } catch (e) {
                        }
                    }
                }, 50);
            } catch (err) {
                console.error('[tasks] Command execution failed (delegate):', err);
                alert('Command failed: ' + (err.message || err));
            }
        };
    }

    // Keydown capture handler - should run before other bubble handlers and is reliable in most browsers.
    document.addEventListener('keydown', async (e) => {
        try {
            const target = e.target;
            if (!target || !(target instanceof HTMLElement)) return;

            if (!isEditableTarget(target) && isPrintableTaskCommandKey(e)) {
                const selectedInput = getSelectedTaskCommandInput();
                if (selectedInput) {
                    e.preventDefault();
                    e.stopPropagation();
                    try {
                        selectedInput.focus({preventScroll: true});
                    } catch (err) {
                        selectedInput.focus();
                    }
                    const start = selectedInput.selectionStart ?? selectedInput.value.length;
                    const end = selectedInput.selectionEnd ?? selectedInput.value.length;
                    selectedInput.setRangeText(e.key, start, end, 'end');
                    return;
                }
            }

            const input = (target.matches && target.matches('.task-command-input')) ? target : (target.closest ? target.closest('.task-command-input') : null);
            if (!input) return;
            if (e.key === 'Escape' || e.keyCode === 27 || e.which === 27) {
                e.stopPropagation();
                e.preventDefault();
                input.blur();
                return;
            }
            const isEnter = (e.key === 'Enter' || e.keyCode === 13 || e.which === 13);
            if (!isEnter) return;
            e.stopPropagation();
            e.preventDefault();
            await handleCommandInputSubmit(input)();
        } catch (err) {
            console.error('[tasks] command input delegate error:', err);
        }
    }, {capture: true});

    // Global Tab handler (fallback) to navigate command inputs if per-input handlers fail.
    document.addEventListener('keydown', (e) => {
        try {
            if (e.key !== 'Tab' && e.keyCode !== 9 && e.which !== 9) return;
            const target = e.target;
            if (!target || !(target instanceof HTMLElement)) return;
            const input = (target.matches && target.matches('.task-command-input')) ? target : (target.closest ? target.closest('.task-command-input') : null);
            if (!input) return;
            e.preventDefault();
            e.stopPropagation();
            const allInputs = Array.from(document.querySelectorAll('.task-command-input'));
            const visibleInputs = allInputs.filter(el => {
                if (!el) return false;
                if (el.offsetParent === null && el.getClientRects().length === 0) return false;
                if (el.disabled) return false;
                return true;
            });
            if (visibleInputs.length === 0) return;
            let idx = visibleInputs.indexOf(input);
            if (idx === -1 && input.dataset && input.dataset.taskId) {
                idx = visibleInputs.findIndex(el => el.dataset && el.dataset.taskId === input.dataset.taskId);
            }
            if (idx === -1) idx = 0;
            let nextIdx = e.shiftKey ? (idx > 0 ? idx - 1 : visibleInputs.length - 1) : ((idx + 1) % visibleInputs.length);
            const nextInput = visibleInputs[nextIdx];
            if (!nextInput) return;
            try {
                nextInput.focus({preventScroll: true});
            } catch (err) {
                try {
                    nextInput.focus();
                } catch (err2) {
                }
            }
            try {
                nextInput.setSelectionRange(nextInput.value.length, nextInput.value.length);
            } catch (err) {
            }
            if (typeof navController !== 'undefined' && navController) {
                try {
                    navController.focusOnServices = false;
                    navController.selectedIndex = -1;
                } catch (e) {
                }
            }
        } catch (err) {
            console.error('[tasks] global tab handler error:', err);
        }
    }, {capture: true});

    // Fallback: listen for form submit events from task-command-form (pressing Enter in many browsers will submit the form).
    document.addEventListener('submit', async (e) => {
        try {
            const form = e.target;
            if (!form || !(form instanceof HTMLFormElement)) return;
            if (!form.classList.contains('task-command-form')) return;
            e.preventDefault();
            const input = form.querySelector('.task-command-input');
            if (!input) return;
            await handleCommandInputSubmit(input)();
        } catch (err) {
            console.error('[tasks] command form submit error:', err);
        }
    });
})();

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
        // Build payload and only include color when the user explicitly provided one (via the hex input)
        const payload = {
            taskId: state.currentEditingTask.id,
            title: elements.taskTitle.value,
            tags: elements.taskTags.value,
            scheduledAt: scheduledDateISO,
            hasScheduledTime: elements.taskHasScheduledTime.checked,
            pattern: pattern
        };
        const hexVal = (elements.taskColorHex.value || '').trim();
        if (/^#[0-9A-Fa-f]{6}$/.test(hexVal)) {
            payload.color = hexVal;
        }

        const response = await fetch('/api/tabs/tasks/action/update-task', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify(payload)
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
