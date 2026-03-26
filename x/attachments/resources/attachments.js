let attachmentsContainer = null;
let pendingFile = null; // File object waiting for name confirmation
let currentView = localStorage.getItem('att-view') || 'list'; // 'list' or 'icon'

import {formatElapsedTime} from '/js/time-formatter.js';

export async function init(container) {
    attachmentsContainer = container;
    injectCSS();
    renderShell();
    await refreshFiles();
    setupDragDrop();
}

export function onMessage() {
    // No SSE handling needed for attachments
}

// ── CSS injection ─────────────────────────────────────────────────────────────

function injectCSS() {
    if (document.querySelector('link[data-att-css]')) return;
    const link = document.createElement('link');
    link.rel = 'stylesheet';
    link.href = '/api/assets/attachments/attachments.css';
    link.setAttribute('data-att-css', '1');
    document.head.appendChild(link);
}

// ── Shell ─────────────────────────────────────────────────────────────────────

function renderShell() {
    attachmentsContainer.innerHTML = `
        <div class="attachments-layout">
          <div class="attachments-content">
            <div class="attachments-header">
              <h2>Attachments</h2>
              <div class="att-view-toggle">
                <button class="att-view-btn${currentView === 'list' ? ' att-view-btn-active' : ''}" id="att-view-list" title="List view">☰</button>
                <button class="att-view-btn${currentView === 'icon' ? ' att-view-btn-active' : ''}" id="att-view-icon" title="Icon view">⊞</button>
              </div>
              <button class="att-upload-btn" id="att-upload-btn">＋ Upload</button>
              <input type="file" id="att-file-input" style="display:none" multiple>
            </div>
            <div id="attachments-list"></div>
          </div>
        </div>
        <!-- Upload filename modal -->
        <div class="att-modal" id="att-modal" style="display:none">
          <div class="att-modal-box">
            <h3>Upload file</h3>
            <label>Filename on server</label>
            <input type="text" id="att-filename-input" autocomplete="off" spellcheck="false">
            <div class="att-modal-hint">
              Allowed characters: a–z, A–Z, 0–9, _ . (other characters are replaced with _)
            </div>
            <div id="att-upload-progress" class="att-progress" style="display:none"></div>
            <div class="att-modal-actions">
              <button class="att-btn" id="att-cancel-btn">Cancel</button>
              <button class="att-btn att-btn-primary" id="att-confirm-btn">Upload</button>
            </div>
          </div>
        </div>
    `;

    document.getElementById('att-upload-btn').addEventListener('click', () => {
        document.getElementById('att-file-input').click();
    });

    document.getElementById('att-file-input').addEventListener('change', (e) => {
        if (e.target.files.length > 0) promptFilename(e.target.files[0]);
        e.target.value = ''; // reset so same file can be re-picked
    });

    document.getElementById('att-cancel-btn').addEventListener('click', closeModal);
    document.getElementById('att-confirm-btn').addEventListener('click', confirmUpload);
    document.getElementById('att-modal').addEventListener('click', (e) => {
        if (e.target === document.getElementById('att-modal')) closeModal();
    });
    document.getElementById('att-filename-input').addEventListener('keydown', (e) => {
        if (e.key === 'Enter') confirmUpload();
        if (e.key === 'Escape') closeModal();
    });
    // Live-sanitize as user types
    document.getElementById('att-filename-input').addEventListener('input', (e) => {
        const pos = e.target.selectionStart;
        const sanitized = sanitizeDisplay(e.target.value);
        if (sanitized !== e.target.value) {
            e.target.value = sanitized;
            e.target.setSelectionRange(pos, pos);
        }
    });

    document.getElementById('att-view-list').addEventListener('click', () => setView('list'));
    document.getElementById('att-view-icon').addEventListener('click', () => setView('icon'));
}

// ── View toggle ───────────────────────────────────────────────────────────────

function setView(view) {
    currentView = view;
    localStorage.setItem('att-view', view);
    const listBtn = document.getElementById('att-view-list');
    const iconBtn = document.getElementById('att-view-icon');
    if (listBtn) listBtn.classList.toggle('att-view-btn-active', view === 'list');
    if (iconBtn) iconBtn.classList.toggle('att-view-btn-active', view === 'icon');
    // Re-render with cached files (trigger a refresh)
    refreshFiles();
}

// ── Filename sanitization ─────────────────────────────────────────────────────

