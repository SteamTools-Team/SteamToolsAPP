

const Toast = (() => {
  const container = () => document.getElementById('toast-container');
  let _id = 0;

  const ICONS = {
    success: `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>`,
    error:   `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--red)" stroke-width="2.5"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>`,
    warning: `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--yellow)" stroke-width="2.5"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>`,
    info:    `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--accent-hover)" stroke-width="2.5"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>`,
  };

  function show(type, title, msg = '', duration = 4000) {
    const id   = ++_id;
    const el   = document.createElement('div');
    el.className = `toast toast--${type}`;
    el.dataset.id = id;
    el.innerHTML = `
      <div class="toast-icon">${ICONS[type] || ICONS.info}</div>
      <div class="toast-content">
        <div class="toast-title">${title}</div>
        ${msg ? `<div class="toast-msg">${msg}</div>` : ''}
      </div>
      <button class="toast-close">×</button>
    `;
    el.querySelector('.toast-close').addEventListener('click', () => dismiss(id));
    container().appendChild(el);

    if (duration > 0) {
      setTimeout(() => dismiss(id), duration);
    }
    return id;
  }

  function dismiss(id) {
    const el = container().querySelector(`[data-id="${id}"]`);
    if (!el) return;
    el.classList.add('removing');
    el.addEventListener('animationend', () => el.remove(), { once: true });
  }

  return {
    success: (title, msg, d) => show('success', title, msg, d),
    error:   (title, msg, d) => show('error',   title, msg, d),
    warning: (title, msg, d) => show('warning', title, msg, d),
    info:    (title, msg, d) => show('info',    title, msg, d),
    dismiss,
  };
})();

const Modal = (() => {
  let _resolve = null;

  const overlay = () => document.getElementById('modal-overlay');
  const dialog  = () => document.getElementById('modal-dialog');
  const title   = () => document.getElementById('modal-title');
  const body    = () => document.getElementById('modal-body');
  const footer  = () => document.getElementById('modal-footer');

  function open(opts = {}) {
    const {
      titleText   = 'Confirm',
      bodyHtml    = '',
      confirmText = 'Confirm',
      cancelText  = 'Cancel',
      danger      = false,
      noFooter    = false,
    } = opts;

    title().textContent = titleText;
    body().innerHTML    = bodyHtml;

    if (noFooter) {
      footer().innerHTML = '';
    } else {
      footer().innerHTML = `
        <button class="btn btn-ghost" id="modal-cancel">${cancelText}</button>
        <button class="btn ${danger ? 'btn-danger' : 'btn-primary'}" id="modal-confirm">${confirmText}</button>
      `;
      document.getElementById('modal-confirm').addEventListener('click', () => close(true));
      document.getElementById('modal-cancel').addEventListener('click',  () => close(false));
    }
    document.getElementById('modal-close').addEventListener('click',   () => close(false));

    overlay().classList.add('active');

    return new Promise(res => { _resolve = res; });
  }

  function close(result) {
    overlay().classList.remove('active');
    if (_resolve) { _resolve(result); _resolve = null; }
  }

  function alert(titleText, bodyHtml) {
    return open({ titleText, bodyHtml, cancelText: null, confirmText: 'OK', danger: false });
  }

  overlay()?.addEventListener('click', e => {
    if (e.target === overlay()) close(false);
  });

  return { open, close, alert };
})();

function appendLog(containerId, msg, type = 'info') {
  const container = document.getElementById(containerId);
  if (!container) return;
  const time = new Date().toLocaleTimeString('en-US', { hour12: false });
  const el   = document.createElement('div');
  el.className = `log-entry log-entry--${type}`;
  el.innerHTML = `<span class="log-time">${time}</span><span class="log-msg">${escapeHtml(msg)}</span>`;
  container.appendChild(el);
  container.scrollTop = container.scrollHeight;
}

function setProgress(barId, pct, labelId, labelText) {
  const bar = document.getElementById(barId);
  if (bar) bar.style.width = `${Math.min(100, Math.max(0, pct))}%`;
  if (labelId) {
    const lbl = document.getElementById(labelId);
    if (lbl) lbl.textContent = labelText || `${pct}%`;
  }
}

