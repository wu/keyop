import {initSSE} from './vendor/sse.js';

const tabsNav = document.getElementById('tabs-nav');
const tabsContent = document.getElementById('tabs-content');

let activeTabId = null;
const tabsModules = {};

async function loadTabs() {
    const response = await fetch('/api/tabs');
    const tabs = await response.json();

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
        switchTab(tabs[0].id);
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
}

initSSE((msg) => {
    // Dispatch message to all active modules or relevant module
    Object.values(tabsModules).forEach(module => {
        if (module.onMessage) module.onMessage(msg);
    });
});

loadTabs();
