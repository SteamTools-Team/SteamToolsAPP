

const PageAchievements = (() => {
  let _games    = [];
  let _appid    = '';
  let _name     = '';
  let _ach      = [];
  let _desired  = {};
  let _wired    = false;

  async function _post(path, body) {
    const r = await fetch(path, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(body || {}),
    });
    if (!r.ok) throw new Error(`HTTP ${r.status}`);
    return r.json();
  }

  function init() {
    if (!_wired) _wire();
    _showGames();
    loadGames();
  }

  function _wire() {
    _wired = true;
    document.getElementById('ach-back-btn')?.addEventListener('click', _showGames);
    document.getElementById('ach-manual-btn')?.addEventListener('click', _promptAppId);
    document.getElementById('ach-games-search')?.addEventListener('input', e => _renderGames(e.target.value));
    document.getElementById('ach-search')?.addEventListener('input', e => _renderItems(e.target.value));
    document.getElementById('ach-unlock-all')?.addEventListener('click', () => _setAll(true));
    document.getElementById('ach-lock-all')?.addEventListener('click', () => _setAll(false));
    document.getElementById('ach-reset-sel')?.addEventListener('click', _resetSelection);
    document.getElementById('ach-apply')?.addEventListener('click', _apply);

    document.getElementById('ach-items')?.addEventListener('change', e => {
      const cb = e.target.closest('input[type="checkbox"][data-ach]');
      if (!cb) return;
      _desired[cb.dataset.ach] = cb.checked;
      _refreshDirty();
    });

    document.getElementById('ach-games-grid')?.addEventListener('click', e => {
      const card = e.target.closest('.game-card[data-appid]');
      if (!card) return;
      openGame(card.dataset.appid, card.dataset.name || `Game #${card.dataset.appid}`);
    });
  }

  function _showGames() {
    document.getElementById('ach-list-view').style.display  = 'none';
    document.getElementById('ach-games-view').style.display = '';
    document.getElementById('ach-back-btn').style.display   = 'none';
  }

  async function loadGames() {
    const grid   = document.getElementById('ach-games-grid');
    const status = document.getElementById('ach-games-status');
    grid.innerHTML = '';
    status.textContent = 'Loading your Steam games…';

    const steam = API.getSteamStatus?.() || {};
    if (steam && steam.detected && steam.running === false) {
      status.innerHTML = '<span class="ach-warn">Steam is not running. Start Steam and sign in, then reload this page.</span>';
    }

    try {
      const res = await _post('/api/achievements/games');
      if (!res.ok) { status.innerHTML = `<span class="ach-warn">${escapeHtml(res.error || 'Could not list games')}</span>`; return; }
      _games = res.games || [];
      if (_games.length === 0) {
        status.innerHTML = '<span class="ach-muted">No installed Steam games found. Use “Open by AppID” for a game you own but haven’t installed.</span>';
        return;
      }
      status.innerHTML = '<span class="ach-muted">Showing games you own. Un-installed games fill in their names in the background on first visit.</span>';
      _renderGames('');
    } catch (e) {
      status.innerHTML = `<span class="ach-warn">${escapeHtml(String(e.message || e))}</span>`;
    }
  }

  function _renderGames(filter) {
    const grid = document.getElementById('ach-games-grid');
    const q = (filter || '').toLowerCase().trim();
    const list = q ? _games.filter(g => (g.name || '').toLowerCase().includes(q) || g.appid.includes(q)) : _games;
    grid.innerHTML = list.map((g, i) => {
      const delay = Math.min(i * 0.03, 0.18).toFixed(2);
      const title = g.name || `AppID ${g.appid}`;
      const badge = g.installed
        ? '<span class="game-card-installed-badge">Installed</span>'
        : '<span class="game-card-installed-badge ach-badge-owned">Owned</span>';
      return `
      <article class="game-card game-card--img" data-appid="${g.appid}" data-name="${escapeHtml(g.name || '')}" style="animation-delay:${delay}s;cursor:pointer">
        <div class="game-card-img-wrap">
          <img class="game-card-img" src="${getImageUrl(g.appid)}" alt="${escapeHtml(title)}" loading="lazy" onerror="steamIconError(this)">
          ${badge}
        </div>
        <div class="game-card-body">
          <div class="game-card-name" data-appid="${g.appid}" title="${escapeHtml(title)}">${escapeHtml(title)}</div>
          <div class="game-card-id">AppID: ${g.appid}</div>
        </div>
      </article>`;
    }).join('');
    _resolveMissingNames();
  }

  let _resolving = false;
  async function _resolveMissingNames() {
    if (_resolving) return;
    _resolving = true;
    try {
      for (const g of _games.slice()) {
        if (g.installed || g._tried) continue;
        g._tried = true;
        let keep = false, name = '';
        try {
          const res = await _post('/api/achievements/appname', { appid: g.appid });
          if (res && res.ok && res.isGame && res.name) { keep = true; name = res.name; }
        } catch (_) { keep = true; }

        if (keep) {
          g.name = name || g.name;
          document.querySelectorAll(`.game-card-name[data-appid="${g.appid}"]`).forEach(el => {
            if (name) { el.textContent = name; el.title = name; }
            el.closest('.game-card')?.setAttribute('data-name', name || '');
          });
        } else {

          const idx = _games.indexOf(g);
          if (idx >= 0) _games.splice(idx, 1);
          document.querySelectorAll(`.game-card[data-appid="${g.appid}"]`).forEach(el => el.remove());
        }
        await new Promise(r => setTimeout(r, 150));
      }
    } finally {
      _resolving = false;
    }
  }

  async function _promptAppId() {
    const confirmed = await Modal.open({
      titleText: 'Open by AppID',
      bodyHtml: `<p style="font-size:0.82rem;color:var(--text-2);margin-bottom:8px">Enter the Steam AppID of a game you own:</p>
                 <input id="ach-appid-input" type="text" inputmode="numeric" placeholder="e.g. 440"
                        style="width:100%;padding:9px 11px;border-radius:8px;border:1px solid var(--border);background:var(--bg-2);color:var(--text-1);font-size:0.9rem">`,
      confirmText: 'Open',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;
    const val = (document.getElementById('ach-appid-input')?.value || '').trim();
    if (/^\d+$/.test(val)) {
      const known = _games.find(g => g.appid === val);
      openGame(val, known ? known.name : `Game #${val}`);
    } else if (val) {
      Toast.error('Invalid AppID', 'An AppID is a number, e.g. 440.');
    }
  }

  async function openGame(appid, name) {
    _appid = String(appid);
    _name  = name;
    _ach = []; _desired = {};

    document.getElementById('ach-games-view').style.display = 'none';
    document.getElementById('ach-list-view').style.display  = '';
    document.getElementById('ach-back-btn').style.display   = '';

    document.getElementById('ach-game-name').textContent = name;
    const hero = document.getElementById('ach-hero');
    hero.style.backgroundImage = `url("${getImageUrl(appid)}")`;
    document.getElementById('ach-game-meta').textContent = 'Loading achievements…';
    _setProgress(0, 0);
    document.getElementById('ach-items').innerHTML = '';
    document.getElementById('ach-list-status').textContent = '';
    document.getElementById('ach-search').value = '';
    _setApplyEnabled(false);

    try {
      const res = await _post('/api/achievements/list', { appid: _appid });
      if (!res.ok) {
        document.getElementById('ach-game-meta').textContent = '';
        document.getElementById('ach-list-status').innerHTML = `<span class="ach-warn">${escapeHtml(res.error || 'Could not read achievements')}</span>`;
        return;
      }
      _ach = res.achievements || [];
      _desired = {};
      _ach.forEach(a => { _desired[a.id] = !!a.achieved; });
      if (_ach.length === 0) {
        document.getElementById('ach-game-meta').textContent = '';
        document.getElementById('ach-list-status').innerHTML = '<span class="ach-muted">This game exposes no achievements.</span>';
        return;
      }
      _renderItems('');
      _updateMeta();
    } catch (e) {
      document.getElementById('ach-game-meta').textContent = '';
      document.getElementById('ach-list-status').innerHTML = `<span class="ach-warn">${escapeHtml(String(e.message || e))}</span>`;
    }
  }

  function _iconUrl(a) {
    if (!a.icon) return '';
    return `https://cdn.cloudflare.steamstatic.com/steamcommunity/public/images/apps/${_appid}/${a.icon}`;
  }

  function _renderItems(filter) {
    const host = document.getElementById('ach-items');
    const q = (filter || '').toLowerCase().trim();
    const list = q
      ? _ach.filter(a => (a.name || '').toLowerCase().includes(q) || (a.id || '').toLowerCase().includes(q))
      : _ach;

    host.innerHTML = list.map(a => {
      const on = !!_desired[a.id];
      const desc = a.hidden && !a.desc ? 'Hidden achievement' : (a.desc || '');
      const url = _iconUrl(a);
      const icon = url
        ? `<img class="ach-item-icon" src="${url}" alt="" loading="lazy" onerror="this.style.visibility='hidden'">`
        : `<span class="ach-item-icon ach-item-icon--ph">🏆</span>`;
      return `
      <label class="ach-item ${on ? 'is-unlocked' : ''}" data-row="${escapeHtml(a.id)}">
        ${icon}
        <div class="ach-item-text">
          <div class="ach-item-name">${escapeHtml(a.name || a.id)}${a.hidden ? ' <span class="ach-tag">hidden</span>' : ''}</div>
          ${desc ? `<div class="ach-item-desc">${escapeHtml(desc)}</div>` : ''}
        </div>
        <span class="ach-switch">
          <input type="checkbox" data-ach="${escapeHtml(a.id)}" ${on ? 'checked' : ''}>
          <span class="ach-switch-track"></span>
        </span>
      </label>`;
    }).join('');

    host.querySelectorAll('input[type="checkbox"][data-ach]').forEach(cb => {
      cb.addEventListener('change', () => {
        cb.closest('.ach-item')?.classList.toggle('is-unlocked', cb.checked);
      });
    });
  }

  function _setProgress(unlocked, total) {
    const fill = document.getElementById('ach-progress-fill');
    if (fill) fill.style.width = total > 0 ? `${Math.round((unlocked / total) * 100)}%` : '0%';
  }

  function _setAll(state) {
    if (_ach.length === 0) return;
    _ach.forEach(a => { _desired[a.id] = state; });
    _renderItems(document.getElementById('ach-search').value);
    _refreshDirty();
  }

  function _resetSelection() {
    _ach.forEach(a => { _desired[a.id] = !!a.achieved; });
    _renderItems(document.getElementById('ach-search').value);
    _refreshDirty();
  }

  function _diff() {
    const unlock = [], lock = [];
    _ach.forEach(a => {
      const want = !!_desired[a.id];
      if (want && !a.achieved) unlock.push(a.id);
      else if (!want && a.achieved) lock.push(a.id);
    });
    return { unlock, lock };
  }

  function _refreshDirty() {
    const { unlock, lock } = _diff();
    _setApplyEnabled(unlock.length + lock.length > 0);
    _updateMeta();
  }

  function _updateMeta() {
    const unlocked = _ach.filter(a => !!_desired[a.id]).length;
    document.getElementById('ach-game-meta').textContent = `${unlocked} / ${_ach.length} unlocked`;
    _setProgress(unlocked, _ach.length);
  }

  function _setApplyEnabled(on) {
    const btn = document.getElementById('ach-apply');
    if (btn) btn.disabled = !on;
  }

  async function _apply() {
    const { unlock, lock } = _diff();
    if (unlock.length + lock.length === 0) return;

    const confirmed = await Modal.open({
      titleText: 'Apply achievement changes',
      bodyHtml: `<p style="font-size:0.84rem;line-height:1.8;color:var(--text-2)">
          For <strong>${escapeHtml(_name)}</strong>:</p>
        <ul style="margin:8px 0 0 16px;font-size:0.82rem;line-height:1.9;color:var(--text-2)">
          ${unlock.length ? `<li><span style="color:var(--green)">Unlock</span> ${unlock.length} achievement(s)</li>` : ''}
          ${lock.length ? `<li><span style="color:var(--yellow)">Lock</span> ${lock.length} achievement(s)</li>` : ''}
        </ul>
        <p style="margin-top:12px;font-size:0.78rem;color:var(--text-3)">Changes are written to Steam and sync to your account.</p>`,
      confirmText: 'Apply',
      cancelText: 'Cancel',
    });
    if (!confirmed) return;

    const btn = document.getElementById('ach-apply');
    if (btn) { btn.disabled = true; btn.innerHTML = '<div class="spinner spinner--sm spinner--white"></div> Applying…'; }

    try {
      const res = await _post('/api/achievements/apply', { appid: _appid, unlock, lock });
      if (!res.ok) {
        Toast.error('Apply failed', res.error || 'Steam rejected the changes.');
      } else {

        _ach.forEach(a => { a.achieved = !!_desired[a.id]; });
        const n = res.changed ?? (unlock.length + lock.length);
        if (res.confirmed === false) {
          Toast.warning('Sent to Steam', `${n} change(s) sent, but Steam didn’t confirm in time. Check in Steam; re-apply if needed.`);
        } else {
          Toast.success('Achievements updated', `${n} change(s) saved and confirmed by Steam.`);
        }
        State.addLog?.(`Achievements updated for ${_name} (${_appid}): +${unlock.length} / −${lock.length}`, 'success');
      }
    } catch (e) {
      Toast.error('Apply failed', String(e.message || e));
    } finally {
      if (btn) { btn.innerHTML = 'Apply changes'; }
      _refreshDirty();
    }
  }

  return { init, openGame, loadGames };
})();
