

const DownloadQueue = (() => {
  const MAX_ITEMS = 50;
  const DOWNLOAD_COOLDOWN = 3000;
  let _initialized = false;
  let _processing  = false;

  function isEnabled() {
    const tier = Auth.getState()?.tier || 'standard';
    return State.getSetting('download_queue') === true &&
           ['premium1', 'premium2'].includes(tier);
  }

  function getItems() { return State.getQueue(); }

  function _pendingCount() {
    return getItems().filter(i => i.status === 'queued' || i.status === 'downloading').length;
  }

  function init() {
    if (_initialized) return;
    _initialized = true;

    const items = getItems();
    let changed = false;
    items.forEach(i => {
      if (i.status === 'downloading') { i.status = 'queued'; i.progress = 0; changed = true; }
    });
    if (changed) State.setQueue(items);

    render();
    _updateFloatBar();
  }

  function enqueue(appid, name, type = 'install') {
    if (!isEnabled()) return false;

    appid = String(appid);
    const items = getItems();
    if (items.some(i => i.appid === appid && (i.status === 'queued' || i.status === 'downloading'))) {
      Toast.info('Already in queue', `${name || `Game_${appid}`} is already queued`);
      return true;
    }
    if (items.length >= MAX_ITEMS) {
      Toast.warning('Queue full', 'Clear finished items to add more');
      return true;
    }

    const pending = _pendingCount();
    items.push({ appid, name: name || `Game_${appid}`, type, status: 'queued', progress: 0, error: '' });
    State.setQueue(items);
    Toast.success('Added to Queue', `${name || `Game_${appid}`} queued (${pending + 1} pending)`);
    render();
    _updateFloatBar();
    return true;
  }

  function remove(appid) {
    State.removeFromQueue(appid);
    render();
    _updateFloatBar();
  }

  function clearFinished() {
    const items = getItems().filter(i => i.status === 'queued' || i.status === 'downloading');
    State.setQueue(items);
    render();
    _updateFloatBar();
  }

  async function startAll() {
    if (!isEnabled()) return;
    if (_processing) return;
    if (_pendingCount() === 0) return;
    _updateFloatBar();
    await process();
  }

  async function process() {
    if (!isEnabled()) return;
    if (_processing) return;

    const items = getItems();
    const queued = items.filter(i => i.status === 'queued');
    if (queued.length === 0) { render(); _updateFloatBar(); return; }

    _processing = true;

    queued.forEach(i => State.updateQueueItem(i.appid, { status: 'downloading', progress: 0, error: '' }));

    try {

      const installs = queued.filter(i => i.type === 'install');
      const updates  = queued.filter(i => i.type === 'update');

      if (installs.length > 0) {
        if (installs.length === 1) {

          const item = installs[0];
          const result = await API.installGame(item.appid, {
            onLog: (msg, type) => State.addLog(msg, type),
            onProgress: (pct) => _updateProgress(item.appid, pct),
          });
          _handleResult(item, result);
        } else {

          const appids = installs.map(i => i.appid);
          const result = await API.installBatch(appids, {
            onLog: (msg, type) => State.addLog(msg, type),
            onProgress: (pct) => {

              installs.forEach(i => _updateProgress(i.appid, pct));
            },
            onGameProgress: (appid, pct, label) => {
              _updateProgress(appid, pct);
            },
          });

          if (result.results) {
            result.results.forEach((r, idx) => {
              if (r) _handleResult(installs[idx], r);
            });
          } else {

            installs.forEach(i => {
              if (result.ok) {
                _handleResult(i, { ok: true, game: i.name });
              } else {
                _handleResult(i, { ok: false, error: result.error || 'Batch failed' });
              }
            });
          }
        }
      }

      for (const item of updates) {
        const result = await API.updateGame(item.appid, {
          onLog: (msg, type) => State.addLog(msg, type),
          onProgress: (pct) => _updateProgress(item.appid, pct),
        });
        _handleResult(item, result);
      }

      Auth.refreshUsage();
    } catch (e) {

      getItems().filter(i => i.status === 'downloading').forEach(i => {
        State.updateQueueItem(i.appid, { status: 'failed', error: e.message || 'Error' });
        State.addLog(`Queue error: ${i.name} — ${e.message || 'Unknown'}`, 'error');
      });
      Toast.error('Queue Error', e.message || 'An error occurred');
    } finally {
      _processing = false;
      render();
      _updateFloatBar();
    }
  }

  function _handleResult(item, result) {
    if (result.ok) {
      State.updateQueueItem(item.appid, { status: 'done', progress: 100 });
      const finalName = result.game || item.name;
      if (item.type === 'install') {
        State.addInstalled(item.appid, finalName);
        State.addLog(`Queue install complete: ${finalName} (ID: ${item.appid})`, 'success');
      } else {
        State.addLog(`Queue update complete: ${finalName} (ID: ${item.appid})`, 'success');
      }
      Toast.success('Queue Complete', `${finalName} finished`);
    } else {
      State.updateQueueItem(item.appid, { status: 'failed', error: result.error || 'Failed' });
      State.addLog(`Queue ${item.type}: ${item.name} failed — ${result.error || 'Unknown'}`, 'error');
      Toast.error('Queue Failed', `${item.name}: ${result.error || 'Failed'}`);
    }
  }

  let _hideTimer = null;
  function _updateFloatBar() {
    const bar = document.getElementById('queue-float-bar');
    if (!bar) return;
    const enabled = isEnabled();
    const items = getItems();
    const pending = _pendingCount();

    if (!enabled || pending === 0) {
      if (bar.style.display !== 'none') {
        if (!bar.classList.contains('leaving')) {
          bar.classList.add('leaving');
          clearTimeout(_hideTimer);
          _hideTimer = setTimeout(() => {
            bar.classList.remove('leaving');
            bar.style.display = 'none';
          }, 220);
        }
      }
      return;
    }

    if (bar.style.display === 'none' || bar.style.display === '') {
      bar.classList.remove('leaving');
      bar.style.display = 'flex';
    }

    const active = items.find(i => i.status === 'downloading');
    const countEl  = document.getElementById('queue-float-count');
    const subEl    = document.getElementById('queue-float-sub');
    const installBtn = document.getElementById('queue-float-install');

    if (active) {
      if (countEl) countEl.textContent = active.name || `Game_${active.appid}`;
      if (subEl)   subEl.textContent   = `Downloading ${Math.min(100, active.progress || 0)}% · ${pending} left`;
      if (installBtn) {
        installBtn.disabled = true;
        installBtn.innerHTML = '<div class="spinner spinner--sm spinner--white"></div> Downloading...';
      }
    } else {
      if (countEl) countEl.textContent = `${pending} game${pending === 1 ? '' : 's'} queued`;
      if (subEl)   subEl.textContent   = 'Click Install to download everything';
      if (installBtn) {
        installBtn.disabled = false;
        installBtn.innerHTML = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" stroke-linejoin="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg> Install`;
      }
    }
  }

  function _bindFloatBar() {
    const installBtn = document.getElementById('queue-float-install');
    installBtn?.addEventListener('click', startAll);

    const closeBtn = document.getElementById('queue-float-close');
    closeBtn?.addEventListener('click', () => {
      const bar = document.getElementById('queue-float-bar');
      if (!bar || bar.style.display === 'none') return;
      bar.classList.add('leaving');
      clearTimeout(_hideTimer);
      _hideTimer = setTimeout(() => {
        bar.classList.remove('leaving');
        bar.style.display = 'none';
      }, 220);
    });
  }

  function _updateProgress(appid, pct) {
    const items = getItems().map(it => it.appid === String(appid) ? { ...it, progress: pct } : it);
    State.setQueue(items);
    _updateFloatBar();
    const id = String(appid).replace(/["\\]/g, '');
    const itemEl = document.querySelector(`.queue-item[data-appid="${id}"]`);
    if (!itemEl) return;
    const fill  = itemEl.querySelector('.queue-item-fill');
    const badge = itemEl.querySelector('.queue-item-pct');
    if (fill)  fill.style.width  = `${Math.min(100, pct || 0)}%`;
    if (badge) badge.textContent = `${Math.min(100, pct || 0)}%`;
  }

  function render() {
    const card = document.getElementById('queue-card');
    if (!card) return;
    const enabled = isEnabled();
    card.style.display = enabled ? 'block' : 'none';

    const progressCard = document.getElementById('install-progress-card');
    const manifestCard = document.getElementById('install-manifest-card');
    if (progressCard) progressCard.style.display = enabled ? 'none' : '';
    if (manifestCard) manifestCard.style.display = enabled ? 'none' : '';

    const grid = document.querySelector('.install-main-grid');
    if (grid) grid.classList.toggle('queue-active', enabled);

    if (!enabled) return;

    const items  = getItems();
    const badge  = document.getElementById('queue-count-badge');
    const body   = document.getElementById('queue-body');
    const pending = items.filter(i => i.status === 'queued').length;
    if (badge) badge.textContent = `${pending} queued`;
    if (!body) return;

    if (items.length === 0) {
      body.innerHTML = `<div class="empty-state small"><p>Queue is empty — install games to add them here.</p></div>`;
      return;
    }

    const hasFinished = items.some(i => i.status === 'done' || i.status === 'failed');
    body.innerHTML = `
      <div class="queue-list">
        ${items.map(_renderItem).join('')}
      </div>
      <div style="display:flex;justify-content:flex-end;gap:8px;margin-top:10px">
        ${_pendingCount() > 0 ? `<button class="btn btn-primary btn-sm" id="queue-card-start">Download All</button>` : ''}
        <button class="btn btn-ghost-danger btn-sm" id="queue-clear-finished" style="display:${hasFinished ? '' : 'none'}">
          Clear Finished
        </button>
      </div>
    `;

    body.querySelectorAll('.queue-remove').forEach(btn => {
      btn.addEventListener('click', () => remove(btn.dataset.appid));
    });
    body.querySelector('#queue-card-start')?.addEventListener('click', startAll);
    body.querySelector('#queue-clear-finished')?.addEventListener('click', clearFinished);
  }

  function _renderItem(item) {
    const isActive = item.status === 'downloading';
    const meta = {
      queued:      { cls: 'badge--neutral', label: 'Queued' },
      downloading: { cls: 'badge--accent',  label: `${Math.min(100, item.progress || 0)}%` },
      done:        { cls: 'badge--green',   label: 'Done' },
      failed:      { cls: 'badge--red',     label: 'Failed' },
    }[item.status] || { cls: 'badge--neutral', label: 'Queued' };

    return `
      <div class="queue-item ${isActive ? 'active' : ''}" data-appid="${escapeHtml(item.appid)}">
        <div class="queue-item-info">
          <span class="queue-item-name" title="${escapeHtml(item.name)}">${escapeHtml(item.name)}</span>
          <span class="queue-item-id">${escapeHtml(item.appid)}</span>
          ${item.type === 'update' ? badgeHtml('Update', 'blue') : ''}
          <span class="badge ${meta.cls}">${item.status === 'downloading'
            ? `<span class="queue-item-pct">${Math.min(100, item.progress || 0)}%</span>`
            : meta.label}</span>
          ${item.error ? `<span class="queue-item-error" title="${escapeHtml(item.error)}">${escapeHtml(item.error)}</span>` : ''}
        </div>
        ${isActive ? `
        <div class="progress-bar-container" style="margin:8px 0 0 0">
          <div class="progress-bar"><div class="progress-bar-fill queue-item-fill" style="width:${Math.min(100, item.progress || 0)}%"></div></div>
        </div>` : ''}
        <button class="btn-icon btn-icon--sm queue-remove" data-appid="${escapeHtml(item.appid)}" title="Remove from queue" ${isActive ? 'disabled' : ''}>×</button>
      </div>
    `;
  }

  window.addEventListener('statechange', e => {
    if (e.detail.area === 'queue' || e.detail.area === 'settings') {
      render();
      _updateFloatBar();
    }
  });

  _bindFloatBar();

  return { init, isEnabled, enqueue, remove, clearFinished, startAll, getItems };
})();
