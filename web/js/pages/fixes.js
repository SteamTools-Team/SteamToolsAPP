const PageFixes = (() => {
  let _fixes = [];
  let _filtered = [];
  let _query = '';
  let _page = 0;
  const PER_PAGE = 48;

  async function init() {
    _bindActions();
    await _load();
  }

  async function _load() {
    const grid = document.getElementById('fixes-grid');
    if (grid) grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1;padding:60px 24px"><div class="spinner spinner--lg"></div><p style="margin-top:8px;font-size:.85rem;color:var(--text-3)">Loading fixes…</p></div>';

    try {
      const res = await API.getFixes();
      _fixes = (res.fixes || []).map(f => ({
        ...f,
        nameClean: f.name.replace(/^\[.*?\]\s*/, '').replace(/\s*по сети\s*$/, ''),
      }));
      _page = 0;
      _applyFilter();
    } catch (err) {
      if (grid) grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1;padding:60px 24px"><p>Failed to load fixes: ' + escapeHtml(err.message) + '</p></div>';
    }
  }

  function _applyFilter() {
    const q = _query.toLowerCase();
    _filtered = q
      ? _fixes.filter(f => f.nameClean.toLowerCase().includes(q) || f.name.toLowerCase().includes(q))
      : _fixes;
    _page = 0;
    _render();
  }

  function _render() {
    const grid = document.getElementById('fixes-grid');
    if (!grid) return;

    if (_filtered.length === 0) {
      const msg = document.getElementById('fixes-count');
      if (msg) msg.textContent = '';
      const existing = document.getElementById('fixes-pagination');
      if (existing) existing.remove();
      grid.innerHTML = '<div class="empty-state" style="grid-column:1/-1;padding:60px 24px"><p style="max-width:none">' + (_query ? 'No fixes match your search.' : 'No fixes available.') + '</p></div>';
      return;
    }

    const count = document.getElementById('fixes-count');
    if (count) count.textContent = _filtered.length + ' fix' + (_filtered.length !== 1 ? 'es' : '');

    const start = _page * PER_PAGE;
    const end = start + PER_PAGE;
    const slice = _filtered.slice(start, end);

    let html = '';
    for (let i = 0; i < slice.length; i++) {
      const f = slice[i];
      const appidMatch = f.name.match(/^\[(\d+)\]/);
      const appid = f.appid || (appidMatch ? appidMatch[1] : '');
      const steamImg = appid ? getImageUrl(appid) : '';
      const delay = Math.min(i * 0.03, 0.18).toFixed(2);
      const initials = f.appid ? '' : ((f.nameClean.match(/\b\w/g) || []).slice(0,2).join('').toUpperCase() || '?');

      html += '<div class="game-card game-card--img" data-url="' + escapeHtml(f.url) + '" data-name="' + escapeHtml(f.nameClean) + '" data-appid="' + escapeHtml(appid) + '" style="animation-delay:' + delay + 's">';
      html += '<div class="game-card-img-wrap">';
      if (appid) {
        html += '<img class="game-card-img" src="' + steamImg + '" data-appid="' + appid + '" data-name="' + escapeHtml(f.nameClean) + '" alt="' + escapeHtml(f.nameClean) + '" loading="lazy" onerror="_steamImgError(this)">';
      } else {
        html += '<div class="game-card-icon game-card-img-placeholder" data-name="' + escapeHtml(f.nameClean) + '">' + initials + '</div>';
      }
      html += '</div>';
      html += '<div class="game-card-body">';
      html += '<div class="game-card-name" title="' + escapeHtml(f.nameClean) + '">' + escapeHtml(f.nameClean) + '</div>';
      html += '<div class="game-card-id" style="display:flex;align-items:center;gap:4px;flex-wrap:wrap">';
      html += appid ? 'AppID: ' + appid : '\u2014';
      html += '<span style="font-size:0.7rem;color:var(--text-3)">' + escapeHtml(f.size || '') + '</span>';
      html += '</div></div>';
      html += '<div class="game-card-actions"><div style="display:flex;gap:6px;width:100%">';
      html += '<button class="btn btn-primary btn-sm fix-install-btn" style="flex:1">';
      html += '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>';
      html += ' Install Fix</button></div></div></div>';
    }
    grid.innerHTML = html;

    grid.querySelectorAll('.fix-install-btn').forEach(function(btn) {
      btn.addEventListener('click', function(e) {
        e.stopPropagation();
        var card = btn.closest('.game-card');
        _installFix(card.dataset.url, card.dataset.name, btn, card.dataset.appid);
      });
    });

    _renderPagination();
  }

  function _renderPagination() {
    var container = document.getElementById('fixes-pagination');
    if (!container) {
      container = document.createElement('div');
      container.id = 'fixes-pagination';
      container.className = 'pagination';
      var grid = document.getElementById('fixes-grid');
      if (grid && grid.parentNode) grid.parentNode.insertBefore(container, grid.nextSibling);
    }

    var total = Math.max(1, Math.ceil(_filtered.length / PER_PAGE));
    if (total <= 1) {
      container.innerHTML = '';
      return;
    }

    var html = '<div class="pagination-pages"><button class="page-btn" id="fx-prev" ' + (_page === 0 ? 'disabled' : '') + '>'
      + '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="15 18 9 12 15 6"/></svg></button>';

    var range = _pageRange(total);
    for (var i = 0; i < range.length; i++) {
      var p = range[i];
      if (p === '...') {
        html += '<span class="page-jump" data-side="' + (i === 1 ? 'left' : 'right') + '" title="Go to page">…</span>';
      } else {
        html += '<button class="page-btn' + (p === _page ? ' active' : '') + '" data-pg="' + p + '">' + (p + 1) + '</button>';
      }
    }

    html += '<button class="page-btn" id="fx-next" ' + (_page >= total - 1 ? 'disabled' : '') + '>'
      + '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="9 18 15 12 9 6"/></svg></button></div>';

    container.innerHTML = html;

    var prevEl = container.querySelector('#fx-prev');
    if (prevEl) prevEl.addEventListener('click', function() { if (_page > 0) { _page--; _render(); window.scrollTo(0, 0); } });
    var nextEl = container.querySelector('#fx-next');
    if (nextEl) nextEl.addEventListener('click', function() { if (_page < total - 1) { _page++; _render(); window.scrollTo(0, 0); } });
    container.querySelectorAll('[data-pg]').forEach(function(btn) {
      btn.addEventListener('click', function() { _page = +btn.dataset.pg; _render(); window.scrollTo(0, 0); });
    });
    container.querySelectorAll('.page-jump').forEach(function(el) {
      el.addEventListener('click', function() {
        var input = document.createElement('input');
        input.type = 'text';
        input.inputMode = 'numeric';
        input.className = 'page-jump-input';
        el.replaceWith(input);
        input.focus();
        var done = function() {
          var val = parseInt(input.value, 10);
          if (val >= 1 && val <= total) {
            _page = val - 1;
            _render();
            window.scrollTo(0, 0);
          } else {
            _render();
          }
        };
        input.addEventListener('blur', done);
        input.addEventListener('keydown', function(e) {
          if (e.key === 'Enter') { input.blur(); }
          if (e.key === 'Escape') { _render(); }
        });
      });
    });
  }

  function _pageRange(total) {
    var pages = [];
    if (total <= 7) {
      for (var i = 0; i < total; i++) pages.push(i);
      return pages;
    }
    pages.push(0);
    if (_page > 2) pages.push('...');
    for (var i = Math.max(1, _page - 1); i <= Math.min(total - 2, _page + 1); i++) pages.push(i);
    if (_page < total - 3) pages.push('...');
    pages.push(total - 1);
    return pages;
  }

  async function _installFix(url, name, btn, appid) {
    btn.disabled = true;
    btn.innerHTML = '<div class="spinner spinner--sm"></div>';

    try {

      var detect = await API.detectFix(name, appid);
      var gamePath = null;

      if (detect.ok && detect.found) {

        var cleanName = _fixName(name);
        gamePath = await new Promise(function(resolve) {
          Modal.open({
            titleText: 'Install Fix',
            bodyHtml: '<p style="margin-bottom:8px;color:var(--text-2);font-size:.85rem">Game detected at:</p>'
              + '<p style="padding:8px 10px;background:var(--surface-2);border-radius:6px;font-size:.8rem;color:var(--text);word-break:break-all;font-family:monospace">'
              + escapeHtml(detect.game_path) + '</p>'
              + '<p style="margin-top:8px;color:var(--text-3);font-size:.8rem">Install the fix for "' + escapeHtml(cleanName) + '" here?</p>',
            confirmText: 'Install',
            cancelText: 'Cancel',
          }).then(function(r) { resolve(r ? detect.game_path : null); });
        });
      } else {

        gamePath = await _promptPath(name);
      }

      if (!gamePath) {
        btn.disabled = false;
        btn.innerHTML = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg> Install Fix';
        return;
      }

      btn.innerHTML = '<div class="spinner spinner--sm"></div>';
      var res = await API.installFix(url, gamePath);
      if (res.ok) {
        btn.textContent = '\u2713';
        btn.style.background = 'var(--green)';
        btn.style.color = '#fff';
        btn.style.border = 'none';
        Toast.success('Fix successfully applied', '');
      } else {
        btn.disabled = false;
        btn.innerHTML = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg> Install Fix';
        Toast.error('Install failed', res.error || 'Unknown error');
      }
    } catch (err) {
      btn.disabled = false;
      btn.innerHTML = '<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg> Install Fix';
      Toast.error('Install failed', err.message);
    }
  }

  function _fixName(name) {
    return name.replace(/\s*по сети\s*$/, '').trim();
  }

  function _promptPath(gameName) {
    return new Promise(function(resolve) {
      var cleanName = _fixName(gameName);
      var body = '<p style="margin-bottom:12px;color:var(--text-2);font-size:.85rem">Game not found automatically. Make sure "' + escapeHtml(cleanName) + '" is installed, then select its root folder.</p><div style="display:flex;gap:8px"><input type="text" id="fix-path-input" placeholder="Paste the game folder path\u2026" style="flex:1;padding:8px 10px;border-radius:6px;border:1px solid var(--border);background:var(--surface-2);color:var(--text-1);font-size:.85rem;font-family:inherit"><button class="btn btn-ghost" id="fix-browse-btn">Browse\u2026</button></div>';
      Modal.open({
        titleText: 'Game Folder for ' + cleanName,
        bodyHtml: body,
        confirmText: 'Install',
        cancelText: 'Cancel',
      }).then(function(result) {
        if (!result) { resolve(null); return; }
        var input = document.getElementById('fix-path-input');
        resolve(input && input.value.trim() || null);
      });

      setTimeout(function() {
        var browseBtn = document.getElementById('fix-browse-btn');
        if (browseBtn) browseBtn.addEventListener('click', async function() {
          var input = document.getElementById('fix-path-input');
          var path = await API.pickFolder();
          if (path) input.value = path;
        });
      }, 50);
    });
  }

  function _bindActions() {
    var searchEl = document.getElementById('fixes-search');
    if (searchEl) searchEl.addEventListener('input', debounce(function(e) {
      _query = e.target.value.trim();
      _applyFilter();
    }, 300));

    var refreshBtn = document.getElementById('fixes-refresh-btn');
    if (refreshBtn) refreshBtn.addEventListener('click', function() { _load(); });
  }

  return { init };
})();
