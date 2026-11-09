const App = (() => {

  const PAGES = {
    dashboard:  { title: 'Dashboard',             module: 'PageDashboard' },
    install:    { title: 'Install a Game',         module: 'PageInstall' },
    available:  { title: 'Available Games',        module: 'PageAvailable' },
    installed:  { title: 'Installed Games',        module: 'PageInstalled' },
    backup:     { title: 'Reinstall from Backup',  module: 'PageBackup' },
    achievements: { title: 'Achievements',         module: 'PageAchievements' },
    settings:   { title: 'Settings',               module: 'PageSettings' },
    diagnostic: { title: 'Diagnostic',             module: 'PageDiagnostic' },
    repair:     { title: 'Repair',                 module: 'PageRepair' },
    fixes:      { title: 'Online Fixes',                  module: 'PageFixes' },
    gamefixes:  { title: 'Game Fixes',                    module: 'PageGameFixes' },
    credits:    { title: 'Credits',                module: 'PageCredits' },
  };

  const _MODULES = {
    PageDashboard, PageInstall, PageAvailable, PageInstalled,
    PageBackup, PageSettings, PageDiagnostic, PageRepair, PageFixes, PageGameFixes, PageCredits,
    PageAchievements,
  };

  let _current   = null;
  let _initiated = new Set();

  async function init() {
    _setupNavigation();
    _setupSidebar();
    _setupGlobalSearch();
    _setupHashRouting();
    DownloadQueue.init();

    const hash = window.location.hash.replace('#', '') || 'dashboard';
    await navigateTo(PAGES[hash] ? hash : 'dashboard');

    State.addLog('SteamTools interface loaded', 'info');
  }

  function navigateTo(pageId) {
    if (!PAGES[pageId]) return;

    document.querySelectorAll('.page-section').forEach(s => s.classList.remove('active'));

    const section = document.getElementById(`page-${pageId}`);
    if (section) section.classList.add('active');

    document.querySelectorAll('.nav-item').forEach(item => {
      item.classList.toggle('active', item.dataset.page === pageId);
    });

    const titleEl = document.getElementById('header-page-name');
    if (titleEl) titleEl.textContent = PAGES[pageId].title;

    history.replaceState(null, '', `#${pageId}`);

    _current = pageId;

    const moduleName = PAGES[pageId].module;
    const module     = _MODULES[moduleName];

    let pending = null;

    if (module) {
      if (!_initiated.has(pageId)) {
        pending = module.init?.();
        _initiated.add(pageId);
      } else {
        if (['dashboard', 'installed', 'backup'].includes(pageId)) {
          module.refresh?.() || module.render?.() || null;
          if (pageId === 'backup') PageBackup.init?.();
          if (pageId === 'installed') PageInstalled.render?.();
        }

        if (pageId === 'settings') PageSettings.init();
        if (pageId === 'credits')  PageCredits.init();
        if (pageId === 'credits')  PageCredits.init();
      }
    }

    _closeSidebar();

    document.getElementById('content-area')?.scrollTo(0, 0);

    return pending;
  }

  function _setupNavigation() {
    document.querySelectorAll('.nav-item[data-page]').forEach(item => {
      item.addEventListener('click', e => {
        e.preventDefault();
        navigateTo(item.dataset.page);
      });
    });

    document.addEventListener('click', e => {
      const el = e.target.closest('[data-page]');
      if (el && el.dataset.page && !el.classList.contains('nav-item')) {
        e.preventDefault();
        navigateTo(el.dataset.page);
      }
    });
  }

  function _setupHashRouting() {
    window.addEventListener('hashchange', () => {
      const hash = window.location.hash.replace('#', '');
      if (PAGES[hash] && hash !== _current) navigateTo(hash);
    });
  }

  function _setupSidebar() {
    const toggle  = document.getElementById('sidebar-toggle');
    const sidebar = document.getElementById('sidebar');

    const overlay = document.createElement('div');
    overlay.className = 'sidebar-overlay';
    document.body.appendChild(overlay);

    toggle?.addEventListener('click', () => {
      sidebar?.classList.toggle('open');
      overlay.classList.toggle('active');
    });

    overlay.addEventListener('click', _closeSidebar);
  }

  function _closeSidebar() {
    document.getElementById('sidebar')?.classList.remove('open');
    document.querySelector('.sidebar-overlay')?.classList.remove('active');
  }

  function _setupGlobalSearch() {
    const input = document.getElementById('global-search');
    if (!input) return;

    input.addEventListener('keydown', e => {
      if (e.key === 'Enter') {
        const q = input.value.trim();
        if (!q) return;
        navigateTo('available');
        setTimeout(() => {
          const availSearch = document.getElementById('available-search');
          if (availSearch) {
            availSearch.value = q;
            availSearch.dispatchEvent(new Event('input'));
          }
        }, 50);
        input.value = '';
      }
    });

    document.addEventListener('keydown', e => {
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault();
        input.focus();
        input.select();
      }
    });
  }

  document.addEventListener('keydown', e => {
    if (e.key === 'Escape') Modal.close(false);
  });

  return { init, navigateTo };
})();

