import {initSSE} from './vendor/sse.js';

const tabsNav = document.getElementById('tabs-nav');
const tabsContent = document.getElementById('tabs-content');

let activeTabId = null;
const tabsModules = {};

async function loadTabs() {
    const response = await fetch('/api/tabs');
    const tabs = await response.json();

    // Sort tabs: dashboard first, then alphabetical by title
    tabs.sort((a, b) => {
        if (a.id === 'dashboard') return -1;
        if (b.id === 'dashboard') return 1;
        return a.title.localeCompare(b.title);
    });

    tabs.forEach(tab => {
        // Create tab link
        const link = document.createElement('div');
        link.className = 'tab-link';
        link.textContent = tab.title;
        link.dataset.tabId = tab.id;
        link.onclick = () => switchTab(tab.id);
        tabsNav.appendChild(link);

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
            e.preventDefault();
            if (focusOnTabs) {
                // Move focus to next tab (don't switch yet)
                if (focusedTabIndex < allTabIds.length - 1) {
                    focusedTabIndex++;
                    updateTabVisualFocus(focusedTabIndex);
                }
            }
        } else if (e.key === 'ArrowLeft') {
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
            if (focusOnTabs && (activeTabId === 'alerts' || activeTabId === 'errors')) {
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
            if (!focusOnTabs && (activeTabId === 'alerts' || activeTabId === 'errors')) {
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

const sseStatusEl = document.getElementById('sse-status');

function setSseStatus(state) {
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

initSSE((msg) => {
    // Dispatch message to all active modules or relevant module
    Object.values(tabsModules).forEach(module => {
        if (module.onMessage) module.onMessage(msg);
    });
}, (state) => {
    setSseStatus(state);
});

loadTabs();
setupTabKeyboardNavigation();
