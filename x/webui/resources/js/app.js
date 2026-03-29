import {initSSE} from './vendor/sse.js';

const tabsNav = document.getElementById('tabs-nav');
const pinnedTabs = document.getElementById('pinned-tabs');
const tabsContent = document.getElementById('tabs-content');

let activeTabId = null;
const tabsModules = {};

// Default hardcoded tab order (used as fallback)
const defaultTabOrder = {
    'dashboard': 0,
    'alerts': 1,
    'errors': 2,
    'statusmon': 3,
    'tasks': 4,
    'notes': 5,
    'links': 6,
    'journal': 7,
    'idle': 8,
    'aurora': 9,
    'tides': 10,
    'gps': 11,
    'temps': 12,
    'messages': 13
};

// Load the saved tab order from the backend
async function getTabOrder() {
    try {
        const response = await fetch('/api/tabs/webui/action/get-tab-order', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({})
        });
        const data = await response.json();
        return data.order || {};
    } catch (err) {
        console.warn('Failed to load saved tab order, using defaults:', err);
        return {};
    }
}

async function loadTabs() {
    const response = await fetch('/api/tabs');
    const tabs = await response.json();

    // Load the saved tab order
    const savedOrder = await getTabOrder();

    // Merge saved order with defaults
    const effectiveOrder = {...defaultTabOrder, ...savedOrder};
    
    tabs.sort((a, b) => {
        const orderA = effectiveOrder[a.id] ?? 999;
        const orderB = effectiveOrder[b.id] ?? 999;
        if (orderA !== orderB) return orderA - orderB;
        // Fallback to alphabetical if same priority
        return a.title.localeCompare(b.title);
    });

    tabs.forEach(tab => {
        // Create tab link
        const link = document.createElement('div');
        link.className = 'tab-link';
        link.dataset.tabId = tab.id;
        link.draggable = true;
        link.onclick = () => switchTab(tab.id);
        if (tab.id === 'dashboard') {
            const label = document.createElement('span');
            label.textContent = tab.title;
            const dot = document.createElement('span');
            dot.className = 'sse-dot sse-disconnected';
            dot.id = 'sse-status';
            dot.title = 'SSE: disconnected';
            link.appendChild(label);
            link.appendChild(dot);
        } else {
            link.textContent = tab.title;
        }
        if (tab.id === 'dashboard') {
            pinnedTabs.appendChild(link);
        } else {
            tabsNav.appendChild(link);
            // Add drag-and-drop handlers for non-dashboard tabs
            setupTabDragAndDrop(link);
        }

        // Create tab content container
        const content = document.createElement('div');
        content.id = `tab-content-${tab.id}`;
        content.className = 'tab-content';
        content.innerHTML = tab.content;
        tabsContent.appendChild(content);

        if (tab.jsPath) {
            import(tab.jsPath).then(module => {
                tabsModules[tab.id] = module;
                if (module.init) module.init(content);
            }).catch(err => console.error(`Failed to load module for tab ${tab.id}:`, err));
        }
    });

    if (tabs.length > 0) {
        // Prefer 'dashboard' tab as the default if present
        const preferred = tabs.find(t => t.id === 'dashboard' || (t.title && t.title.toLowerCase() === 'dashboard'));
        const first = preferred || tabs[0];
        switchTab(first.id);
    }
}

let draggedElement = null;  // Global state for drag-and-drop

function setupTabDragAndDrop(link) {
    link.addEventListener('dragstart', (e) => {
        draggedElement = link;
        link.classList.add('dragging');
        e.dataTransfer.effectAllowed = 'move';
        e.dataTransfer.setData('text/html', link.innerHTML);
    });

    link.addEventListener('dragend', (e) => {
        draggedElement = null;
        link.classList.remove('dragging');
        document.querySelectorAll('.tab-link').forEach(el => {
            el.classList.remove('drag-over');
        });
    });

    link.addEventListener('dragover', (e) => {
        e.preventDefault();
        e.dataTransfer.dropEffect = 'move';

        // Only show drag-over if we're dragging a different element
        if (draggedElement && draggedElement !== link) {
            link.classList.add('drag-over');
        }
    });

    link.addEventListener('dragleave', (e) => {
        // Only remove if leaving this specific element
        if (e.target === link) {
            link.classList.remove('drag-over');
        }
    });

    link.addEventListener('drop', async (e) => {
        e.preventDefault();
        e.stopPropagation();

        if (!draggedElement || draggedElement === link) {
            return;
        }

        // Remove all drag-over classes
        document.querySelectorAll('.tab-link').forEach(el => {
            el.classList.remove('drag-over');
        });

        // Perform the swap
        const tabsNavContainer = tabsNav;
        const allLinks = Array.from(tabsNavContainer.querySelectorAll('.tab-link'));
        const draggedIndex = allLinks.indexOf(draggedElement);
        const targetIndex = allLinks.indexOf(link);

        if (draggedIndex !== -1 && targetIndex !== -1 && draggedIndex !== targetIndex) {
            // Insert at the correct position
            if (draggedIndex < targetIndex) {
                // Moving right: insert after target
                link.parentNode.insertBefore(draggedElement, link.nextSibling);
            } else {
                // Moving left: insert before target
                link.parentNode.insertBefore(draggedElement, link);
            }

            // Save the new tab order
            await saveTabOrder();
        }
    });
}

