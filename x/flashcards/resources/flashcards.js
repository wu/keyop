import {ServiceFilterNav} from '/js/service-filter-nav.js';

let container = null;
let navController = null;
let currentTag = 'all';
let allCards = [];
let expandedCardId = null;

export async function init(c) {
    container = c;
    await refresh();
    setupNewCardButton();
    setupModalKeyboard();
}

export function onMessage(_msg) {
    // No live push events for flashcards currently
}

export function focusItems() {
    if (navController) navController.focusOnItems();
}

export function canReturnToTabs() {
    return navController && navController.canReturnFocus();
}

export function updateBubble() {
    const tabLink = document.querySelector('[data-tab-id="flashcards"]');
    if (!tabLink) return;
    const existing = tabLink.querySelector('.tab-badge');
    if (existing) existing.remove();

    const due = allCards.length;
    if (due > 0) {
        const badge = document.createElement('span');
        badge.className = 'tab-badge';
        badge.textContent = due;
        badge.style.backgroundColor = '#8a3fd3';
        tabLink.appendChild(badge);
    }
}

// ── Data loading ──────────────────────────────────────────────────────────────

async function refresh() {
    await Promise.all([loadTags(), loadCards(currentTag)]);
    setupNav();
    updateBubble();
}

async function loadCards(tag) {
    try {
        const res = await fetch('/api/tabs/flashcards/action/list-due', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({tag}),
        });
        const data = await res.json();
        allCards = data.cards || [];
        await renderCards(allCards);
    } catch (err) {
        console.error('[flashcards] Failed to load cards:', err);
    }
}

async function loadTags() {
    try {
        const res = await fetch('/api/tabs/flashcards/action/list-tags', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({}),
        });
        const data = await res.json();
        renderTags(data.tags || [], data.allDue ?? 0);
    } catch (err) {
        console.error('[flashcards] Failed to load tags:', err);
    }
}

// ── Rendering ─────────────────────────────────────────────────────────────────

function renderTags(tags, allDue) {
    const list = container.querySelector('.tag-list');
    if (!list) return;

    const allItem = list.querySelector('[data-tag="all"]');
    if (allItem) {
        const countEl = allItem.querySelector('.service-count');
        if (countEl) countEl.textContent = allDue;
    }

    // Remove old tag items (keep "all")
    list.querySelectorAll('[data-tag]:not([data-tag="all"])').forEach(el => el.remove());

    tags.sort((a, b) => a.tag.localeCompare(b.tag)).forEach(info => {
        const el = document.createElement('div');
        el.className = 'service-item tag-item';
        el.dataset.tag = info.tag;
        if (info.tag === currentTag) el.classList.add('active');
        el.innerHTML = `<span class="tag-label">${escapeHtml(info.tag)}</span><span class="service-count">${info.due}</span>`;
        el.addEventListener('click', () => selectTag(info.tag));
        list.appendChild(el);
    });

    // Update "all" active state
    if (allItem) {
        allItem.classList.toggle('active', currentTag === 'all');
        allItem.addEventListener('click', () => selectTag('all'));
    }
}

// ── Markdown rendering ────────────────────────────────────────────────────────

const _mdCache = new Map();

async function renderMarkdown(text) {
    if (!text) return '';
    if (_mdCache.has(text)) return _mdCache.get(text);
    try {
        const res = await fetch('/api/tabs/flashcards/action/render-markdown', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({content: text}),
        });
        const data = await res.json();
        const html = (data && data.html) ? data.html : escapeHtml(text);
        _mdCache.set(text, html);
        return html;
    } catch (_) {
        return escapeHtml(text);
    }
}

// ── Card list rendering ───────────────────────────────────────────────────────

async function renderCards(cards) {
    const listDiv = container.querySelector('#fc-list');
    if (!listDiv) return;

    if (cards.length === 0) {
        listDiv.innerHTML = '<div class="fc-empty">No cards due</div>';
        return;
    }

    listDiv.innerHTML = '';
    for (const card of cards) {
        const item = document.createElement('div');
        item.className = 'fc-item alert-item';
        item.dataset.cardId = card.id;
        item.dataset.serviceName = card.tags; // for ServiceFilterNav compat

        const isExpanded = card.id === expandedCardId;
        item.innerHTML = buildCardShell(card, isExpanded);
        attachCardListeners(item, card);

        listDiv.appendChild(item);

        // Render question as plain text in the list view
        const qEl = item.querySelector('.fc-question-text');
        if (qEl) qEl.innerHTML = escapeHtml(card.question);

        // Render answer markdown only when card is expanded
        if (isExpanded) {
            const aEl = item.querySelector('.fc-answer-text');
            if (aEl) aEl.innerHTML = await renderMarkdown(card.answer);
            loadSchedulePreviews(card.id);
        }
    }
}