// Mirrors server-side sanitizeFilename: replaces non-[a-zA-Z0-9_.] in the base,
// keeps the extension with only alphanumeric chars.
function sanitizeDisplay(name) {
    if (!name) return '';
    const lastDot = name.lastIndexOf('.');
    if (lastDot > 0) {
        const base = name.slice(0, lastDot).replace(/[^a-zA-Z0-9_.]/g, '_');
        const ext = name.slice(lastDot + 1).replace(/[^a-zA-Z0-9]/g, '_');
        return ext ? base + '.' + ext : base;
    }
    return name.replace(/[^a-zA-Z0-9_.]/g, '_');
}

// ── Upload modal ──────────────────────────────────────────────────────────────

function promptFilename(file) {
    pendingFile = file;
    const input = document.getElementById('att-filename-input');
    input.value = sanitizeDisplay(file.name);
    document.getElementById('att-upload-progress').style.display = 'none';
    document.getElementById('att-modal').style.display = 'flex';
    setTimeout(() => {
        input.focus();
        // Select just the base name (before extension) for easy editing
        const lastDot = input.value.lastIndexOf('.');
        input.setSelectionRange(0, lastDot > 0 ? lastDot : input.value.length);
    }, 50);
}

function closeModal() {
    pendingFile = null;
    document.getElementById('att-modal').style.display = 'none';
    document.getElementById('att-upload-progress').style.display = 'none';
    const confirmBtn = document.getElementById('att-confirm-btn');
    if (confirmBtn) confirmBtn.disabled = false;
}

async function confirmUpload() {
    if (!pendingFile) return;
    const file = pendingFile;
    const filename = sanitizeDisplay(document.getElementById('att-filename-input').value.trim()) || sanitizeDisplay(file.name);

    const progress = document.getElementById('att-upload-progress');
    progress.textContent = 'Uploading…';
    progress.style.display = 'block';
    document.getElementById('att-confirm-btn').disabled = true;

    try {
        const form = new FormData();
        form.append('file', file, file.name);
        form.append('filename', filename);

        const resp = await fetch('/api/attachments/upload', {
            method: 'POST',
            body: form,
        });

        if (!resp.ok) {
            const text = await resp.text();
            progress.textContent = 'Upload failed: ' + text;
            document.getElementById('att-confirm-btn').disabled = false;
            return;
        }

        closeModal();
        await refreshFiles();
    } catch (err) {
        console.error('[attachments] upload error:', err);
        progress.textContent = 'Upload failed: ' + err.message;
        document.getElementById('att-confirm-btn').disabled = false;
    }
}

// ── Drag and drop ─────────────────────────────────────────────────────────────

function setupDragDrop() {
    const container = attachmentsContainer;
    let dragCounter = 0;

    container.addEventListener('dragenter', (e) => {
        e.preventDefault();
        dragCounter++;
        container.classList.add('att-dropzone-active');
    });
    container.addEventListener('dragleave', () => {
        dragCounter--;
        if (dragCounter <= 0) {
            dragCounter = 0;
            container.classList.remove('att-dropzone-active');
        }
    });
    container.addEventListener('dragover', (e) => e.preventDefault());
    container.addEventListener('drop', (e) => {
        e.preventDefault();
        dragCounter = 0;
        container.classList.remove('att-dropzone-active');
        const files = e.dataTransfer?.files;
        if (files && files.length > 0) promptFilename(files[0]);
    });
}

// ── File list ─────────────────────────────────────────────────────────────────

async function refreshFiles() {
    if (!attachmentsContainer) return;
    try {
        const resp = await fetch('/api/tabs/attachments/action/list-files', {method: 'POST'});
        if (!resp.ok) return;
        const data = await resp.json();
        const files = data.files || [];
        if (currentView === 'icon') {
            renderIconView(files);
        } else {
            renderFileList(files);
        }
    } catch (err) {
        console.error('[attachments] list error:', err);
    }
}

