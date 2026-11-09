

const PageDashboard = (() => {

  async function init() {
    console.log('[Dashboard] init start');
    _bindActions();
    await _refresh().catch(() => {});
    console.log('[Dashboard] _refresh done');
    await _loadRecentInstalls().catch(() => {});
    _maybeShowPremiumUpsell();
    console.log('[Dashboard] init complete');
  }

  let _premiumUpsellShown = false;

  function _maybeShowPremiumUpsell() {
    if (_premiumUpsellShown) return;
    const state = Auth.getState();
    if (!state?.authenticated) return;
    if (state.tier && state.tier !== 'standard') return;
    _premiumUpsellShown = true;
    _showPremiumModal();
  }

  function _showPremiumModal() {
    const overlay = document.getElementById('prem-overlay');
    const skip = document.getElementById('prem-skip');
    const cd = document.getElementById('prem-cd');
    const cta = document.getElementById('prem-cta');
    if (!overlay) return;

    overlay.classList.add('active');

    let cooldown = 6;
    skip.disabled = true;
    cd.textContent = '6';
    const timer = setInterval(() => {
      cooldown--;
      cd.textContent = cooldown;
      if (cooldown <= 0) {
        clearInterval(timer);
        skip.disabled = false;
        skip.textContent = 'Dismiss';
      }
    }, 1000);

    const close = () => {
      clearInterval(timer);
      overlay.classList.remove('active');
      skip.disabled = true;
      skip.innerHTML = 'Dismiss <span id="prem-cd">6</span>s';
    };

    cta.addEventListener('click', () => {
      window.open('https://discord.com/channels/1363222357975502878/1436611572687437904', '_blank');
      close();
    }, { once: true });
    skip.addEventListener('click', close, { once: true });
  }

  async function _refresh() {
    console.log('[Dashboard] _refresh start');
    await Promise.allSettled([
      _updateInstalledCount().then(() => console.log('[Dashboard] installed ✓')).catch(e => console.error('[Dashboard] installed ✗', e)),
      _updateAvailableCount().then(() => console.log('[Dashboard] available ✓')).catch(e => console.error('[Dashboard] available ✗', e)),
      _updateSteamStatus().then(()  => console.log('[Dashboard] steam ✓')).catch(e => console.error('[Dashboard] steam ✗', e)),
    ]);
    console.log('[Dashboard] allSettled resolved');
    _updateSystemStatus();
    _updateDownloadsLeft();
    document.getElementById('sli-last-refresh').textContent = 'Just now';
  }

  function _updateDownloadsLeft() {
    const state = Auth.getState();
    const el    = document.getElementById('stat-downloads-left');
    const trend = document.getElementById('downloads-stat-trend');
    if (!el) return;

    if (!state?.authenticated) {
      el.textContent = '—';
      if (trend) trend.textContent = 'Today';
      return;
    }

    const tier  = state.tier  || 'standard';
    const usage = state.usage || {};

    if (tier === 'premium2' || usage.unlimited) {
      el.textContent = '∞';
    } else if (usage.downloadsRemaining !== undefined) {
      el.textContent = usage.downloadsRemaining;
    } else {
      el.textContent = '—';
    }

    const tierLabels = { standard: 'Standard', premium1: 'Premium T1', premium2: 'Premium T2' };
    if (trend) trend.textContent = tierLabels[tier] || 'Standard';
  }

  async function _updateInstalledCount() {
    const records = await API.getInstalledGames();
    const count   = records && typeof records === 'object' ? Object.keys(records).length : 0;
    const el      = document.getElementById('stat-installed-count');
    if (el) el.textContent = count;

    return records;
  }

  async function _updateAvailableCount() {
    const el = document.getElementById('stat-available-count');
    if (el) el.innerHTML = '<div class="spinner spinner--sm"></div>';
    try {
      const total = await API.getGamesTotal();
      if (el) el.textContent = total || '—';
      const sub = document.getElementById('available-subtitle');
      if (sub && total) sub.textContent = `${total} games available in catalog`;
    } catch {
      if (el) el.textContent = 'N/A';
    }
  }

  async function _updateSteamStatus() {
    const steam    = await API.refreshSteamStatus();
    const statEl   = document.getElementById('stat-steam-status');
    const iconEl   = document.getElementById('steam-stat-icon');
    const trendEl  = document.getElementById('steam-stat-trend');
    const dotEl    = document.getElementById('steam-dot');
    const lblEl    = document.getElementById('steam-status-label');

    if (steam.detected) {
      if (statEl) statEl.textContent = steam.running ? 'Running' : 'Detected';
      if (iconEl) { iconEl.className = 'stat-icon stat-icon--green'; }
      if (trendEl) { trendEl.className = 'stat-trend stat-trend--up'; trendEl.textContent = steam.running ? 'Active' : 'Ready'; }
      if (dotEl)  dotEl.className = `status-dot status-dot--${steam.running ? 'green' : 'yellow'}`;
      if (lblEl)  lblEl.textContent = steam.running ? 'Running' : 'Detected';
    } else {
      if (statEl) statEl.textContent = 'Not Found';
      if (iconEl) { iconEl.className = 'stat-icon stat-icon--red'; }
      if (trendEl) { trendEl.className = 'stat-trend stat-trend--down'; trendEl.textContent = 'Error'; }
      if (dotEl)  dotEl.className = 'status-dot';
      if (lblEl)  lblEl.textContent = 'Not found';
    }
  }

  function _updateSystemStatus() {
    const steam = API.getSteamStatus();
    const pathEl = document.getElementById('sli-steam-path');
    const runEl  = document.getElementById('sli-steam-running');

    if (pathEl) pathEl.textContent = steam.path || 'Not detected';
    if (runEl) {
      runEl.innerHTML = steam.running
        ? '<span class="badge badge--green">Running</span>'
        : steam.detected
        ? '<span class="badge badge--yellow">Stopped</span>'
        : '<span class="badge badge--red">Not found</span>';
    }

    const apiDot = document.getElementById('api-dot');
    const apiLbl = document.getElementById('api-status-label');
    if (apiDot) apiDot.className = 'status-dot status-dot--green';
    if (apiLbl) apiLbl.textContent = 'Online';
  }

  async function _loadRecentInstalls() {
    const container = document.getElementById('recent-installs');
    if (!container) return;
    let records = {};
    try { records = await API.getInstalledGames() || {}; } catch { records = {}; }
    const entries = Object.entries(records);

    if (entries.length === 0) {
      container.innerHTML = `
        <div class="empty-state">
          <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="1.5">
            <path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/>
          </svg>
          <p>No games installed yet</p>
        </div>`;
      return;
    }

    entries.sort((a, b) => (b[1].timestamp || 0) - (a[1].timestamp || 0));
    const recent = entries.slice(0, 5);

    const steamImgUrl = appid => getImageUrl(appid);

    container.innerHTML = recent.map(([appid, entry]) => `
      <div class="recent-install-item">
        <div class="recent-install-icon">
          <img src="${steamImgUrl(appid)}" alt="" data-appid="${appid}" data-name="${escapeHtml(entry.name)}" style="width:100%;height:100%;object-fit:cover;border-radius:inherit" loading="lazy" onerror="steamIconError(this)">
        </div>
        <div class="recent-install-info">
          <div class="recent-install-name">${escapeHtml(entry.name)}</div>
          <div class="recent-install-time">AppID: ${appid} · ${timeAgo(entry.timestamp)}</div>
        </div>
        ${badgeHtml('Installed', 'green')}
      </div>
    `).join('');
  }

  function _bindActions() {
    document.getElementById('dashboard-refresh')?.addEventListener('click', async () => {
      API.invalidateSteamCache();
      API.invalidateGamesCache();
      await Promise.allSettled([_refresh(), Auth.refreshUsage()]);
      Toast.success('Refreshed', 'Dashboard data updated');
    });

    document.getElementById('header-refresh')?.addEventListener('click', async () => {
      API.invalidateSteamCache();
      API.invalidateGamesCache();
      await Promise.allSettled([_refresh(), Auth.refreshUsage()]);
    });

    document.getElementById('run-quick-diagnostic')?.addEventListener('click', () => {
      App.navigateTo('diagnostic');
    });

    document.querySelectorAll('.quick-action-btn[data-page]').forEach(btn => {
      btn.addEventListener('click', () => App.navigateTo(btn.dataset.page));
    });
  }

  window.addEventListener('statechange', e => {
    if (!document.getElementById('page-dashboard').classList.contains('active')) return;
    if (e.detail.area === 'installed') {
      _updateInstalledCount();
      _loadRecentInstalls();
    }
  });

  window.addEventListener('authstatechange', () => {
    if (!document.getElementById('page-dashboard').classList.contains('active')) return;
    _updateDownloadsLeft();
  });

  return { init, refresh: _refresh };
})();