function attachCardListeners(item, card) {
    item.addEventListener('click', (e) => {
        if (e.target.closest('.fc-rating-btn') || e.target.closest('.fc-delete-btn') || e.target.closest('.fc-edit-btn')) return;
        toggleCard(card.id);
    });

    item.querySelectorAll('.fc-rating-btn').forEach(btn => {
        btn.addEventListener('click', (e) => {
            e.stopPropagation();
            handleRating(card.id, btn.dataset.rating);
        });
    });

    const editBtn = item.querySelector('.fc-edit-btn');
    if (editBtn) {
        editBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            openEditModal(card);
        });
    }

    const delBtn = item.querySelector('.fc-delete-btn');
    if (delBtn) {
        delBtn.addEventListener('click', (e) => {
            e.stopPropagation();
            handleDelete(card.id);
        });
    }
}

function buildCardShell(card, expanded) {
    const tagsHtml = card.tags
        ? card.tags.split(',').filter(Boolean).map(t =>
            `<span class="fc-tag">${escapeHtml(t.trim())}</span>`).join('')
        : '';

    const dueLabel = card.interval > 0
        ? `<span class="fc-due">interval: ${card.interval}d</span>`
        : `<span class="fc-due fc-due-new">new</span>`;

    const answerHtml = expanded ? `
        <div class="fc-answer">
            <div class="fc-ratings">
                <button class="fc-rating-btn fc-again" data-rating="show_again">
                    <span class="fc-btn-label">Show Again</span>
                    <span class="fc-btn-time"></span>
                </button>
                <button class="fc-rating-btn fc-hard" data-rating="hard">
                    <span class="fc-btn-label">Hard</span>
                    <span class="fc-btn-time"></span>
                </button>
                <button class="fc-rating-btn fc-correct" data-rating="correct">
                    <span class="fc-btn-label">Correct</span>
                    <span class="fc-btn-time"></span>
                </button>
                <button class="fc-rating-btn fc-easy" data-rating="easy">
                    <span class="fc-btn-label">Easy</span>
                    <span class="fc-btn-time"></span>
                </button>
            </div>
            <div class="fc-answer-label">
                Answer
                <button class="fc-edit-btn" title="Edit card">✎ Edit</button>
            </div>
            <div class="fc-answer-text fc-md"></div>
        </div>` : '';

    return `
        <div class="fc-item-header">
            <div class="fc-question-text fc-md"></div>
            <div class="fc-item-meta">
                <div class="fc-tags">${tagsHtml}</div>
                ${dueLabel}
                <button class="fc-delete-btn" title="Delete card">✕</button>
            </div>
        </div>
        ${answerHtml}`;
}


async function toggleCard(cardId) {
    expandedCardId = expandedCardId === cardId ? null : cardId;
    await renderCards(allCards);
    setupNav();
}

function relativeTime(isoStr) {
    const diffMs = new Date(isoStr) - Date.now();
    if (diffMs <= 0) return 'now';
    const totalSec = Math.round(diffMs / 1000);
    const parts = [
        {unit: 'd', val: Math.floor(totalSec / 86400)},
        {unit: 'h', val: Math.floor((totalSec % 86400) / 3600)},
        {unit: 'm', val: Math.floor((totalSec % 3600) / 60)},
        {unit: 's', val: totalSec % 60},
    ].filter(p => p.val > 0).slice(0, 2);
    return parts.length ? parts.map(p => p.val + p.unit).join(' ') : 'now';
}

async function loadSchedulePreviews(cardId) {
    try {
        const data = await fetch('/api/tabs/flashcards/action/preview-schedule', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id: cardId}),
        }).then(r => r.json());
        const item = container.querySelector(`[data-card-id="${cardId}"]`);
        if (!item) return;
        item.querySelectorAll('.fc-rating-btn').forEach(btn => {
            const rating = btn.dataset.rating;
            const timeEl = btn.querySelector('.fc-btn-time');
            if (timeEl && data[rating]) {
                timeEl.textContent = relativeTime(data[rating]);
            }
        });
    } catch (err) {
        console.error('[flashcards] preview-schedule failed:', err);
    }
}

async function handleRating(cardId, rating) {
    try {
        await fetch('/api/tabs/flashcards/action/review', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id: cardId, rating}),
        });
        expandedCardId = null;
        await refresh();
    } catch (err) {
        console.error('[flashcards] Review failed:', err);
    }
}

async function handleDelete(cardId) {
    if (!confirm('Delete this flashcard?')) return;
    try {
        await fetch('/api/tabs/flashcards/action/delete-card', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id: cardId}),
        });
        expandedCardId = null;
        await refresh();
    } catch (err) {
        console.error('[flashcards] Delete failed:', err);
    }
}

// ── Edit modal ────────────────────────────────────────────────────────────────

let _editCardId = null;

