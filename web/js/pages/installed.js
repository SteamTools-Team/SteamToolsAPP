

const PageInstalled = (() => {
  let _all     = [];
  let _filtered = [];
  let _page    = 0;
  const PER_PAGE = 12;

  function init() {
    _bindActions();
    _render();
  }

  async function _loadData() {
    const records = await API.getInstalledGames();
    _all = Object.entries(records).map(([appid, e]) => ({
      appid,
      name:      e.name || `Game_${appid}`,
      installdir: e.installdir || '—',
      timestamp: e.timestamp || 0,
    }));
    _all.sort((a, b) => b.timestamp - a.timestamp);
  }

  function _applyFilter() {
    const q = (document.getElementById('installed-search')?.value || '').toLowerCase();
    _filtered = q
      ? _all.filter(g => g.name.toLowerCase().includes(q) || g.appid.includes(q))
      : [..._all];
  }

  async function _render() {
    await _loadData();
    _applyFilter();
    _renderGrid();
    _renderPagination();
    _updateSubtitle();
    _updateNav();
  }

  function _updateNav() {
    const countEl = document.getElementById('stat-installed-count');
    if (countEl) countEl.textContent = _all.length;
  }

  function _updateSubtitle() {
    const sub = document.getElementById('installed-subtitle');
    if (sub) sub.textContent = _all.length === 0
      ? 'No games installed yet'
      : `${_all.length} game${_all.length !== 1 ? 's' : ''} installed`;
  }

  function _renderGrid() {
    const grid    = document.getElementById('installed-grid-container');
    const empty   = document.getElementById('installed-empty');
    if (!grid) return;

    if (_filtered.length === 0) {
      if (empty) empty.style.display = 'block';
      grid.style.display = 'none';
      return;
    }

    if (empty) empty.style.display = 'none';
    grid.style.display = '';

    const start = _page * PER_PAGE;
    const slice = _filtered.slice(start, start + PER_PAGE);

    const tier      = Auth.getState().tier || 'standard';
    const isPremium = tier === 'premium1' || tier === 'premium2';
    const provider  = State.getSetting('provider');
    const isNonUpdatable = provider === 'hubcap' || provider === 'depotbox';

    const _svgUpdate = `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M23 4v6h-6"/><path d="M1 20v-6h6"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>`;

    const steamImgUrl = appid => getImageUrl(appid);

    grid.innerHTML = slice.map((g, i) => {
      const steamImg = steamImgUrl(g.appid);
      const delay = Math.min(i * 0.04, 0.18).toFixed(2);
      return `
      <article class="game-card game-card--img" data-appid="${g.appid}" style="animation-delay:${delay}s">
        <div class="game-card-img-wrap">
          <img class="game-card-img" src="${steamImg}" data-appid="${g.appid}" data-name="${escapeHtml(g.name)}" alt="${escapeHtml(g.name)}" loading="lazy" onerror="steamIconError(this)">
          <span class="game-card-installed-badge">Installed</span>
        </div>
        <div class="game-card-body">
          <div class="game-card-name" title="${escapeHtml(g.name)}">${escapeHtml(g.name)}</div>
          <div class="game-card-id">AppID: ${g.appid}</div>
        </div>
        <div class="game-card-actions">
          ${!isNonUpdatable ? `
          <button class="btn btn-ghost btn-sm btn-update-game ${isPremium ? '' : 'btn-locked'}" data-appid="${g.appid}" data-name="${escapeHtml(g.name)}" title="${isPremium ? 'Update manifest' : 'Premium required'}">
            ${_svgUpdate}
            Update
          </button>` : ''}
          <button class="btn btn-ghost-danger btn-sm btn-remove-game" data-appid="${g.appid}" data-name="${escapeHtml(g.name)}">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
              <polyline points="3 6 5 6 21 6"/>
              <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/>
            </svg>
            Remove
          </button>
        </div>
      </article>
      `;
    }).join('');

    grid.querySelectorAll('.btn-update-game').forEach(btn => {
      btn.addEventListener('click', e => { e.stopPropagation(); PageInstall.startUpdate(btn.dataset.appid, btn.dataset.name); });
    });

    grid.querySelectorAll('.btn-remove-game').forEach(btn => {
      btn.addEventListener('click', e => { e.stopPropagation(); _confirmRemove(btn.dataset.appid, btn.dataset.name); });
    });

    grid.querySelectorAll('.game-card').forEach(card => {
      card.addEventListener('click', () => {
        const appid = card.dataset.appid;
        const g = _all.find(x => x.appid === appid) || { appid, name: card.querySelector('.game-card-name')?.textContent || `Game #${appid}` };
        GameDetails.open(g, true);
      });
    });
  }

  function _renderPagination() {
    const container = document.getElementById('installed-pagination');
    if (!container) return;
    const total = Math.ceil(_filtered.length / PER_PAGE);
    if (total <= 1) { container.innerHTML = ''; return; }

    let html = `<div class="pagination-pages">
      <button class="page-btn" id="ins-prev" ${_page === 0 ? 'disabled' : ''}>
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="15 18 9 12 15 6"/></svg>
      </button>
    `;

    const range = _pageRange(total);
    for (const p of range) {
      if (p === '...') {
        html += `<span class="page-jump" data-side="${range.indexOf(p) === 1 ? 'left' : 'right'}" title="Go to page">…</span>`;
      } else {
        html += `<button class="page-btn ${p === _page ? 'active' : ''}" data-pg="${p}">${p + 1}</button>`;
      }
    }

    html += `<button class="page-btn" id="ins-next" ${_page >= total - 1 ? 'disabled' : ''}>
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="9 18 15 12 9 6"/></svg>
    </button>
    </div>`;

    container.innerHTML = html;

    container.querySelector('#ins-prev')?.addEventListener('click', () => { if (_page > 0) { _page--; _render(); } });
    container.querySelector('#ins-next')?.addEventListener('click', () => { if (_page < total - 1) { _page++; _render(); } });
    container.querySelectorAll('[data-pg]').forEach(btn => {
      btn.addEventListener('click', () => { _page = +btn.dataset.pg; _render(); });
    });
    container.querySelectorAll('.page-jump').forEach(el => {
      el.addEventListener('click', () => {
        const input = document.createElement('input');
        input.type = 'text';
        input.inputMode = 'numeric';
        input.className = 'page-jump-input';
        el.replaceWith(input);
        input.focus();
        const done = () => {
          const val = parseInt(input.value, 10);
          if (val >= 1 && val <= total) {
            _page = val - 1;
            _render();
          } else {
            _render();
          }
        };
        input.addEventListener('blur', done);
        input.addEventListener('keydown', e => {
          if (e.key === 'Enter') { input.blur(); }
          if (e.key === 'Escape') { _render(); }
        });
      });
    });
  }

  function _pageRange(total) {
    const pages = [];
    if (total <= 7) {
      for (let i = 0; i < total; i++) pages.push(i);
      return pages;
    }
    pages.push(0);
    if (_page > 2)         pages.push('...');
    for (let i = Math.max(1, _page - 1); i <= Math.min(total - 2, _page + 1); i++) pages.push(i);
    if (_page < total - 3) pages.push('...');
    pages.push(total - 1);
    return pages;
  }

  async function _confirmRemove(appid, name) {
    const confirmed = State.getSetting('confirm_remove')
      ? await Modal.open({
          titleText:   'Remove Game',
          bodyHtml:    `<p>Remove <strong>${escapeHtml(name)}</strong> (AppID: ${appid}) from your Steam library?</p><p style="margin-top:8px;font-size:0.78rem;color:var(--text-3)">This will delete the manifest and game files.</p>`,
          confirmText: 'Remove',
          cancelText:  'Cancel',
          danger:      true,
        })
      : true;

    if (!confirmed) return;

    const result = await API.removeGame(appid);

    if (result.ok) {
      Toast.success('Game Removed', `${name} has been removed`);
      _page = 0;
      _render();
    } else {
      Toast.error('Removal Failed', result.error || 'An error occurred');
    }
  }

  async function _confirmRemoveAll() {
    const count = _all.length;
    if (count === 0) {
      Toast.info('Nothing to remove', 'No games are installed');
      return;
    }

    const confirmed = await Modal.open({
      titleText:   'Remove All Games',
      bodyHtml:    `<p>Remove all <strong>${count} games</strong> from your Steam library?</p><p style="margin-top:8px;color:var(--red);font-size:0.78rem">This action cannot be undone.</p>`,
      confirmText: 'Remove All',
      cancelText:  'Cancel',
      danger:      true,
    });

    if (!confirmed) return;


    for (const game of _all) {
      await API.removeGame(game.appid);
    }

    State.addLog(`Removed all ${count} games`, 'warning');
    Toast.success('All Games Removed', `${count} games have been removed`);
    _page = 0;
    _render();
  }

  function _bindActions() {
    document.getElementById('installed-search')?.addEventListener('input', debounce(() => {
      _applyFilter();
      _page = 0;
      _renderGrid();
      _renderPagination();
    }, 200));

    document.getElementById('remove-all-btn')?.addEventListener('click', _confirmRemoveAll);
  }

  window.addEventListener('statechange', e => {
    if (e.detail.area === 'installed') _render();
  });

  return { init, render: _render };
})();
