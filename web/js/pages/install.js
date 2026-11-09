

const PageInstall = (() => {
  let _installing = false;
  let _manifestFiles = [];

  function init() {
    _bindActions();
    _setupQuickSearch();
    _setupManifestUI();
    DownloadQueue.init();
    _updateInstallBtnLabel();
  }

  function _bindActions() {
    const btn   = document.getElementById('install-btn');
    const input = document.getElementById('install-appid-input');

    btn?.addEventListener('click', () => _startInstall());
    input?.addEventListener('keydown', e => {
      if (e.key === 'Enter') _startInstall();
    });
  }

  function _updateInstallBtnLabel() {
    const btn = document.getElementById('install-btn');
    if (!btn || btn.disabled) return;
    const queued = DownloadQueue.isEnabled();
    const icon = queued
      ? `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>`
      : `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>`;
    btn.innerHTML = `${icon} ${queued ? 'Add to Queue' : 'Install'}`;
  }

  async function _startInstall() {
    const input = document.getElementById('install-appid-input');
    const raw   = input?.value?.trim();
    if (!raw) {
      Toast.warning('AppID required', 'Please enter a Steam AppID or URL');
      input?.focus();
      return;
    }

    const appid = API.extractAppId(raw);
    if (!appid) {
      Toast.error('Invalid input', 'Could not extract a valid AppID from the input');
      return;
    }

    if (DownloadQueue.isEnabled()) {
      const name = await _getGameName(appid) || `Game_${appid}`;
      DownloadQueue.enqueue(appid, name, 'install');
      if (input) input.value = '';
      return;
    }

    await _runInstall(appid);
  }

  async function _getGameName(appid) {
    const mock = API.getGameByAppId(appid);
    if (mock) return mock.name;
    try {
      const res   = await API.getAvailableGames(String(appid));
      const match = (res.games || []).find(g => String(g.appid) === String(appid));
      return match?.name || null;
    } catch { return null; }
  }

  async function _runInstall(appid, skipRestart = false, knownName = null) {
    if (_installing) return;
    _installing = true;

    let name = knownName || `Game_${appid}`;

    _showProgressCard(appid, name);

    if (!knownName) {
      _getGameName(appid).then(resolved => {
        if (!resolved || resolved === name) return;
        name = resolved;
        const nameEl = document.getElementById('install-game-name');
        if (nameEl) nameEl.textContent = resolved;
      }).catch(() => {});
    }

    const installBtn = document.getElementById('install-btn');
    if (installBtn) {
      installBtn.disabled = true;
      installBtn.innerHTML = '<div class="spinner spinner--sm spinner--white"></div> Installing...';
    }

    renderInstallSteps('install-steps', API.INSTALL_STEPS);
    document.getElementById('install-progress-container').style.display = 'block';
    document.getElementById('install-log').style.display = 'block';
    document.getElementById('install-log').innerHTML = '';

    const badge = document.getElementById('install-progress-badge');
    if (badge) { badge.className = 'badge badge--accent'; badge.textContent = 'In Progress'; }

    try {
      const result = await API.installGame(appid, {
        onStep:    (id, status) => updateInstallStep(id, status),
        onLog:     (msg, type) => appendLog('install-log', msg, type),
        onProgress:(pct, label) => setProgress('progress-bar-fill', pct, 'progress-pct', `${pct}%`),
        skipRestart,
      });

      if (result.ok) {
        const finalName = result.game || name;
        if (finalName !== name) {
          const nameEl = document.getElementById('install-game-name');
          if (nameEl) nameEl.textContent = finalName;
        }
        if (badge) { badge.className = 'badge badge--green'; badge.textContent = 'Completed'; }
        Toast.success('Game Installed', `${finalName} has been installed successfully`);
        State.addInstalled(appid, finalName);
        State.addLog(`Installed: ${finalName} (ID: ${appid})`, 'success');
        Auth.refreshUsage();

        _checkOnlineFix(finalName, appid);

        _checkGameFix(finalName, appid);

        const autoRestart = State.getSetting('auto_restart');
        if (!autoRestart) {
          appendLog('install-log', 'You can restart Steam manually from the dashboard.', 'info');
        }
      } else {
        if (badge) { badge.className = 'badge badge--red'; badge.textContent = 'Failed'; }
        Toast.error('Installation Failed', result.error || 'An error occurred');
        State.addLog(`Install failed: ${name} (ID: ${appid})`, 'error');
      }
    } catch (err) {
      if (badge) { badge.className = 'badge badge--red'; badge.textContent = 'Error'; }
      Toast.error('Installation Error', err.message);
    } finally {
      _installing = false;
      if (installBtn) {
        installBtn.disabled = false;
        _updateInstallBtnLabel();
      }
    }
  }

  function _showProgressCard(appid, name) {
    const card = document.getElementById('install-progress-card');
    if (card) card.style.display = 'block';

    const nameEl = document.getElementById('install-game-name');
    const idEl   = document.getElementById('install-game-id');
    if (nameEl) nameEl.textContent = name;
    if (idEl)   idEl.textContent   = `AppID: ${appid}`;

    card?.scrollIntoView({ behavior: 'smooth', block: 'start' });
  }

  async function _checkOnlineFix(gameName, appid) {
    try {
      const res = await API.getFixes();
      if (!res.ok || !res.fixes?.length) return;
      const q = gameName.toLowerCase().replace(/[^a-z0-9]/g, '');
      if (q.length < 3) return;
      const match = res.fixes.find(f => {
        const fn = (f.name || '').toLowerCase().replace(/[^a-z0-9]/g, '');
        return fn.length >= 3 && (fn.includes(q) || (q.length >= 5 && q.includes(fn)));
      });
      if (!match) return;
      Modal.open({
        titleText: 'Online Fix Available',
        bodyHtml: `
          <p style="margin-bottom:12px;color:var(--text-2);font-size:.9rem">
            A online fix for <strong>${escapeHtml(match.name)}</strong> is available.
          </p>
          <p style="margin-bottom:16px;color:var(--text-3);font-size:.82rem;line-height:1.5">
            Install the game via Steam first, then install the online fix from the
            <strong>Online Fixes</strong> category.
          </p>
          <div style="display:flex;gap:8px">
            <button class="btn btn-primary" onclick="App.navigateTo('fixes');Modal.close()">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              Go to Online Fixes
            </button>
            <button class="btn btn-ghost" onclick="Modal.close()">Later</button>
          </div>
        `,
        noFooter: true,
      });
    } catch {

    }
  }

  async function _checkGameFix(gameName, appid) {
    try {
      const res = await API.getGameFixes();
      if (!res.ok || !res.fixes?.length) return;
      const q = (gameName || '').toLowerCase().replace(/[^a-z0-9]/g, '');
      const match = res.fixes.find(f => {
        if (appid && f.appid && String(f.appid) === String(appid)) return true;
        if (q.length < 3) return false;
        const fn = (f.name || '').toLowerCase().replace(/[^a-z0-9]/g, '');
        return fn.length >= 3 && (fn.includes(q) || (q.length >= 5 && q.includes(fn)));
      });
      if (!match) return;
      const badge = (match.badges && match.badges[0]) || '';
      Modal.open({
        titleText: 'Game Fix Available',
        bodyHtml: `
          <p style="margin-bottom:12px;color:var(--text-2);font-size:.9rem">
            A game fix${badge ? ' (<strong>' + escapeHtml(badge) + '</strong>)' : ''} for
            <strong>${escapeHtml(match.name)}</strong> is available.
          </p>
          <p style="margin-bottom:16px;color:var(--text-3);font-size:.82rem;line-height:1.5">
            Install it from the <strong>Game Fixes</strong> category to apply the
            bypass / online / hypervisor patch to the game.
          </p>
          <div style="display:flex;gap:8px">
            <button class="btn btn-primary" onclick="App.navigateTo('gamefixes');Modal.close()">
              <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
              Go to Game Fixes
            </button>
            <button class="btn btn-ghost" onclick="Modal.close()">Later</button>
          </div>
        `,
        noFooter: true,
      });
    } catch {

    }
  }

  function _setupManifestUI() {
    const input    = document.getElementById('manifest-file-input');
    const dropZone = document.getElementById('manifest-drop-zone');
    if (!input || !dropZone) return;

    input.addEventListener('change', async () => {
      if (input.files?.length) {
        await _addFiles(Array.from(input.files));
        input.value = '';
      }
    });

    dropZone.addEventListener('click', () => input.click());

    dropZone.addEventListener('dragover', e => {
      e.preventDefault();
      dropZone.classList.add('drag-over');
    });
    dropZone.addEventListener('dragleave', () => {
      dropZone.classList.remove('drag-over');
    });
    dropZone.addEventListener('drop', async e => {
      e.preventDefault();
      dropZone.classList.remove('drag-over');
      const files = e.dataTransfer?.files;
      if (files?.length) await _addFiles(Array.from(files));
    });

    document.getElementById('install-manifest-btn')?.addEventListener('click', () => _startManifestInstall());
  }

  async function _addFiles(files) {
    for (const f of files) {
      const name = f.name || '';
      const low = name.toLowerCase();

      if (low.endsWith('.zip') || low.endsWith('.manifest') || low.endsWith('.lua')) {

        if (_manifestFiles.some(ex => ex.name === name)) continue;

        try {
          const buf = await f.arrayBuffer();
          const blob = new Blob([buf], { type: f.type || 'application/octet-stream' });
          _manifestFiles.push({ name, size: f.size || buf.byteLength, data: blob });
        } catch {
          continue;
        }
      }
    }
    _renderFileList();
    _updateManifestBtn();
  }

  function _fmtSize(bytes) {
    if (!bytes) return '';
    if (bytes >= 1073741824) return (bytes / 1073741824).toFixed(2) + ' GB';
    if (bytes >= 1048576) return (bytes / 1048576).toFixed(1) + ' MB';
    if (bytes >= 1024) return Math.round(bytes / 1024) + ' KB';
    return bytes + ' B';
  }

  function _renderFileList() {
    const container = document.getElementById('manifest-file-list');
    if (!container) return;
    if (_manifestFiles.length === 0) {
      container.style.display = 'none';
      container.innerHTML = '';
      return;
    }
    container.style.display = 'flex';

    const zipCount = _manifestFiles.filter(f => f.name.toLowerCase().endsWith('.zip')).length;
    const manCount = _manifestFiles.filter(f => f.name.toLowerCase().endsWith('.manifest')).length;
    const luaCount = _manifestFiles.filter(f => f.name.toLowerCase().endsWith('.lua')).length;

    const parts = [];
    if (zipCount) parts.push(`${zipCount} .zip`);
    if (manCount) parts.push(`${manCount} .manifest`);
    if (luaCount) parts.push(`${luaCount} .lua`);

    const totalBytes = _manifestFiles.reduce((s, f) => s + (f.size || 0), 0);
    const size = _fmtSize(totalBytes);
    const label = `${_manifestFiles.length} file${_manifestFiles.length > 1 ? 's' : ''} ready · ${size} · ${parts.join(', ')}`;

    container.innerHTML = `
      <div class="mf-head">
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="2.5" style="flex-shrink:0"><polyline points="20 6 9 17 4 12"/></svg>
        <span class="mf-head-summary" title="${_manifestFiles.map(f => escapeHtml(f.name)).join('\n')}">${escapeHtml(label)}</span>
        <button class="btn-icon btn-icon--sm" id="manifest-files-clear" title="Clear all" style="flex-shrink:0">×</button>
      </div>
    `;

    document.getElementById('manifest-files-clear')?.addEventListener('click', () => {
      _manifestFiles = [];
      _renderFileList();
      _updateManifestBtn();
    });
  }

  function _updateManifestBtn() {
    const btn = document.getElementById('install-manifest-btn');
    if (!btn) return;
    btn.disabled = _manifestFiles.length === 0;
  }

  async function _startManifestInstall() {
    if (_installing || _manifestFiles.length === 0) return;
    _installing = true;

    const progressCard = document.getElementById('install-progress-card');
    if (progressCard) progressCard.style.display = 'block';
    document.getElementById('install-game-name').textContent = 'Manual Manifest';
    document.getElementById('install-game-id').textContent   = `${_manifestFiles.length} file(s)`;
    document.getElementById('install-steps')?.style.setProperty('display', 'none');
    document.getElementById('install-progress-container')?.style.setProperty('display', 'none');

    const badge = document.getElementById('install-progress-badge');
    if (badge) { badge.className = 'badge badge--accent'; badge.textContent = 'Installing'; }

    const btn = document.getElementById('install-manifest-btn');
    if (btn) {
      btn.disabled = true;
      btn.innerHTML = '<div class="spinner spinner--sm spinner--white"></div> Installing...';
    }

    const logEl = document.getElementById('install-log');
    if (logEl) {
      logEl.style.display = 'block';
      logEl.innerHTML = '';
    }
    appendLog('install-log', 'Starting manual manifest installation...', 'info');

    try {
      const result = await API.installFromManifest(_manifestFiles);

      if (result.ok && result.installed?.length) {
        for (const g of result.installed) {
          appendLog('install-log', `Installed: ${g.game} (AppID: ${g.appid})`, 'success');
          State.addInstalled(g.appid, g.game);
          State.addLog(`Installed: ${g.game} (ID: ${g.appid}) from manifest`, 'success');
        }
        const names = result.installed.map(g => g.game).join(', ');
        Toast.success('Installation Complete', `Installed: ${names}`);
        if (badge) { badge.className = 'badge badge--green'; badge.textContent = 'Completed'; }
        _manifestFiles = [];
        _renderFileList();
      } else {
        appendLog('install-log', `Error: ${result.error || 'Unknown error'}`, 'error');
        Toast.error('Installation Failed', result.error || 'Unknown error');
        if (badge) { badge.className = 'badge badge--red'; badge.textContent = 'Failed'; }
      }
    } catch (err) {
      appendLog('install-log', `Error: ${err.message}`, 'error');
      Toast.error('Installation Error', err.message);
      if (badge) { badge.className = 'badge badge--red'; badge.textContent = 'Error'; }
    } finally {
      _installing = false;
      if (btn) {
        btn.disabled = false;
        btn.innerHTML = `<svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg> Install from Manifest`;
      }
    }
  }

  function _setupQuickSearch() {
    const input   = document.getElementById('install-quick-search');
    const results = document.getElementById('install-quick-results');
    if (!input || !results) return;

    const search = debounce(async () => {
      const q = input.value.trim();
      if (!q) {
        results.innerHTML = `<div class="empty-state small"><p>Type to search available games</p></div>`;
        return;
      }

      let games = [];
      try {
        const res = await API.getAvailableGames(q);
        games = (res.games || []).slice(0, 8);
      } catch {
        games = API.searchGames(q).slice(0, 8);
      }

      if (games.length === 0) {
        results.innerHTML = `<div class="empty-state small"><p>No games found for "<strong>${escapeHtml(q)}</strong>"</p></div>`;
        return;
      }

      results.innerHTML = games.map(g => {
        const installed = State.isInstalled(g.appid);
        return `
          <div class="quick-result-item" data-appid="${g.appid}" data-name="${escapeHtml(g.name)}">
            <span class="quick-result-name">${escapeHtml(g.name)}</span>
            <div style="display:flex;align-items:center;gap:6px">
              <span class="quick-result-id">${g.appid}</span>
              ${installed ? badgeHtml('Installed', 'green') : ''}
            </div>
          </div>
        `;
      }).join('');

      results.querySelectorAll('.quick-result-item').forEach(item => {
        item.addEventListener('click', () => {
          const appid    = item.dataset.appid;
          const gameName = item.dataset.name;
          const appInput = document.getElementById('install-appid-input');
          if (appInput) appInput.value = appid;
          input.value = '';
          results.innerHTML = `<div class="empty-state small"><p>Type to search available games</p></div>`;
          if (DownloadQueue.isEnabled()) {
            DownloadQueue.enqueue(appid, gameName, 'install');
            return;
          }
          _runInstall(appid, false, gameName);
        });
      });
    }, 300);

    input.addEventListener('input', search);
  }

  function installFromCatalog(appid, gameName) {
    if (DownloadQueue.isEnabled()) {
      DownloadQueue.enqueue(String(appid), gameName || null, 'install');
      return;
    }
    App.navigateTo('install');
    setTimeout(() => _runInstall(String(appid), false, gameName || null), 100);
  }

  window.addEventListener('statechange', e => {
    if (e.detail.area === 'settings') _updateInstallBtnLabel();
  });

  async function startUpdate(appid, gameName) {
    const tier = Auth.getState().tier || 'standard';
    if (tier === 'standard') {
      Toast.error('Premium Required', 'Game updates are a Premium feature. Upgrade your plan to unlock it.');
      return;
    }

    if (DownloadQueue.isEnabled()) {
      DownloadQueue.enqueue(String(appid), gameName || null, 'update');
      return;
    }

    App.navigateTo('install');
    await new Promise(r => setTimeout(r, 100));

    if (_installing) return;
    _installing = true;

    const name = gameName || await _getGameName(String(appid)) || `Game_${appid}`;

    _showProgressCard(String(appid), name);

    const installBtn = document.getElementById('install-btn');
    if (installBtn) {
      installBtn.disabled = true;
      installBtn.innerHTML = '<div class="spinner spinner--sm spinner--white"></div> Updating...';
    }

    const UPDATE_STEPS = API.INSTALL_STEPS.filter(s => s.id !== 'stop' && s.id !== 'restart');
    renderInstallSteps('install-steps', UPDATE_STEPS);
    document.getElementById('install-progress-container').style.display = 'block';
    document.getElementById('install-log').style.display = 'block';
    document.getElementById('install-log').innerHTML = '';

    const badge = document.getElementById('install-progress-badge');
    if (badge) { badge.className = 'badge badge--accent'; badge.textContent = 'Updating'; }

    try {
      const result = await API.updateGame(String(appid), {
        onStep:     (id, status) => updateInstallStep(id, status),
        onLog:      (msg, type)  => appendLog('install-log', msg, type),
        onProgress: (pct, label) => setProgress('progress-bar-fill', pct, 'progress-pct', `${pct}%`),
      });

      if (result.premium_required) {
        if (badge) { badge.className = 'badge badge--red'; badge.textContent = 'Premium Required'; }
        Toast.error('Premium Required', result.error || 'Game updates require a Premium account.');
      } else if (result.ok) {
        if (badge) { badge.className = 'badge badge--green'; badge.textContent = 'Updated'; }
        Toast.success('Game Updated', `${result.game || name} has been updated successfully`);
        Auth.refreshUsage();
      } else {
        if (badge) { badge.className = 'badge badge--red'; badge.textContent = 'Failed'; }
        Toast.error('Update Failed', result.error || 'An error occurred');
      }
    } catch (err) {
      if (badge) { badge.className = 'badge badge--red'; badge.textContent = 'Error'; }
      Toast.error('Update Error', err.message);
    } finally {
      _installing = false;
      if (installBtn) {
        installBtn.disabled = false;
        _updateInstallBtnLabel();
      }
    }
  }

  return { init, installFromCatalog, startUpdate };
})();