function renderInstallSteps(containerId, steps) {
  const container = document.getElementById(containerId);
  if (!container) return;
  container.innerHTML = steps.map((s, i) => `
    <div class="install-step" id="step-${s.id}">
      <div class="step-icon">
        <span class="step-number">${i + 1}</span>
      </div>
      <span class="step-label">${s.label}</span>
      <span class="step-status" id="step-status-${s.id}">Pending</span>
    </div>
  `).join('');
}

function updateInstallStep(stepId, status) {
  const el         = document.getElementById(`step-${stepId}`);
  const statusEl   = document.getElementById(`step-status-${stepId}`);
  if (!el) return;

  el.className = `install-step install-step--${status}`;

  const iconMap = {
    active: `<div class="spinner spinner--sm"></div>`,
    done:   `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>`,
    error:  `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="var(--red)" stroke-width="3"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`,
    pending:`<span class="step-number" style="color:var(--text-3)">${el.querySelector('.step-number, .step-icon')?.textContent || ''}</span>`,
  };

  el.querySelector('.step-icon').innerHTML = status === 'done'
    ? `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="3"><polyline points="20 6 9 17 4 12"/></svg>`
    : status === 'error'
    ? `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="var(--red)" stroke-width="3"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`
    : status === 'active'
    ? `<div class="spinner spinner--sm"></div>`
    : `<span class="step-number" style="font-size:0.65rem;color:var(--text-3)">${el.querySelector('.step-number')?.textContent || ''}</span>`;

  if (statusEl) {
    statusEl.textContent = { active: 'Running...', done: 'Done', error: 'Failed', pending: 'Pending' }[status] || status;
  }
}

function renderRepairSteps(containerId, steps) {
  const container = document.getElementById(containerId);
  if (!container) return;
  container.innerHTML = steps.map(s => `
    <div class="repair-step" id="rstep-${s.id}">
      <div class="repair-step-icon" id="rstep-icon-${s.id}">
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="2">
          <circle cx="12" cy="12" r="4"/>
        </svg>
      </div>
      <span class="repair-step-label">${s.label}</span>
      <span class="repair-step-detail" id="rstep-detail-${s.id}">Waiting</span>
    </div>
  `).join('');
}

function updateRepairStep(stepId, status, detail = '') {
  const el     = document.getElementById(`rstep-${stepId}`);
  const icon   = document.getElementById(`rstep-icon-${stepId}`);
  const detEl  = document.getElementById(`rstep-detail-${stepId}`);
  if (!el) return;

  el.className = `repair-step repair-step--${status}`;

  const icons = {
    active: `<div class="spinner spinner--sm"></div>`,
    done:   `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>`,
    error:  `<svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="var(--red)" stroke-width="2.5"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`,
  };
  if (icon && icons[status]) icon.innerHTML = icons[status];

  if (detEl) {
    detEl.textContent = detail || { active: 'Running...', done: 'Done', error: 'Failed' }[status] || 'Waiting';
  }
}

function _normalizeTs(ts) {
  if (!ts) return 0;
  const n = Number(ts);
  return n < 1e12 ? n * 1000 : n;
}

function formatDate(ts) {
  if (!ts) return '—';
  return new Date(_normalizeTs(ts)).toLocaleDateString('en-US', { month: 'short', day: 'numeric', year: 'numeric' });
}

function formatTime(ts) {
  if (!ts) return '—';
  return new Date(_normalizeTs(ts)).toLocaleTimeString('en-US', { hour: '2-digit', minute: '2-digit' });
}

function timeAgo(ts) {
  if (!ts) return '—';
  const diff = Date.now() - _normalizeTs(ts);
  const m    = Math.floor(diff / 60000);
  const h    = Math.floor(diff / 3600000);
  const d    = Math.floor(diff / 86400000);
  if (diff < 60000)    return 'Just now';
  if (m < 60)          return `${m}m ago`;
  if (h < 24)          return `${h}h ago`;
  return `${d}d ago`;
}

function getImageUrl(appid) {
  const settings = State.getSettings();
  if (settings.image_cache) {
    return `/api/image/${appid}`;
  }
  return `https://shared.akamai.steamstatic.com/store_item_assets/steam/apps/${appid}/header.jpg`;
}