document.addEventListener('DOMContentLoaded', async () => {
  const loadingEl  = document.getElementById('loading-screen');
  const loadingMsg = document.getElementById('loading-step');

  document.documentElement.dataset.theme = State.getSetting('theme') || 'default';

  function _step(msg) {
    console.log('[ST] ' + msg);
    if (loadingMsg) loadingMsg.textContent = msg;
  }

  function _hideLoading() {
    if (!loadingEl) return;
    loadingEl.classList.add('loading-screen--out');
    setTimeout(() => { loadingEl.style.display = 'none'; }, 480);
  }

  const SERVER_CONNECTION_MESSAGE = 'Unable to connect to the servers. Please connect to the internet and try again.';
  const RETRY_DELAY_SECONDS = 15;
  const _sleep = ms => new Promise(r => setTimeout(r, ms));

  async function _waitForServerConnection() {
    while (true) {
      _step('Checking server connection...');

      let cfg = null;
      try {
        cfg = await API.loadConfig();
      } catch (e) {
        cfg = { ok: false, error: e?.message || String(e) };
      }

      if (cfg?.ok) return cfg;

      console.warn('[SteamTools] Remote server check failed:', cfg?.error || 'offline');
      for (let remaining = RETRY_DELAY_SECONDS; remaining > 0; remaining--) {
        _step(`${SERVER_CONNECTION_MESSAGE} Retrying in ${remaining}s...`);
        await _sleep(1000);
      }
    }
  }

  let startupConfig = null;

  if (window.location.protocol === 'http:' || window.location.protocol === 'https:') {
    _step('Connecting to server…');
    try {
      const ctrl = new AbortController();
      setTimeout(() => ctrl.abort(), 4000);
      await fetch('/api/settings', { signal: ctrl.signal });
    } catch (e) {
      console.error('[SteamTools] API fetch failed:', e);
      const b = document.getElementById('js-error-banner');
      if (b) {
        b.style.display = 'block';
        b.textContent = '⚠️ Cannot reach backend API: ' + e.message;
      }
    }

    startupConfig = await _waitForServerConnection();
    _step('Loading your account…');
    await Auth.init().catch(() => {});
    _step('Loading game catalog…');
    await API.getAvailableGames('').catch(() => {});
    if (typeof PageAvailable !== 'undefined' && PageAvailable.prerender) PageAvailable.prerender();
    _step('Checking for updates…');

    let localVer = null;
    try {
      const vd = await fetch('/api/version').then(r => r.json());
      localVer = vd?.local;
      if (localVer) {
        const sEl = document.getElementById('sidebar-version');
        const cEl = document.getElementById('credits-version');
        const dEl = document.getElementById('credits-version-detail');
        if (sEl) sEl.textContent = `v${localVer}`;
        if (cEl) cEl.textContent = `Version ${localVer}`;
        if (dEl) dEl.textContent = localVer;
      }
    } catch {}

    try {
      const cfg = startupConfig || await API.loadConfig();
      const remoteVer = cfg?.version;
      if (remoteVer && localVer) {
        const _parseVer = v => {
          const isBeta = v.endsWith('b');
          const clean = isBeta ? v.slice(0, -1) : v;
          const parts = clean.split('.').map(Number);
          return { major: parts[0] || 0, minor: parts[1] || 0, isBeta };
        };
        const _compareVer = (local, remote) => {
          const l = _parseVer(local);
          const r = _parseVer(remote);
          const lNum = l.major * 10000 + l.minor;
          const rNum = r.major * 10000 + r.minor;
          if (rNum > lNum) return 'update_required';
          if (rNum === lNum) {
            if (l.isBeta && !r.isBeta) return 'update_required';
            return 'up_to_date';
          }
          return l.isBeta ? 'test_version' : 'dev_version';
        };

        const result = _compareVer(localVer, remoteVer);

        if (result === 'update_required') {
          const banner = document.getElementById('update-banner');
          const textEl = document.getElementById('update-banner-text');
          const linkEl = document.getElementById('update-banner-link');
          if (textEl) textEl.textContent = `Version ${remoteVer} is available (you have ${localVer}).`;
          if (banner) banner.style.display = 'flex';

          document.body.classList.add('update-required');

          if (linkEl) {
            linkEl.removeAttribute('href');
            linkEl.addEventListener('click', async e => {
              e.preventDefault();
              linkEl.textContent = 'Updating…';
              linkEl.style.pointerEvents = 'none';
              try { await fetch('/api/auto-update', { method: 'POST' }); } catch {}
            }, { once: true });
          }
        } else if (result === 'dev_version') {
          const notice = document.getElementById('version-notice-banner');
          const text = document.getElementById('version-notice-text');
          if (text) text.textContent = `Development version (${localVer}) - you are running a build newer than the latest stable release.`;
          if (notice) notice.style.display = 'flex';
        } else if (result === 'test_version') {
          const notice = document.getElementById('version-notice-banner');
          const text = document.getElementById('version-notice-text');
          if (text) text.textContent = `Test version (${localVer}) - for beta testers and premium users.`;
          if (notice) notice.style.display = 'flex';
        }
      }
    } catch {}
  }

  _step('Applying your settings…');
  try {
    const _serverSettings = await API.getSettings();
    if (_serverSettings && _serverSettings.theme) {
      document.documentElement.dataset.theme = _serverSettings.theme;
    }
  } catch {}

  _step('Ready!');
  await new Promise(r => setTimeout(r, 320));

  try {
    await App.init();
  } catch (e) {
    console.error('[SteamTools] App.init() threw:', e);
    const b = document.getElementById('js-error-banner');
    if (b) {
      b.style.display = 'block';
      b.style.cursor = 'pointer';
      b.textContent = '⚠️ A loading error occurred — click here to try again.';
      b.addEventListener('click', () => window.location.reload(), { once: true });
    }
  }

  _hideLoading();
});
