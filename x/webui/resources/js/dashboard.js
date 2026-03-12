let panelsContainer = null;
const panelDefs = {};
const panelModules = {};

async function refreshPanels() {
    if (!panelsContainer) return;
    try {
        const res = await fetch('/api/panels');
        if (!res.ok) throw new Error(`HTTP ${res.status}`);
        const list = await res.json();

        // Clear existing
        panelsContainer.innerHTML = '';
        Object.keys(panelDefs).forEach(k => delete panelDefs[k]);
        Object.keys(panelModules).forEach(k => delete panelModules[k]);

        if (!Array.isArray(list) || list.length === 0) {
            panelsContainer.textContent = 'No panels available';
            return;
        }

        for (const p of list) {
            const wrapper = document.createElement('div');
            wrapper.className = 'dashboard-panel';
            wrapper.id = `panel-${p.id}`;
            wrapper.innerHTML = p.content || `<div class="panel"><div class="panel-title">${p.title}</div><div class="panel-body">Loading…</div></div>`;
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