function steamIconError(img) {
  if (!img) return;
  if (img.dataset.fb) {
    const div = img.closest('.recent-install-icon, .game-cell-icon');
    if (div) {
      div.innerHTML = `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" style="width:60%;height:60%;color:var(--text-3)"><rect x="2" y="6" width="20" height="12" rx="2"/><path d="M6 12h4"/><path d="M14 12h4"/><path d="M6 16h4"/><path d="M14 16h4"/></svg>`;
      div.style.display = 'flex';
      div.style.alignItems = 'center';
      div.style.justifyContent = 'center';
      div.style.background = 'var(--surface-2)';
    }
    return;
  }
  img.dataset.fb = '1';
  const appid = img.dataset.appid || '';
  img.src = `https://generator.ryuu.lol/files/images/${appid}.jpg`;
}

function escapeHtml(str) {
  return String(str)
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

function gameInitials(name) {
  return (name || '?')
    .split(/\s+/)
    .slice(0, 2)
    .map(w => w[0])
    .join('')
    .toUpperCase();
}

function downloadJSON(filename, content) {
  const blob = new Blob([content], { type: 'application/json' });
  const url  = URL.createObjectURL(blob);
  const a    = document.createElement('a');
  a.href     = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  document.body.removeChild(a);
  URL.revokeObjectURL(url);
}

function debounce(fn, ms = 250) {
  let t;
  return (...args) => {
    clearTimeout(t);
    t = setTimeout(() => fn(...args), ms);
  };
}

function badgeHtml(text, variant = 'neutral') {
  return `<span class="badge badge--${variant}">${escapeHtml(text)}</span>`;
}

function statusBadgeHtml(ok, textOk = 'OK', textFail = 'Missing') {
  return ok ? badgeHtml(textOk, 'green') : badgeHtml(textFail, 'red');
}

class CustomSelect {
  constructor(selectEl) {
    if (!selectEl) return;
    this._sel = selectEl;
    this._build();
    this._bind();
  }

  _build() {
    const sel = this._sel;
    sel.style.cssText = 'position:absolute;opacity:0;pointer-events:none;width:0;height:0';

    this._wrap = document.createElement('div');
    this._wrap.className = 'cselect';

    this._btn = document.createElement('button');
    this._btn.type = 'button';
    this._btn.className = 'cselect-trigger';

    this._lbl = document.createElement('span');
    this._lbl.className = 'cselect-label';
    this._btn.appendChild(this._lbl);
    this._btn.insertAdjacentHTML('beforeend',
      `<svg class="cselect-arrow" width="11" height="11" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="6 9 12 15 18 9"/></svg>`);

    this._panel = document.createElement('div');
    this._panel.className = 'cselect-panel';

    this._wrap.appendChild(this._btn);
    this._wrap.appendChild(this._panel);

    sel.parentNode.insertBefore(this._wrap, sel);
    this._wrap.appendChild(sel);

    this.refresh();
  }

  refresh() {
    const sel = this._sel;
    if (!sel) return;
    this._lbl.textContent = sel.selectedIndex >= 0 ? sel.options[sel.selectedIndex].text : '';
    this._panel.innerHTML = '';
    for (const opt of sel.options) {
      const div = document.createElement('div');
      div.className = 'cselect-option' + (opt.value === sel.value ? ' active' : '');
      div.dataset.value = opt.value;
      div.textContent = opt.text;
      this._panel.appendChild(div);
    }
  }

  _bind() {
    this._btn.addEventListener('click', e => {
      e.stopPropagation();
      const wasOpen = this._wrap.classList.contains('open');
      document.querySelectorAll('.cselect.open').forEach(el => el.classList.remove('open'));
      if (!wasOpen) this._wrap.classList.add('open');
    });

    this._panel.addEventListener('click', e => {
      const opt = e.target.closest('.cselect-option');
      if (!opt) return;
      this._sel.value = opt.dataset.value;
      this._sel.dispatchEvent(new Event('change', { bubbles: true }));
      this.refresh();
      this._wrap.classList.remove('open');
    });

    new MutationObserver(() => this.refresh()).observe(this._sel, { childList: true });
  }
}

document.addEventListener('click', () => {
  document.querySelectorAll('.cselect.open').forEach(el => el.classList.remove('open'));
});