function renderFileList(files) {
    const list = attachmentsContainer?.querySelector('#attachments-list');
    if (!list) return;

    if (files.length === 0) {
        list.innerHTML = '<div class="att-empty">No attachments yet. Click "＋ Upload" or drag a file here.</div>';
        return;
    }

    const rows = files.map(f => {
        const sizeStr = formatSize(f.size);
        const dateStr = formatElapsedTime(f.uploadedAt);
        const originalNote = f.originalFilename !== f.storedFilename
            ? `<div class="att-original-name" title="Original: ${escHtml(f.originalFilename)}">← ${escHtml(f.originalFilename)}</div>`
            : '';
        return `
            <tr>
                <td>
                    <a class="att-filename-link" href="/api/attachments/file/${f.id}" target="_blank" rel="noopener">${escHtml(f.storedFilename)}</a>
                    ${originalNote}
                </td>
                <td class="att-mime">${escHtml(f.mimeType || '—')}</td>
                <td class="att-size">${sizeStr}</td>
                <td class="att-date" title="${escHtml(f.uploadedAt)}">${dateStr}</td>
                <td><button class="att-delete-btn" data-id="${f.id}" title="Delete">✕</button></td>
            </tr>
        `;
    }).join('');

    list.innerHTML = `
        <table class="att-table">
            <thead>
                <tr>
                    <th>Filename</th>
                    <th>Type</th>
                    <th>Size</th>
                    <th>Uploaded</th>
                    <th></th>
                </tr>
            </thead>
            <tbody>${rows}</tbody>
        </table>
    `;

    list.querySelectorAll('.att-delete-btn').forEach(btn => {
        btn.addEventListener('click', () => handleDelete(btn.dataset.id));
    });
}

function renderIconView(files) {
    const list = attachmentsContainer?.querySelector('#attachments-list');
    if (!list) return;

    if (files.length === 0) {
        list.innerHTML = '<div class="att-empty">No attachments yet. Click "＋ Upload" or drag a file here.</div>';
        return;
    }

    const cards = files.map(f => {
        const isImage = f.mimeType && f.mimeType.startsWith('image/');
        const previewHTML = isImage
            ? `<img class="att-icon-img" src="/api/attachments/preview/${f.id}" alt="${escHtml(f.storedFilename)}" loading="lazy">`
            : `<div class="att-icon-placeholder">${mimeIcon(f.mimeType)}</div>`;
        const sizeStr = formatSize(f.size);
        return `
            <div class="att-icon-card" data-id="${f.id}">
                <a class="att-icon-preview" href="/api/attachments/file/${f.id}" target="_blank" rel="noopener" title="${escHtml(f.storedFilename)}">
                    ${previewHTML}
                </a>
                <div class="att-icon-footer">
                    <a class="att-icon-name" href="/api/attachments/file/${f.id}" target="_blank" rel="noopener" title="${escHtml(f.storedFilename)}">${escHtml(f.storedFilename)}</a>
                    <div class="att-icon-meta">${sizeStr}</div>
                </div>
                <button class="att-icon-delete" data-id="${f.id}" title="Delete">✕</button>
            </div>
        `;
    }).join('');

    list.innerHTML = `<div class="att-icon-grid">${cards}</div>`;

    list.querySelectorAll('.att-icon-delete').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.preventDefault();
            e.stopPropagation();
            handleDelete(btn.dataset.id);
        });
    });
}

// Returns an emoji icon for a given MIME type.
function mimeIcon(mimeType) {
    if (!mimeType) return '📎';
    if (mimeType === 'application/pdf') return '📄';
    if (mimeType.startsWith('video/')) return '🎬';
    if (mimeType.startsWith('audio/')) return '🎵';
    if (mimeType.startsWith('text/')) return '📝';
    if (mimeType.includes('zip') || mimeType.includes('compressed') || mimeType.includes('tar') || mimeType.includes('gzip')) return '📦';
    if (mimeType.includes('spreadsheet') || mimeType.includes('excel') || mimeType.includes('csv')) return '📊';
    if (mimeType.includes('word') || mimeType.includes('document')) return '📝';
    if (mimeType.includes('presentation') || mimeType.includes('powerpoint')) return '📊';
    return '📎';
}

async function handleDelete(id) {
    if (!confirm('Delete this attachment?')) return;
    try {
        const resp = await fetch('/api/tabs/attachments/action/delete-file', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id}),
        });
        if (resp.ok) await refreshFiles();
    } catch (err) {
        console.error('[attachments] delete error:', err);
    }
}

// ── Helpers ───────────────────────────────────────────────────────────────────

function formatSize(bytes) {
    if (bytes < 1024) return bytes + ' B';
    if (bytes < 1024 * 1024) return (bytes / 1024).toFixed(1) + ' KB';
    return (bytes / (1024 * 1024)).toFixed(1) + ' MB';
}

function escHtml(str) {
    return String(str)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;');
}