async function saveTabOrder() {
    const tabLinks = document.querySelectorAll('.tab-link');
    const newOrder = {};

    tabLinks.forEach((link, index) => {
        const tabId = link.dataset.tabId;
        newOrder[tabId] = index;
    });

    try {
        const response = await fetch('/api/tabs/webui/action/save-tab-order', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({order: newOrder})
        });

        if (!response.ok) {
            console.error('Failed to save tab order:', response.statusText);
        }
    } catch (err) {
        console.error('Error saving tab order:', err);
    }
}

function switchTab(tabId) {
    if (activeTabId === tabId) return;

    document.querySelectorAll('.tab-link').forEach(link => {
        link.classList.toggle('active', link.dataset.tabId === tabId);
    });

    document.querySelectorAll('.tab-content').forEach(content => {
        content.classList.toggle('active', content.id === `tab-content-${tabId}`);
    });

    activeTabId = tabId;
    focusOnTabs = true; // Reset to tabs focus when switching
}

// Export switchTab globally so dashboard panels can navigate to tabs
window.switchTab = switchTab;

function updateTabVisualFocus(tabIndex) {
    // Update visual focus (keyboard focus highlight) without switching tabs
    document.querySelectorAll('.tab-link').forEach((link, index) => {
        link.classList.toggle('tab-focused', index === tabIndex);
    });
}


let selectedTabIndex = 0; // Track which tab is selected
let focusedTabIndex = 0; // Track which tab has keyboard focus (visually highlighted but not active)
let focusOnTabs = true;  // Is keyboard focus on tabs or on items?
let allTabIds = [];      // List of tab IDs in order


function setupTabKeyboardNavigation() {
    document.addEventListener('keydown', (e) => {
        // Don't process keyboard navigation if we're currently dragging
        if (draggedElement) return;
        
        // Get all tab links
        const tabLinks = Array.from(document.querySelectorAll('.tab-link'));
        if (tabLinks.length === 0) return;

        // Store tab IDs if not already done
        if (allTabIds.length === 0) {
            allTabIds = tabLinks.map(link => link.dataset.tabId);
            selectedTabIndex = allTabIds.indexOf(activeTabId);
            focusedTabIndex = selectedTabIndex;
            updateTabVisualFocus(focusedTabIndex);
        }

        if (e.key === 'ArrowRight') {
            // Let tabs that manage their own horizontal panel navigation handle this key.
            if (tabsModules[activeTabId] && tabsModules[activeTabId].handlesHorizontalNav) return;
            e.preventDefault();
            if (focusOnTabs) {
                // Move focus to next tab (don't switch yet)
                if (focusedTabIndex < allTabIds.length - 1) {
                    focusedTabIndex++;
                    updateTabVisualFocus(focusedTabIndex);
                }
            }
        } else if (e.key === 'ArrowLeft') {
            // Let tabs that manage their own horizontal panel navigation handle this key.
            if (tabsModules[activeTabId] && tabsModules[activeTabId].handlesHorizontalNav) return;
            e.preventDefault();
            if (focusOnTabs) {
                // Move focus to previous tab (don't switch yet)
                if (focusedTabIndex > 0) {
                    focusedTabIndex--;
                    updateTabVisualFocus(focusedTabIndex);
                }
            }
        } else if (e.key === 'Enter') {
            e.preventDefault();
            if (focusOnTabs && focusedTabIndex !== selectedTabIndex) {
                // Switch to focused tab on Enter
                selectedTabIndex = focusedTabIndex;
                switchTab(allTabIds[focusedTabIndex]);
            }
        } else if (e.key === 'ArrowDown') {
            e.preventDefault();
            if (focusOnTabs && (activeTabId === 'alerts' || activeTabId === 'errors' || activeTabId === 'tasks' || activeTabId === 'journal' || activeTabId === 'movies')) {
                // Move focus from tabs to items - deselect tab visual
                focusOnTabs = false;
                updateTabVisualFocus(-1); // Clear any focused tab
                // Dispatch a pseudo-event to the tab module to select first item
                const tabModule = tabsModules[activeTabId];
                if (tabModule && tabModule.focusItems) {
                    tabModule.focusItems();
                }
            }
        } else if (e.key === 'ArrowUp') {
            e.preventDefault();
            if (!focusOnTabs && (activeTabId === 'alerts' || activeTabId === 'errors' || activeTabId === 'tasks' || activeTabId === 'journal' || activeTabId === 'movies')) {
                // Move focus from items back to tabs (only if at top of items)
                const tabModule = tabsModules[activeTabId];
                if (tabModule && tabModule.canReturnToTabs && tabModule.canReturnToTabs()) {
                    focusOnTabs = true;
                    // Sync focused tab to current active tab when returning
                    focusedTabIndex = selectedTabIndex;
                    updateTabVisualFocus(focusedTabIndex);
                }
            }
        }
    });
}

function setSseStatus(state) {
    const sseStatusEl = document.getElementById('sse-status');
    if (!sseStatusEl) return;
    sseStatusEl.classList.remove('connected', 'disconnected', 'reconnecting');
    if (state === 'open' || state === 'connected') {
        sseStatusEl.classList.add('connected');
        sseStatusEl.title = 'SSE: connected';
    } else if (state === 'reconnecting' || state === 'connecting') {
        sseStatusEl.classList.add('reconnecting');
        sseStatusEl.title = 'SSE: reconnecting';
    } else {
        sseStatusEl.classList.add('disconnected');
        sseStatusEl.title = 'SSE: disconnected';
    }
}

loadTabs().then(() => {
    initSSE((msg) => {
        // Dispatch message to all active modules or relevant module
        Object.values(tabsModules).forEach(module => {
            if (module.onMessage) module.onMessage(msg);
        });
    }, (state) => {
        setSseStatus(state);
    });

    setupTabKeyboardNavigation();
});