function openEditModal(card) {
    _editCardId = card.id;
    container.querySelector('#fc-edit-question').value = card.question;
    container.querySelector('#fc-edit-answer').value = card.answer;
    container.querySelector('#fc-edit-tags').value = card.tags || '';
    container.querySelector('#fc-edit-modal').style.display = 'flex';
    container.querySelector('#fc-edit-question').focus();
}

function closeEditModal() {
    _editCardId = null;
    container.querySelector('#fc-edit-modal').style.display = 'none';
}

async function handleEditSave() {
    const id = _editCardId;
    if (!id) return;
    const question = container.querySelector('#fc-edit-question').value.trim();
    const answer = container.querySelector('#fc-edit-answer').value.trim();
    const tags = container.querySelector('#fc-edit-tags').value.trim();
    if (!question) {
        container.querySelector('#fc-edit-question').focus();
        return;
    }
    try {
        await fetch('/api/tabs/flashcards/action/update-card', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({id, question, answer, tags}),
        });
        // Invalidate cached markdown for old content
        _mdCache.delete(allCards.find(c => c.id === id)?.question || '');
        _mdCache.delete(allCards.find(c => c.id === id)?.answer || '');
        closeEditModal();
        await refresh();
    } catch (err) {
        console.error('[flashcards] Edit save failed:', err);
    }
}

async function selectTag(tag) {
    currentTag = tag;
    container.querySelectorAll('.service-item[data-tag]').forEach(el => {
        el.classList.toggle('active', el.dataset.tag === tag);
    });
    await loadCards(tag);
    setupNav();
}

// ── New card modal ────────────────────────────────────────────────────────────

function setupNewCardButton() {
    const btn = container.querySelector('#fc-new-btn');
    const modal = container.querySelector('#fc-modal');
    const saveBtn = container.querySelector('#fc-save-btn');
    const cancelBtn = container.querySelector('#fc-cancel-btn');

    if (!btn || !modal) return;

    btn.addEventListener('click', () => openModal());
    cancelBtn.addEventListener('click', () => closeModal());
    modal.addEventListener('click', (e) => {
        if (e.target === modal) closeModal();
    });
    saveBtn.addEventListener('click', () => saveCard());

    const editModal = container.querySelector('#fc-edit-modal');
    const editSaveBtn = container.querySelector('#fc-edit-save-btn');
    const editCancelBtn = container.querySelector('#fc-edit-cancel-btn');
    if (editModal) {
        editSaveBtn.addEventListener('click', () => handleEditSave());
        editCancelBtn.addEventListener('click', () => closeEditModal());
        editModal.addEventListener('click', (e) => {
            if (e.target === editModal) closeEditModal();
        });
    }
}

function setupModalKeyboard() {
    document.addEventListener('keydown', (e) => {
        const modal = container?.querySelector('#fc-modal');
        if (modal && modal.style.display !== 'none') {
            if (e.key === 'Escape') closeModal();
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) saveCard();
            return;
        }
        const editModal = container?.querySelector('#fc-edit-modal');
        if (editModal && editModal.style.display !== 'none') {
            if (e.key === 'Escape') closeEditModal();
            if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) handleEditSave();
        }
    });
}

function openModal() {
    const modal = container.querySelector('#fc-modal');
    if (!modal) return;
    container.querySelector('#fc-question').value = '';
    container.querySelector('#fc-answer').value = '';
    container.querySelector('#fc-tags').value = '';
    modal.style.display = 'flex';
    container.querySelector('#fc-question').focus();
}

function closeModal() {
    const modal = container.querySelector('#fc-modal');
    if (modal) modal.style.display = 'none';
}

async function saveCard() {
    const question = container.querySelector('#fc-question').value.trim();
    const answer = container.querySelector('#fc-answer').value.trim();
    const tags = container.querySelector('#fc-tags').value.trim();

    if (!question) {
        container.querySelector('#fc-question').focus();
        return;
    }

    try {
        await fetch('/api/tabs/flashcards/action/create-card', {
            method: 'POST',
            headers: {'Content-Type': 'application/json'},
            body: JSON.stringify({question, answer, tags}),
        });
        closeModal();
        await refresh();
    } catch (err) {
        console.error('[flashcards] Save failed:', err);
    }
}

// ── Keyboard nav ─────────────────────────────────────────────────────────────

function setupNav() {
    navController = new ServiceFilterNav({
        container,
        itemSelector: '.fc-item',
        serviceSelector: '.service-item[data-tag]',
        selectedClass: 'alert-selected',
        markedClass: 'fc-marked',
        markItemCallback: async () => {
        },
        onStateChange: () => {
        },
    });
}

// ── Utilities ─────────────────────────────────────────────────────────────────

function escapeHtml(text) {
    if (text == null) return '';
    return String(text)
        .replace(/&/g, '&amp;')
        .replace(/</g, '&lt;')
        .replace(/>/g, '&gt;')
        .replace(/"/g, '&quot;')
        .replace(/'/g, '&#39;');
}
