let panelsContainer = null;
const panelDefs = {};
const panelModules = {};

async function refreshPanels() {
    if (!panelsContainer) return;
    try {
        const res = await fetch('/api/panels');
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const list = await res.json();

        // Load saved order from state
        const orderRes = await fetch('/api/dashboard/panel-order');
        const orderData = orderRes.ok ? await orderRes.json() : {order: []};
        const savedOrder = orderData.order || [];

        // Sort panels by saved order, then by original order
        const panelsByOrder = list.map((p, idx) => ({
            panel: p,
            originalIdx: idx,
            savedIdx: savedOrder.indexOf(p.id)
        }));
        panelsByOrder.sort((a, b) => {
            const aIdx = a.savedIdx >= 0 ? a.savedIdx : a.originalIdx + 10000;
            const bIdx = b.savedIdx >= 0 ? b.savedIdx : b.originalIdx + 10000;
            return aIdx - bIdx;
        });

        // Clear existing
        panelsContainer.innerHTML = '';
        Object.keys(panelDefs).forEach(k => delete panelDefs[k]);
        Object.keys(panelModules).forEach(k => delete panelModules[k]);

        if (!Array.isArray(list) || list.length === 0) {
            panelsContainer.textContent = 'No panels available';
            return;
        }

        for (const {panel: p} of panelsByOrder) {
            const wrapper = document.createElement('div');
            wrapper.className = 'dashboard-panel';
            wrapper.id = `panel-${p.id}`;
            wrapper.draggable = true;
            wrapper.innerHTML = p.content || `<div class="panel"><div class="panel-title">${p.title}</div><div class="panel-body">Loading…</div></div>`;

            // Add click handler to navigate to tab if one exists
            wrapper.addEventListener('click', (e) => {
                // Don't navigate if user is dragging or if click was on a dragged element
                if (draggedElement !== null) return;
                // Navigate to the tab with the same ID as the panel
                if (window.switchTab) {
                    window.switchTab(p.id);
                }
            });

            // Add drag-drop event listeners
            wrapper.addEventListener('dragstart', handleDragStart);
            wrapper.addEventListener('dragover', handleDragOver);
            wrapper.addEventListener('drop', handleDrop);
            wrapper.addEventListener('dragend', handleDragEnd);
            
            panelsContainer.appendChild(wrapper);

            panelDefs[p.id] = p;

            if (p.jsPath) {
                import(p.jsPath).then(mod => {
                    panelModules[p.id] = mod;
                    if (mod.init) {
                        try {
                            mod.init(wrapper);
                        } catch (e) {
                            console.error('panel init error', e);
                        }
                    }
                }).catch(err => console.error('Failed to load panel module', p.jsPath, err));
            }
        }
    } catch (err) {
        console.error('Failed to load panels', err);
        panelsContainer.textContent = 'Failed to load panels';
    }
}

let draggedElement = null;

function handleDragStart(e) {
    draggedElement = this;
    e.dataTransfer.effectAllowed = 'move';
    this.style.opacity = '0.5';
}

function handleDragOver(e) {
    if (e.preventDefault) {
        e.preventDefault();
    }
    e.dataTransfer.dropEffect = 'move';

    if (this !== draggedElement && this.classList.contains('dashboard-panel')) {
        const rect = this.getBoundingClientRect();
        const midpoint = rect.left + rect.width / 2;

        // Show border on the side where drop will occur
        if (e.clientX < midpoint) {
            this.style.borderLeft = '3px solid var(--accent)';
            this.style.borderRight = '';
        } else {
            this.style.borderRight = '3px solid var(--accent)';
            this.style.borderLeft = '';
        }
    }
    return false;
}

function handleDrop(e) {
    if (e.stopPropagation) {
        e.stopPropagation();
    }

    if (draggedElement !== this && this.classList.contains('dashboard-panel')) {
        const rect = this.getBoundingClientRect();
        const midpoint = rect.left + rect.width / 2;

        // Determine if dropping to the left or right
        if (e.clientX < midpoint) {
            // Insert before this panel
            this.parentNode.insertBefore(draggedElement, this);
        } else {
            // Insert after this panel
            this.parentNode.insertBefore(draggedElement, this.nextSibling);
        }

        savePanelOrder();
    }

    return false;
}

function handleDragEnd(e) {
    draggedElement.style.opacity = '1';

    // Clear visual feedback on all panels
    const panels = panelsContainer.querySelectorAll('.dashboard-panel');
    panels.forEach(p => {
        p.style.borderLeft = '';
        p.style.borderRight = '';
    });

    // Clear the dragged element reference
    draggedElement = null;
}

function savePanelOrder() {
    const panels = panelsContainer.querySelectorAll('.dashboard-panel');
    const order = Array.from(panels).map(p => p.id.replace('panel-', ''));

    fetch('/api/dashboard/panel-order', {
        method: 'POST',
        headers: {
            'Content-Type': 'application/json'
        },
        body: JSON.stringify({order})
    }).catch(err => console.error('Failed to save panel order', err));
}

export async function init(el) {
    panelsContainer = el.querySelector('#dashboard-panels');
    if (!panelsContainer) return;
    await refreshPanels();
    // Panels refresh is driven by SSE 'panels_updated' events; initial load already performed.
}

export function onMessage(msg) {
    // If panels list updated, refresh
    if (msg && msg.serviceType === 'webui' && msg.event === 'panels_updated') {
        refreshPanels();
        return;
    }

    // Dispatch to loaded panel modules
    for (const id in panelModules) {
        const mod = panelModules[id];
        if (mod && mod.onMessage) {
            try {
                mod.onMessage(msg);
            } catch (e) {
                console.error('panel module onMessage error', e);
            }
        }
    }

    // For panels without modules that declare an event, update simple content
    for (const id in panelDefs) {
        const def = panelDefs[id];
        if (!def.jsPath && def.event && msg.event === def.event) {
            const el = document.getElementById(`panel-${def.id}`);
            if (el) {
                const body = el.querySelector('.panel-body');
                if (body) {
                    body.textContent = msg.summary || msg.text || '';
                } else {
                    el.textContent = msg.summary || msg.text || '';
                }
            }
        }
    }
}
