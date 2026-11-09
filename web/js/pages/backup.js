

const PageBackup = (() => {
  let _importedGames = [];

  function init() {
    _updateExportInfo().catch(() => {});
    _bindActions();
  }

  async function _updateExportInfo() {
    const el = document.getElementById('export-game-count');
    try {
      const records = await API.getInstalledGames();
      const count   = Object.keys(records).length;
      if (el) el.textContent = count;
    } catch {
      if (el) el.textContent = '0';
    }
  }

  function _bindActions() {

    document.getElementById('export-btn')?.addEventListener('click', _doExport);

    document.getElementById('import-browse-btn')?.addEventListener('click', () => {
      document.getElementById('import-file-input')?.click();
    });

    document.getElementById('import-file-input')?.addEventListener('change', e => {
      const file = e.target.files?.[0];
      if (file) _readImportFile(file);
    });

    const dropZone = document.getElementById('import-drop-zone');
    if (dropZone) {
      dropZone.addEventListener('dragover', e => {
        e.preventDefault();
        dropZone.classList.add('drag-over');
      });
      dropZone.addEventListener('dragleave', () => dropZone.classList.remove('drag-over'));
      dropZone.addEventListener('drop', e => {
        e.preventDefault();
        dropZone.classList.remove('drag-over');
        const file = e.dataTransfer.files?.[0];
        if (file) _readImportFile(file);
      });
      dropZone.addEventListener('click', () => {
        document.getElementById('import-file-input')?.click();
      });
    }

    document.getElementById('import-clear')?.addEventListener('click', _clearImport);

    document.getElementById('import-install-btn')?.addEventListener('click', _startReinstall);
  }

  async function _doExport() {
    let json, count;
    try {
      const records = await API.getInstalledGames();
      count = Object.keys(records).length;
      json  = await API.exportGamesJson();
    } catch(e) {
      Toast.error('Export failed', e.message);
      return;
    }

    if (count === 0) {
      Toast.warning('Nothing to export', 'No games are currently installed');
      return;
    }

    const date     = new Date().toISOString().slice(0, 10);
    const filename = `SteamTools-games-${date}.json`;

    if (window.showSaveFilePicker) {
      try {
        const handle = await window.showSaveFilePicker({
          suggestedName: filename,
          types: [{ description: 'JSON Files', accept: { 'application/json': ['.json'] } }],
        });
        const writable = await handle.createWritable();
        await writable.write(json);
        await writable.close();
        Toast.success('Exported', `${count} games saved`);
        State.addLog(`Exported ${count} games to ${filename}`, 'success');
        return;
      } catch(e) {
        if (e.name === 'AbortError') return;

      }
    }

    downloadJSON(filename, json);
    Toast.success('Exported', `${count} games exported to ${filename}`);
    State.addLog(`Exported ${count} games to ${filename}`, 'success');
  }

  function _readImportFile(file) {
    if (!file.name.endsWith('.json') && file.type !== 'application/json') {
      Toast.error('Invalid file', 'Please select a JSON file');
      return;
    }

    const reader = new FileReader();
    reader.onload = e => {
      const result = State.importInstalledJSON(e.target.result);
      if (!result.ok) {
        Toast.error('Invalid file', result.error || 'Could not parse the file');
        return;
      }
      _importedGames = result.games;
      _showImportPreview(file.name, result.games);
    };
    reader.readAsText(file);
  }

  function _showImportPreview(filename, games) {
    const dropZone = document.getElementById('import-drop-zone');
    const preview  = document.getElementById('import-preview');
    const fileEl   = document.getElementById('import-filename');
    const list     = document.getElementById('import-game-list');
    const btn      = document.getElementById('import-install-btn');

    if (dropZone) dropZone.style.display = 'none';
    if (preview)  preview.style.display  = 'block';
    if (fileEl)   fileEl.textContent     = filename;

    if (list) {
      list.innerHTML = games.map(g => `
        <div class="import-game-item">
          <span class="import-game-name">${escapeHtml(g.name)}</span>
          <span class="import-game-id">${g.appid}</span>
        </div>
      `).join('');
    }

    if (btn) {
      btn.innerHTML = `
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
          <polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/>
        </svg>
        Reinstall ${games.length} Game${games.length !== 1 ? 's' : ''}
      `;
    }
  }

  function _clearImport() {
    _importedGames = [];
    const dropZone = document.getElementById('import-drop-zone');
    const preview  = document.getElementById('import-preview');
    const fileInput = document.getElementById('import-file-input');
    if (dropZone) dropZone.style.display = 'flex';
    if (preview)  preview.style.display  = 'none';
    if (fileInput) fileInput.value = '';
  }

  async function _startReinstall() {
    if (_importedGames.length === 0) return;

    const confirmed = await Modal.open({
      titleText:   'Reinstall Games',
      bodyHtml:    `<p>Reinstall <strong>${_importedGames.length} games</strong> from the backup? Each game will be downloaded and installed sequentially.</p>`,
      confirmText: 'Reinstall All',
      cancelText:  'Cancel',
    });

    if (!confirmed) return;

    const card = document.getElementById('reinstall-progress-card');
    const body = document.getElementById('reinstall-progress-body');
    const badge = document.getElementById('reinstall-badge');
    if (card) card.style.display = 'block';
    if (badge) { badge.className = 'badge badge--accent'; badge.textContent = 'Running'; }

    card?.scrollIntoView({ behavior: 'smooth', block: 'start' });

    if (body) {
      body.className = 'card-body reinstall-progress-list';
      body.innerHTML = _importedGames.map(g => `
        <div class="reinstall-entry" id="reinstall-${g.appid}">
          <div class="spinner spinner--sm" id="reinstall-spin-${g.appid}" style="display:none"></div>
          <span id="reinstall-icon-${g.appid}">○</span>
          <span class="reinstall-game-name">${escapeHtml(g.name)}</span>
          <span class="reinstall-status" id="reinstall-status-${g.appid}">Waiting</span>
        </div>
      `).join('');
    }

    let success = 0;
    let failed  = 0;
    let isFirst = true;

    for (const game of _importedGames) {

      if (!isFirst) await new Promise(r => setTimeout(r, 2000));
      isFirst = false;

      const el     = document.getElementById(`reinstall-${game.appid}`);
      const spin   = document.getElementById(`reinstall-spin-${game.appid}`);
      const icon   = document.getElementById(`reinstall-icon-${game.appid}`);
      const status = document.getElementById(`reinstall-status-${game.appid}`);

      if (el) el.className = 'reinstall-entry active';
      if (spin) spin.style.display = 'block';
      if (icon) icon.style.display = 'none';
      if (status) status.textContent = 'Installing...';

      let result = await API.installGame(game.appid, {
        onLog: (msg, type) => State.addLog(msg, type),
        skipRestart: true,
      });

      if (!result.ok && result.rate_limited) {
        if (status) status.textContent = 'Retrying…';
        await new Promise(r => setTimeout(r, 3000));
        result = await API.installGame(game.appid, {
          onLog: (msg, type) => State.addLog(msg, type),
          skipRestart: true,
        });
      }

      if (spin) spin.style.display = 'none';
      if (icon) icon.style.display = '';

      if (result.ok) {
        success++;
        if (el) el.className = 'reinstall-entry done';
        if (icon) icon.textContent = '✓';
        if (status) status.textContent = 'Done';
        Auth.refreshUsage();
      } else {
        failed++;
        if (el) el.className = 'reinstall-entry error';
        if (icon) icon.textContent = '✗';
        if (status) {
          status.textContent = result.error || 'Failed';
          status.title = result.error || '';
        }
      }
    }

    if (badge) {
      badge.className = failed === 0 ? 'badge badge--green' : 'badge badge--yellow';
      badge.textContent = failed === 0 ? 'Complete' : 'Partial';
    }

    const msg = failed === 0
      ? `All ${success} games reinstalled successfully`
      : `${success} installed, ${failed} failed`;

    Toast[failed === 0 ? 'success' : 'warning']('Reinstallation Done', msg);
    State.addLog(`Bulk reinstall: ${success} OK, ${failed} failed`, failed ? 'warning' : 'success');
    _clearImport();
  }

  window.addEventListener('statechange', e => {
    if (e.detail.area === 'installed') _updateExportInfo().catch(() => {});
  });

  return { init };
})();
