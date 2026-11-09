

const PageSettings = (() => {
  let _initialized = false;

  function init() {
    _loadSettings();
    if (!_initialized) {
      _bindActions();
      _initialized = true;
    }
    _checkDefaultPaths();
  }

  async function _loadSettings() {
    const s = await API.getSettings();

    const steam = API.getSteamStatus();
    const pathEl  = document.getElementById('current-steam-path-text');
    const badgeEl = document.getElementById('current-steam-path-badge');

    if (s.custom_steam_path) {
      if (pathEl) pathEl.textContent = s.custom_steam_path;
      if (badgeEl) badgeEl.innerHTML = badgeHtml('Custom', 'accent');
      document.getElementById('custom-steam-path-input').value = s.custom_steam_path;
    } else {
      if (pathEl) pathEl.textContent = steam.detected ? steam.path : 'Auto-detection...';
      if (badgeEl) badgeEl.innerHTML = steam.detected
        ? badgeHtml('Auto', 'green')
        : badgeHtml('Not found', 'red');
    }

    _setToggle('pref-animations',    s.animations);
    _setToggle('pref-compact',       s.compact);
    _setToggle('pref-auto-restart',     s.auto_restart);
    _setToggle('pref-auto-close-steam', s.auto_close_steam);
    _setToggle('pref-confirm-remove',   s.confirm_remove);
    _setToggle('pref-image-cache',      s.image_cache);
    _setToggle('pref-auto-install-ost', s.auto_install_ost);
    _setToggle('pref-download-queue',   s.download_queue);

    _setTheme(s.theme || 'default');

    const providerRadio = document.querySelector(`input[name="provider"][value="${s.provider || 'default'}"]`);
    if (providerRadio) providerRadio.checked = true;
    const hubcapSection = document.getElementById('hubcap-settings');
    if (hubcapSection) hubcapSection.style.display = (s.provider === 'hubcap') ? 'block' : 'none';
    const hubcapInput = document.getElementById('hubcap-api-key-input');
    if (hubcapInput && s.hubcap_api_key) hubcapInput.value = s.hubcap_api_key;
  }

  function _setToggle(id, value) {
    const el = document.getElementById(id);
    if (el) el.checked = !!value;
  }

  function _setTheme(theme) {
    document.documentElement.dataset.theme = theme;
    document.querySelectorAll('.theme-card').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.theme === theme);
    });
  }

  function _bindActions() {

    document.querySelectorAll('.settings-nav-item').forEach(item => {
      item.addEventListener('click', () => {
        document.querySelectorAll('.settings-nav-item').forEach(i => i.classList.remove('active'));
        document.querySelectorAll('.settings-panel').forEach(p => p.classList.remove('active'));
        item.classList.add('active');
        const panel = document.getElementById(`settings-panel-${item.dataset.settings}`);
        if (panel) panel.classList.add('active');
      });
    });

    document.getElementById('validate-steam-path-btn')?.addEventListener('click', _validatePath);

    document.getElementById('save-steam-path-btn')?.addEventListener('click', _savePath);

    document.getElementById('reset-steam-path-btn')?.addEventListener('click', _resetPath);

    ['pref-animations', 'pref-compact', 'pref-auto-restart', 'pref-auto-close-steam', 'pref-confirm-remove', 'pref-image-cache', 'pref-auto-install-ost', 'pref-download-queue'].forEach(id => {
      document.getElementById(id)?.addEventListener('change', _saveAppPrefs);
    });

    document.getElementById('clear-all-data-btn')?.addEventListener('click', _clearAllData);

    document.getElementById('clear-game-lists-btn')?.addEventListener('click', _clearGameLists);
    document.getElementById('clear-fixes-btn')?.addEventListener('click', _clearFixes);
    document.getElementById('clear-image-cache-btn')?.addEventListener('click', _clearImageCache);
    document.getElementById('clear-local-storage-btn')?.addEventListener('click', _clearLocalStorage);

    document.querySelectorAll('.theme-card').forEach(btn => {
      btn.addEventListener('click', async () => {
        const theme = btn.dataset.theme;
        _setTheme(theme);
        await API.saveSettings({ theme });
      });
    });

    document.getElementById('custom-steam-path-input')?.addEventListener('keydown', e => {
      if (e.key === 'Enter') _validatePath();
    });

    document.querySelectorAll('input[name="provider"]').forEach(r => {
      r.addEventListener('change', _onProviderChange);
    });

    document.getElementById('hubcap-api-key-input')?.addEventListener('change', _onHubcapKeyChange);
    document.getElementById('toggle-hubcap-key-visibility')?.addEventListener('click', _toggleHubcapKeyVisibility);
  }

  async function _validatePath() {
    const input   = document.getElementById('custom-steam-path-input');
    const resultEl = document.getElementById('path-validation-result');
    const path    = input?.value?.trim();

    if (!path) {
      if (resultEl) {
        resultEl.style.display = 'block';
        resultEl.className = 'validation-result error';
        resultEl.textContent = 'Please enter a path';
      }
      return;
    }

    if (resultEl) {
      resultEl.style.display = 'block';
      resultEl.className = 'validation-result';
      resultEl.textContent = 'Validating...';
    }

    const v = await API.validateSteamPath(path);

    if (resultEl) {
      if (v.ok) {
        resultEl.className = 'validation-result success';
        resultEl.innerHTML = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" style="margin-right:4px"><polyline points="20 6 9 17 4 12"/></svg> Valid: ${escapeHtml(v.path)}`;
      } else {
        resultEl.className = 'validation-result error';
        resultEl.innerHTML = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" style="margin-right:4px"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg> ${escapeHtml(v.error || 'Invalid path')}`;
      }
    }
  }

  async function _savePath() {
    const input = document.getElementById('custom-steam-path-input');
    const path  = input?.value?.trim();

    if (!path) {
      Toast.warning('Empty path', 'Please enter a Steam path or use Reset');
      return;
    }

    const v = await API.validateSteamPath(path);
    if (!v.ok) {
      Toast.error('Invalid path', v.error || 'steam.exe not found at this location');
      return;
    }

    await API.saveSettings({ custom_steam_path: v.path });
    API.invalidateSteamCache();
    await _loadSettings();
    Toast.success('Path Saved', `Custom Steam path: ${v.path}`);
    State.addLog(`Custom Steam path set: ${v.path}`, 'info');
  }

  async function _resetPath() {
    await API.saveSettings({ custom_steam_path: '' });
    API.invalidateSteamCache();

    const input   = document.getElementById('custom-steam-path-input');
    const resultEl = document.getElementById('path-validation-result');
    if (input)   input.value = '';
    if (resultEl) resultEl.style.display = 'none';

    _loadSettings();
    Toast.success('Path Reset', 'Back to automatic Steam detection');
    State.addLog('Custom Steam path removed — using auto-detection', 'info');
  }

  async function _saveAppPrefs(e) {
    const changedId = e?.target?.id;

    const ostEl     = document.getElementById('pref-auto-install-ost');
    const restartEl = document.getElementById('pref-auto-restart');
    const closeEl   = document.getElementById('pref-auto-close-steam');
    const queueEl   = document.getElementById('pref-download-queue');

    if (changedId === 'pref-download-queue' && queueEl?.checked) {
      const tier = Auth.getState()?.tier || 'standard';
      if (!['premium1', 'premium2'].includes(tier)) {
        queueEl.checked = false;
        Toast.error('Premium Required', 'The Download Queue is a Premium feature. Upgrade your plan to unlock it.');
        return;
      }
      DownloadQueue.init();
    } else if (changedId === 'pref-download-queue' && !queueEl?.checked) {

      DownloadQueue.clearFinished();
    }

    if (changedId === 'pref-auto-install-ost' && ostEl?.checked) {
      if (restartEl) restartEl.checked = true;
      if (closeEl)   closeEl.checked   = true;
    } else if (changedId === 'pref-auto-restart' || changedId === 'pref-auto-close-steam') {
      if ((restartEl && !restartEl.checked) || (closeEl && !closeEl.checked)) {
        if (ostEl) ostEl.checked = false;
      }
    }

    await API.saveSettings({
      animations:     document.getElementById('pref-animations')?.checked    ?? true,
      compact:        document.getElementById('pref-compact')?.checked       ?? false,
      auto_restart:     restartEl?.checked     ?? false,
      auto_close_steam: closeEl?.checked       ?? false,
      confirm_remove:   document.getElementById('pref-confirm-remove')?.checked   ?? true,
      image_cache:      document.getElementById('pref-image-cache')?.checked      ?? true,
      auto_install_ost: ostEl?.checked         ?? false,
      download_queue:   queueEl?.checked       ?? false,
    });
    Toast.info('Settings saved', '', 1500);
  }

  async function _clearAllData() {
    const confirmed = await Modal.open({
      titleText:   'Clear All Data',
      bodyHtml:    '<p>This will permanently clear all application data including install records, settings, and activity logs. Are you sure?</p>',
      confirmText: 'Clear All',
      cancelText:  'Cancel',
      danger:      true,
    });
    if (!confirmed) return;

    await fetch('/api/clear-game-caches', { method: 'POST' }).catch(() => {});
    API.invalidateGamesCache();
    State.clearAll();
    _loadSettings();
    Toast.success('Data Cleared', 'All application data and game caches have been reset');
  }

  async function _clearGameLists() {
    const confirmed = await Modal.open({
      titleText:   'Clear Game Lists',
      bodyHtml:    '<p>This will remove all cached game catalogs (Hubcap, DepotBox, SteamTools, Ryuu) and trending data. They will be re-fetched on next load.</p>',
      confirmText: 'Clear',
      cancelText:  'Cancel',
      danger:      true,
    });
    if (!confirmed) return;
    await fetch('/api/clear/game-lists', { method: 'POST' }).catch(() => {});
    API.invalidateGamesCache();
    Toast.success('Cleared', 'Game lists and catalog caches cleared');
  }

  async function _clearFixes() {
    const confirmed = await Modal.open({
      titleText:   'Clear Game Fixes',
      bodyHtml:    '<p>This will remove cached game fixes and fix metadata. They will be re-fetched on next load.</p>',
      confirmText: 'Clear',
      cancelText:  'Cancel',
      danger:      true,
    });
    if (!confirmed) return;
    await fetch('/api/clear/fixes', { method: 'POST' }).catch(() => {});
    Toast.success('Cleared', 'Game fixes cache cleared');
  }

  async function _clearImageCache() {
    const confirmed = await Modal.open({
      titleText:   'Clear Image Cache',
      bodyHtml:    '<p>This will remove all downloaded game cover images. They will be re-downloaded when needed.</p>',
      confirmText: 'Clear',
      cancelText:  'Cancel',
      danger:      true,
    });
    if (!confirmed) return;
    await fetch('/api/clear/image-cache', { method: 'POST' }).catch(() => {});
    Toast.success('Cleared', 'Image cache cleared');
  }

  async function _clearLocalStorage() {
    const confirmed = await Modal.open({
      titleText:   'Clear UI Preferences',
      bodyHtml:    '<p>This will reset your theme, sidebar state, and other display settings to defaults.</p>',
      confirmText: 'Clear',
      cancelText:  'Cancel',
      danger:      true,
    });
    if (!confirmed) return;
    State.clearAll();
    _loadSettings();
    Toast.success('Cleared', 'UI preferences reset to defaults');
  }

  function _checkDefaultPaths() {

    setTimeout(() => {
      const steam = API.getSteamStatus();
      const badges = [
        document.getElementById('path-check-1'),
        document.getElementById('path-check-2'),
        document.getElementById('path-check-3'),
      ];

      badges.forEach((badge, i) => {
        if (!badge) return;
        if (steam.detected && i === 0) {
          badge.innerHTML = badgeHtml('Found', 'green');
        } else {
          badge.innerHTML = badgeHtml('Not found', 'neutral');
        }
      });
    }, 600);
  }

  async function _onProviderChange() {
    const provider = document.querySelector('input[name="provider"]:checked')?.value || 'default';
    const hubcapSection = document.getElementById('hubcap-settings');
    if (hubcapSection) hubcapSection.style.display = (provider === 'hubcap') ? 'block' : 'none';
    await API.saveSettings({ provider });
    State.addLog(`Provider changed to: ${provider}`, 'info');
  }

  async function _onHubcapKeyChange() {
    const input = document.getElementById('hubcap-api-key-input');
    if (!input) return;
    await API.saveSettings({ hubcap_api_key: input.value });
    Toast.info('Hubcap API key saved', '', 1500);
  }

  function _toggleHubcapKeyVisibility() {
    const input = document.getElementById('hubcap-api-key-input');
    const btn   = document.getElementById('toggle-hubcap-key-visibility');
    if (!input || !btn) return;
    const isPassword = input.type === 'password';
    input.type = isPassword ? 'text' : 'password';
    btn.textContent = isPassword ? 'Hide' : 'Show';
  }

  return { init };
})();
