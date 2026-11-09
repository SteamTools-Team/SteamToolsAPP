

const PageAvailable = (() => {
  let _allGames = [];
  let _games    = [];
  let _total    = 0;
  let _query    = '';
  let _page     = 0;
  let _loading  = false;
  let _filters  = { type: 'all', tag: '', nodrm: false, hidensfw: true };
  let _abortController = null;
  const PER_PAGE = 24;

  const ST_OPTS_DEFAULT = { dlc: true, delisted: true, software: true, nsfw: false };
  const SOFTWARE_TYPES = ['software', 'video', 'hardware'];
  let _stOpts = null;
  let _savingStOpts = false;

  function _getStOpts() {
    if (!_stOpts) {
      const saved = State.getSetting('available_options');
      _stOpts = { ...ST_OPTS_DEFAULT, ...(saved && typeof saved === 'object' ? saved : {}) };
    }
    return _stOpts;
  }

  async function _saveStOpts() {
    _savingStOpts = true;
    try {
      await API.saveSettings({ available_options: { ..._getStOpts() } });
    } catch {

    } finally {
      _savingStOpts = false;
    }
  }

  function _isSteamtools() {
    return State.getSetting('provider') === 'steamtools';
  }

  let _prerendered = false;

  async function init() {
    new CustomSelect(document.getElementById('available-sort'));
    new CustomSelect(document.getElementById('available-filter-type'));
    new CustomSelect(document.getElementById('available-filter-tag'));
    _bindActions();
    if (_prerendered && _allGames.length > 0) {
      _prerendered = false;
      return;
    }
    await _load();
  }

  async function _load(query = '') {
    if (_abortController) _abortController.abort();
    _abortController = new AbortController();
    const signal = _abortController.signal;

    _loading = true;
    _query   = query;
    _page    = 0;

    const grid = document.getElementById('available-games-grid');

    if (!query && API.gamesCacheExists && API.gamesCacheExists()) {
      const cached = API.getGamesCache();
      if (cached) {
        _allGames = cached.games || cached;
        _total    = cached.total || _allGames.length;
        _loading = false;
        _toggleProviderFilters();
        _populateTags();
        _applyFilters();
        return;
      }
    }

    const _provider = State.getSetting('provider');
    const loadingMsg = query ? 'Searching…' : (_provider === 'hubcap' ? 'Loading game catalog… (this may take a while on first load)' : 'Loading game catalog…');

    let spinnerTimer = null;
    const showSpinner = () => {
      if (grid) grid.innerHTML = `<div style="grid-column:1/-1;padding:40px 0;display:flex;flex-direction:column;align-items:center;gap:12px;color:var(--text-3)"><div class="spinner spinner--lg"></div><span style="font-size:0.82rem">${loadingMsg}</span></div>`;
    };
    spinnerTimer = setTimeout(showSpinner, 200);

    try {
      const res = await API.getAvailableGames(query, signal);
      clearTimeout(spinnerTimer);
      if (signal.aborted) return;

      _allGames = res.games || res;
      _total    = res.total || _allGames.length;
      if (res._hubcap_fallback) {
        const msg = res._hubcap_error || 'Hubcap API unavailable';
        Toast.warning('Hubcap API unavailable', msg + '. Using default provider as fallback');
      }
      _toggleProviderFilters();
      _populateTags();
      _applyFilters();
    } catch (err) {
      clearTimeout(spinnerTimer);
      if (signal.aborted) return;
      const provider = State.getSetting('provider');
      if (provider === 'hubcap') {
        if (grid) grid.innerHTML = `<div style="grid-column:1/-1"><div class="empty-state-large"><h3>Hubcap API unreachable</h3><p style="max-width:400px;margin:0 auto">Please verify your Hubcap API key in <strong>Settings</strong> → <strong>Provider</strong>.<br><br>If the issue persists, switch to <strong>Ryuu API</strong> as your provider.</p></div></div>`;
        Toast.warning('Hubcap API error', 'Check your API key in Settings > Provider');
      } else if (provider === 'depotbox') {
        if (grid) grid.innerHTML = `<div style="grid-column:1/-1"><div class="empty-state-large"><h3>Search failed</h3><p>Could not reach DepotBox. Please try again later.</p></div></div>`;
        Toast.error('DepotBox error', 'Could not reach DepotBox API');
      } else {
        if (grid) grid.innerHTML = `<div style="grid-column:1/-1"><div class="empty-state-large"><h3>Failed to load catalog</h3><p>${escapeHtml(err.message)}</p></div></div>`;
        Toast.error('Load failed', 'Could not load game catalog');
      }
    } finally {
      if (!signal.aborted) {
        _loading = false;
        _abortController = null;
      }
    }
  }

  function _toggleProviderFilters() {
    const provider = State.getSetting('provider');
    const bar = document.querySelector('.available-filter-bar');
    const sortWrap = document.querySelector('.toolbar-right');
    if (bar) bar.style.display = (provider === 'hubcap' || provider === 'depotbox' || provider === 'steamtools') ? 'none' : '';
    if (sortWrap) sortWrap.style.display = (provider === 'depotbox' || provider === 'steamtools') ? 'none' : '';
    const opts = document.getElementById('available-options');
    if (opts) {
      opts.hidden = provider !== 'steamtools';
      if (opts.hidden) _closeStOptions();
    }
  }

  function _syncStOptions() {
    const opts = _getStOpts();
    document.querySelectorAll('#available-options-panel [data-opt]').forEach(input => {
      input.checked = !!opts[input.dataset.opt];
    });

    const changed = Object.keys(ST_OPTS_DEFAULT).some(k => opts[k] !== ST_OPTS_DEFAULT[k]);
    document.getElementById('available-options-btn')?.classList.toggle('has-changes', changed);
  }

  function _closeStOptions() {
    const wrap = document.getElementById('available-options');
    if (!wrap) return;
    wrap.classList.remove('open');
    document.getElementById('available-options-btn')?.setAttribute('aria-expanded', 'false');
  }

  function _populateTags() {
    const select = document.getElementById('available-filter-tag');
    if (!select) return;
    const tags = new Set();
    for (const g of _allGames) {
      if (Array.isArray(g.tags)) g.tags.forEach(t => { if (t) tags.add(t); });
    }
    const current = select.value;
    const sorted  = [...tags].sort();
    select.innerHTML = '<option value="">All Genres</option>' +
      sorted.map(t => `<option value="${escapeHtml(t)}"${t === current ? ' selected' : ''}>${escapeHtml(t)}</option>`).join('');
  }

  function _readFilters() {
    return {
      type:     document.getElementById('available-filter-type')?.value || 'all',
      tag:      document.getElementById('available-filter-tag')?.value  || '',
      nodrm:    document.getElementById('available-filter-nodrm')?.classList.contains('active') ?? false,
      hidensfw: document.getElementById('available-filter-hidensfw')?.classList.contains('active') ?? true,
    };
  }

  function _applyFilters() {
    _filters = _readFilters();
    const { type, tag, nodrm, hidensfw } = _filters;

    if (_isSteamtools()) {

      const opts = _getStOpts();
      _games = _allGames.filter(g => {
        if (!opts.dlc      && g.type === 'dlc')                  return false;
        if (!opts.delisted && g.delisted)                        return false;
        if (!opts.software && SOFTWARE_TYPES.includes(g.type))   return false;
        if (!opts.nsfw     && g.nsfw)                            return false;
        return true;
      });
    } else {
      _games = _allGames.filter(g => {
        if (type !== 'all' && g.type !== type)                              return false;
        if (tag  && !(Array.isArray(g.tags) && g.tags.includes(tag)))      return false;
        if (nodrm   && g.drm)                                               return false;
        if (hidensfw && g.nsfw)                                             return false;
        return true;
      });
    }

    _page = 0;
    _updateSubtitle();
    _updateClearBtn();
    _applySort();
  }

  function _updateSubtitle() {
    const sub = document.getElementById('available-subtitle');
    if (!sub) return;

    const filtered = _games.length;
    const { type, tag, nodrm } = _filters;
    const hasFilter = type !== 'all' || tag || nodrm || !_filters.hidensfw || _query;

    if (_games.length > 0 && _games[0]._trending_cat) {
      sub.textContent = `Trending & popular games — search above for a specific title`;
    } else if (hasFilter) {
      sub.textContent = _query
        ? `${filtered} result${filtered !== 1 ? 's' : ''} for "${_query}" (${_total} total in catalog)`
        : `${filtered} game${filtered !== 1 ? 's' : ''} matching filters (${_total} total)`;
    } else {
      sub.textContent = `${_total} games available in catalog`;
    }
  }

  function _updateClearBtn() {
    const btn = document.getElementById('available-clear-filters');
    if (!btn) return;
    const { type, tag, nodrm, hidensfw } = _filters;
    const hasFilter = type !== 'all' || tag || nodrm || !hidensfw;
    btn.style.display = hasFilter ? 'inline-flex' : 'none';
  }

  function _clearFilters() {
    const typeEl  = document.getElementById('available-filter-type');
    const tagEl   = document.getElementById('available-filter-tag');
    const nodrmEl = document.getElementById('available-filter-nodrm');
    const nsfwEl  = document.getElementById('available-filter-hidensfw');
    if (typeEl)  typeEl.value = 'all';
    if (tagEl)   tagEl.value  = '';
    if (nodrmEl) nodrmEl.classList.remove('active');
    if (nsfwEl)  { nsfwEl.classList.remove('active'); nsfwEl.classList.add('active'); }
    _applyFilters();
  }

  function _render() {
    _renderGrid();
    _renderPagination();
  }

  function _renderGrid() {
    const grid = document.getElementById('available-games-grid');
    if (!grid) return;

    const start = _page * PER_PAGE;
    const end   = start + PER_PAGE;
    const slice = _games.slice(start, end);

    if (slice.length === 0) {
      const provider = State.getSetting('provider');
      if ((provider === 'depotbox' || provider === 'steamtools') && !document.getElementById('available-search')?.value?.trim()) {
        grid.innerHTML = `
          <div style="grid-column:1/-1">
            <div class="empty-state-large">
              <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="1.2">
                <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
              </svg>
              <h3>No trending games available</h3>
              <p>Try searching for a specific game above,<br>or switch to <strong>Ryuu API</strong> or <strong>Hubcap API</strong> in Settings for a full game list.</p>
            </div>
          </div>`;
        return;
      }
      grid.innerHTML = `
        <div style="grid-column:1/-1">
          <div class="empty-state-large">
            <svg width="40" height="40" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="1.2">
              <circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/>
            </svg>
            <h3>No games found</h3>
            <p>Try a different search term or adjust the filters.</p>
          </div>
        </div>`;
      return;
    }

    const isPremium = ['premium1', 'premium2'].includes(Auth.getState().tier || 'standard');
    const provider = State.getSetting('provider');
    const isNonUpdatable = provider === 'hubcap' || provider === 'depotbox' || provider === 'steamtools';
    const hasTrending = _games.some(g => g._trending_cat);
    const queueActive = DownloadQueue.isEnabled();

    const _svgUpdate = `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M23 4v6h-6"/><path d="M1 20v-6h6"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>`;

    if (hasTrending) {
      const cats = {};
      for (const g of _games) {
        const cat = g._trending_cat || 'Other';
        if (!cats[cat]) cats[cat] = [];
        cats[cat].push(g);
      }
      let sections = '';
      for (const [catName, catGames] of Object.entries(cats)) {
        sections += `<div class="trending-section" style="grid-column:1/-1;margin-bottom:4px">
          <h3 style="font-size:0.95rem;font-weight:700;margin:0 0 12px 0;color:var(--text)">${escapeHtml(catName)}</h3>
          <div class="games-grid-inline" style="display:grid;grid-template-columns:repeat(auto-fill,minmax(180px,1fr));gap:12px">
            ${catGames.slice(0, 12).map((g, i) => {
              const installed = State.isInstalled(g.appid);
              const steamImg = getImageUrl(g.appid);
              return `<div class="game-card game-card--img" data-appid="${g.appid}" style="animation-delay:${(i*0.03).toFixed(2)}s">
                <div class="game-card-img-wrap">
                  <img class="game-card-img" src="${steamImg}" data-appid="${g.appid}" data-name="${escapeHtml(g.name)}" alt="${escapeHtml(g.name)}" loading="lazy" onerror="_steamImgError(this)">
                </div>
                <div class="game-card-body">
                  <div class="game-card-name" title="${escapeHtml(g.name)}">${escapeHtml(g.name)}</div>
                  <div class="game-card-id">AppID: ${g.appid}</div>
                </div>
                <div class="game-card-actions">
                  ${installed
                    ? `<button class="btn btn-ghost btn-sm" style="flex:1;color:var(--green);border-color:var(--green-border)" disabled>
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                        Installed
                       </button>`
                    : `<button class="btn btn-primary btn-sm btn-install" style="flex:1" data-appid="${g.appid}" data-name="${escapeHtml(g.name)}">
                        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                        ${queueActive ? 'Add to Queue' : 'Install'}
                       </button>`
                  }
                </div>
              </div>`;
            }).join('')}
          </div>
        </div>`;
      }
      grid.innerHTML = sections;
      grid.querySelectorAll('.btn-install').forEach(btn => {
        btn.addEventListener('click', e => {
          e.stopPropagation();
          PageInstall.installFromCatalog(btn.dataset.appid, btn.dataset.name);
        });
      });
      _bindCardDetails(grid);
      return;
    }

    grid.innerHTML = slice.map((g, i) => {
      const installed = State.isInstalled(g.appid);
      const steamImg  = getImageUrl(g.appid);
      const drmBadge  = g.drm  ? `<span class="badge badge--yellow" style="font-size:0.68rem;padding:2px 5px">DRM</span>`  : '';
      const nsfwBadge = g.nsfw ? `<span class="badge badge--red"    style="font-size:0.68rem;padding:2px 5px">NSFW</span>` : '';
      const dlcBadge  = g.type === 'dlc' ? `<span class="badge badge--blue" style="font-size:0.68rem;padding:2px 5px">DLC</span>` : '';
      const delistedBadge = g.delisted ? `<span class="badge badge--neutral" style="font-size:0.68rem;padding:2px 5px">Delisted</span>` : '';
      const delay     = Math.min(i * 0.04, 0.18).toFixed(2);
      return `
        <div class="game-card game-card--img" data-appid="${g.appid}" style="animation-delay:${delay}s">
          <div class="game-card-img-wrap">
            <img class="game-card-img"
                 src="${steamImg}"
                 data-appid="${g.appid}"
                 data-name="${escapeHtml(g.name)}"
                 alt="${escapeHtml(g.name)}"
                 loading="lazy"
                 onerror="_steamImgError(this)">
          </div>
          <div class="game-card-body">
            <div class="game-card-name" title="${escapeHtml(g.name)}">${escapeHtml(g.name)}</div>
            <div class="game-card-id" style="display:flex;align-items:center;gap:4px;flex-wrap:wrap">
              AppID: ${g.appid}${dlcBadge}${delistedBadge}${drmBadge}${nsfwBadge}
            </div>
          </div>
          <div class="game-card-actions">
            <div style="display:flex;gap:6px;width:100%">
              ${!installed
                ? `<button class="btn btn-primary btn-sm btn-install" style="flex:1" data-appid="${g.appid}" data-name="${escapeHtml(g.name)}">
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                    ${queueActive ? 'Add to Queue' : 'Install'}
                  </button>`
                : `<button class="btn btn-ghost btn-sm" style="flex:1;color:var(--green);border-color:var(--green-border)" disabled>
                    <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
                    Installed
                  </button>`
              }
              ${isNonUpdatable ? '' : `
              <button class="btn btn-ghost btn-sm btn-update ${isPremium ? '' : 'btn-locked'}" title="${isPremium ? 'Update manifest' : 'Premium required'}" data-appid="${g.appid}" data-name="${escapeHtml(g.name)}">
                ${_svgUpdate}
              </button>`}
            </div>
          </div>
        </div>
      `;
    }).join('');

    grid.querySelectorAll('.btn-install').forEach(btn => {
      btn.addEventListener('click', e => {
        e.stopPropagation();
        PageInstall.installFromCatalog(btn.dataset.appid, btn.dataset.name);
      });
    });

    grid.querySelectorAll('.btn-update').forEach(btn => {
      btn.addEventListener('click', async e => {
        e.stopPropagation();
        const tier = Auth.getState().tier || 'standard';
        if (!['premium1', 'premium2'].includes(tier)) {
          Toast.error('Premium Required', 'Game updates are a Premium feature.');
          return;
        }
        const appid = btn.dataset.appid;
        const name  = btn.dataset.name;
        btn.disabled = true;
        btn.innerHTML = `<div class="spinner spinner--sm"></div>`;
        const result = await API.updateManifestOnly(appid);
        btn.disabled = false;
        btn.innerHTML = _svgUpdate;
        if (result.ok && result.alreadyLatest) {
          Toast.info('Already up to date', `${name} is already on the latest manifest`);
        } else if (result.ok) {
          Toast.success('Manifest Updated', `${name} manifest has been updated`);
        } else if (result.premium_required) {
          Toast.error('Premium Required', result.error || 'Premium required');
        } else {
          Toast.error('Update Failed', result.error || 'An error occurred');
        }
      });
    });

    _bindCardDetails(grid);
  }

  function _bindCardDetails(grid) {
    grid.querySelectorAll('.game-card').forEach(card => {
      card.addEventListener('click', () => {
        const appid = card.dataset.appid;
        const g = (Array.isArray(_games)
          ? _games.find(x => x.appid === appid)
          : null) || { appid, name: card.querySelector('.game-card-name')?.textContent || `Game #${appid}` };
        GameDetails.open(g);
      });
    });
  }

  function _renderPagination() {
    const container = document.getElementById('available-pagination');
    if (!container) return;
    if (_games.length > 0 && _games[0]._trending_cat) { container.innerHTML = ''; return; }

    const total = Math.max(1, Math.ceil(_games.length / PER_PAGE));
    if (total <= 1) { container.innerHTML = ''; return; }

    let html = `<div class="pagination-pages">
      <button class="page-btn" id="av-prev" ${_page === 0 ? 'disabled' : ''}>
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

    html += `<button class="page-btn" id="av-next" ${_page >= total - 1 ? 'disabled' : ''}>
      <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="9 18 15 12 9 6"/></svg>
    </button>
    </div>`;

    container.innerHTML = html;

    container.querySelector('#av-prev')?.addEventListener('click', () => { if (_page > 0) { _page--; _render(); } });
    container.querySelector('#av-next')?.addEventListener('click', () => { if (_page < total - 1) { _page++; _render(); } });
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

  function _applySort() {
    const sort = document.getElementById('available-sort')?.value || 'name';
    _games.sort((a, b) => {
      if (sort === 'name')       return a.name.localeCompare(b.name);
      if (sort === 'name-desc')  return b.name.localeCompare(a.name);
      if (sort === 'appid')      return +a.appid - +b.appid;
      if (sort === 'appid-desc') return +b.appid - +a.appid;
      return 0;
    });
    _page = 0;
    _render();
  }

  function _bindActions() {
    const searchInput = document.getElementById('available-search');
    const sortSelect  = document.getElementById('available-sort');
    const refreshBtn  = document.getElementById('available-refresh-btn');

    searchInput?.addEventListener('input', debounce(() => {
      _load(searchInput.value.trim());
    }, 300));

    sortSelect?.addEventListener('change', _applySort);
    refreshBtn?.addEventListener('click', () => _load(_query));

    document.getElementById('available-filter-type')?.addEventListener('change', _applyFilters);
    document.getElementById('available-filter-tag')?.addEventListener('change',  _applyFilters);

    document.getElementById('available-filter-nodrm')?.addEventListener('click', function() {
      this.classList.toggle('active');
      _applyFilters();
    });
    document.getElementById('available-filter-hidensfw')?.addEventListener('click', function() {
      this.classList.toggle('active');
      _applyFilters();
    });

    document.getElementById('available-clear-filters')?.addEventListener('click', _clearFilters);

    const optsWrap = document.getElementById('available-options');
    const optsBtn  = document.getElementById('available-options-btn');
    _syncStOptions();
    optsBtn?.addEventListener('click', e => {
      e.stopPropagation();
      const open = !optsWrap.classList.contains('open');
      optsWrap.classList.toggle('open', open);
      optsBtn.setAttribute('aria-expanded', String(open));
    });
    document.querySelectorAll('#available-options-panel [data-opt]').forEach(input => {
      input.addEventListener('change', () => {
        _getStOpts()[input.dataset.opt] = input.checked;
        _saveStOpts();
        _syncStOptions();
        _applyFilters();
      });
    });
    document.addEventListener('click', e => {
      if (optsWrap?.classList.contains('open') && !optsWrap.contains(e.target)) _closeStOptions();
    });
    document.addEventListener('keydown', e => {
      if (e.key === 'Escape') _closeStOptions();
    });
  }

  window.addEventListener('statechange', e => {
    if (e.detail.area === 'installed' && _games.length > 0) _renderGrid();
    if (e.detail.area === 'settings') {

      if (_savingStOpts) return;

      _stOpts = null;
      _syncStOptions();
      API.invalidateGamesCache();
      const page = document.getElementById('page-available');
      if (page && page.classList.contains('active')) {
        _load(document.getElementById('available-search-input')?.value || '');
      }
    }
  });

  function prerender() {
    if (!API.gamesCacheExists || !API.gamesCacheExists()) return;
    const cached = API.getGamesCache();
    if (!cached) return;
    _allGames = cached.games || [];
    _total    = cached.total || _allGames.length;
    _query    = '';
    _page     = 0;
    _loading  = false;
    _prerendered = true;
    const grid = document.getElementById('available-games-grid');
    if (grid) {
      _toggleProviderFilters();
      _populateTags();
      _applyFilters();
    }
  }

  return { init, prerender, reload: () => _load(_query), refresh: () => { if (!_loading) _load(_query); } };
})();

function _steamImgError(img) {
  const appid = img.dataset.appid || '';
  const name  = img.dataset.name  || img.alt || '';
  if (!img.dataset.fb) {
    img.dataset.fb = '1';
    img.src = `https://generator.ryuu.lol/files/images/${appid}.jpg`;
  } else {
    const wrap = img.closest('.game-card-img-wrap');
    if (wrap) {
      const initials = name.split(/\s+/).map(w => w[0] || '').join('').toUpperCase().slice(0, 2) || '?';
      wrap.innerHTML = `<div class="game-card-icon">${initials}</div>`;
    }
  }
}
