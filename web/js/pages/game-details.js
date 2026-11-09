

const GameDetails = (() => {
  let _appid     = null;
  let _game      = null;
  let _installed = false;

  const overlay = () => document.getElementById('game-details-overlay');
  const scroll  = () => document.getElementById('gd-scroll');
  const footer  = () => document.getElementById('gd-footer');
  const title   = () => document.getElementById('gd-title');
  const subtitle= () => document.getElementById('gd-subtitle');
  const backdrop= () => document.getElementById('gd-backdrop');

  function open(game, forceInstalled) {
    _game      = game;
    _appid     = String(game.appid || '');
    if (!_appid) return;
    _installed = forceInstalled || State.isInstalled(_appid);

    _fillSkeleton();
    overlay().classList.add('open');
    document.body.style.overflow = 'hidden';

    API.getGameDetails(_appid).then(res => {
      if (res && res.ok) {
        _render(res);
      } else {
        _renderFallback(res || {});
      }
    });
  }

  function close() {
    overlay().classList.remove('open');
    document.body.style.overflow = '';
    _appid = null;
    _game  = null;
  }

  function _fillSkeleton() {
    backdrop().style.backgroundImage = `url(${getImageUrl(_appid)})`;
    title().textContent   = _game.name || `Game #${_appid}`;
    subtitle().textContent = `AppID: ${_appid}`;
    scroll().innerHTML = `
      <div style="display:flex;flex-direction:column;gap:14px;padding:18px">
        <div class="skel skel--lg"></div>
        <div class="skel"></div><div class="skel"></div><div class="skel skel--sm"></div>
        <div style="display:grid;grid-template-columns:repeat(3,1fr);gap:10px;margin-top:8px">
          <div class="skel" style="height:70px"></div><div class="skel" style="height:70px"></div><div class="skel" style="height:70px"></div>
        </div>
      </div>`;
    footer().innerHTML = '';
  }

  function _render(d) {
    title().textContent = d.name || _game.name || `Game #${_appid}`;
    const badges = [];
    if (d.drm)    badges.push('<span class="badge badge--yellow">DRM</span>');
    if (d.nsfw)   badges.push('<span class="badge badge--red">NSFW</span>');
    if (_installed) badges.push('<span class="badge badge--green">Installed</span>');
    subtitle().innerHTML = `AppID: ${d.appid} ${badges.join(' ')}`;
    backdrop().style.backgroundImage = `url(${d.capsule_image || d.header_image || getImageUrl(_appid)})`;

    let html = '<div class="gd-body">';

    const summary = stripTags(d.short_description || d.about || '');
    if (summary) {
      html += `<section class="gd-section gd-summary">${escapeHtml(summary)}</section>`;
    }

    html += '<section class="gd-section"><div class="gd-facts">';
    if (d.release_date && d.release_date.date) html += _fact('Release', escapeHtml(d.release_date.date));
    if (d.developers && d.developers.length)  html += _fact('Developer', escapeHtml(d.developers.slice(0, 2).join(', ')));
    if (d.publishers && d.publishers.length)  html += _fact('Publisher', escapeHtml(d.publishers.slice(0, 2).join(', ')));
    if (d.genres && d.genres.length)          html += _fact('Genres', escapeHtml(d.genres.slice(0, 3).join(', ')));
    const price = _priceHTML(d);
    if (price) html += _factHTML('Price', price);
    if (d.metacritic && d.metacritic.score) {
      const score = d.metacritic.score;
      const tone  = score >= 75 ? 'green' : score >= 50 ? 'yellow' : 'red';
      html += _factHTML('Metacritic', `<span class="badge badge--${tone}" style="font-size:0.72rem">${score}</span>`);
    }
    if (d.recommendations && d.recommendations > 0) html += _fact('Reviews', formatNumber(d.recommendations));
    const plat = _platformsHTML(d);
    if (plat) html += _factHTML('Platforms', plat);
    html += '</div></section>';

    if (d.screenshots && d.screenshots.length) {
      html += '<section class="gd-section"><h4 class="gd-sec-title">Screenshots</h4>';
      html += `<div class="gd-shots">${d.screenshots.map(s => `
        <div class="gd-shot" onclick="GameDetails.preview('${s.full}')">
          <img src="${s.thumb}" loading="lazy" alt="Screenshot">
        </div>`).join('')}</div>`;
      html += '</section>';
    }

    const trailers = (d.movies || []).filter(m => m.mp4);
    if (trailers.length) {
      html += '<section class="gd-section"><h4 class="gd-sec-title">Trailers</h4><div class="gd-movies">';
      trailers.slice(0, 3).forEach(m => {
        html += `<div class="gd-movie"><video controls preload="none" poster="${m.thumb}"><source src="${m.mp4}" type="video/mp4"></video></div>`;
      });
      html += '</div></section>';
    }

    html += '</div>';
    const sc = scroll();
    sc.innerHTML = html;
    sc.scrollTop = 0;

    _renderFooter(d);
  }

  function _renderFallback(d) {
    title().textContent = _game.name || `Game #${_appid}`;
    subtitle().textContent = `AppID: ${_appid}`;
    scroll().innerHTML = `
      <div class="gd-body">
        <section class="gd-section">
          <h4 class="gd-sec-title">Game details</h4>
          <div class="gd-desc" style="color:var(--text-2)">${escapeHtml(d.error || 'Could not load details from the Steam Store API.')}</div>
        </section>
      </div>`;
    _renderFooter(d);
  }

  function _renderFooter(d) {
    const queueActive = DownloadQueue.isEnabled();
    const isPremium = ['premium1', 'premium2'].includes(Auth.getState().tier || 'standard');
    const provider = State.getSetting('provider');
    const isUpdatable = provider !== 'hubcap' && provider !== 'depotbox' && provider !== 'steamtools';

    let actions = '';
    if (_installed) {
      actions = `<button class="btn btn-ghost-danger btn-sm" id="gd-remove">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
          <polyline points="3 6 5 6 21 6"/>
          <path d="M19 6l-1 14a2 2 0 0 1-2 2H8a2 2 0 0 1-2-2L5 6"/>
        </svg>
        Remove
      </button>`;
    } else {
      actions = `<button class="btn btn-primary btn-sm" id="gd-install">
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
        ${queueActive ? 'Add to Queue' : 'Install'}
      </button>`;
    }
    const storeUrl = d.store_url || `https://store.steampowered.com/app/${_appid}/`;
    actions += `<button class="btn btn-ghost btn-sm" id="gd-store">View on Steam</button>`;

    const updateBtn = isUpdatable
      ? `<button class="btn btn-ghost btn-sm" id="gd-update" title="${isPremium ? 'Update manifest' : 'Premium required'}"><svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M23 4v6h-6"/><path d="M1 20v-6h6"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg></button>` : '';

    footer().innerHTML = `
      <div class="gd-footer-left"></div>
      <div class="gd-footer-right">${updateBtn}${actions}</div>`;

    const storeBtn = footer().querySelector('#gd-store');
    if (storeBtn) storeBtn.addEventListener('click', () => {
      if (window.pywebview?.api?.open_browser) window.pywebview.api.open_browser(storeUrl);
      else window.open(storeUrl, '_blank', 'noopener,noreferrer');
    });

    const installBtn = footer().querySelector('#gd-install');
    if (installBtn) installBtn.addEventListener('click', () => {
      close();
      PageInstall.installFromCatalog(_appid, (d && d.name) || _game.name);
    });

    const removeBtn = footer().querySelector('#gd-remove');
    if (removeBtn) removeBtn.addEventListener('click', async () => {
      const gameName = (d && d.name) || _game.name || `Game #${_appid}`;
      const confirmed = await Modal.open({
        titleText: 'Remove Game',
        bodyHtml: `<p>Remove <strong>${escapeHtml(gameName)}</strong> (AppID: ${_appid}) from your Steam library?</p><p style="margin-top:8px;font-size:0.78rem;color:var(--text-3)">This will delete the manifest and game files.</p>`,
        confirmText: 'Remove',
        cancelText: 'Cancel',
        danger: true,
      });
      if (!confirmed) return;
      const result = await API.removeGame(_appid);
      if (result.ok) {
        Toast.success('Game Removed', `${gameName} has been removed`);
        close();
        State.reloadArea('installed');
      } else {
        Toast.error('Removal Failed', result.error || 'An error occurred');
      }
    });

    const updateBtnEl = footer().querySelector('#gd-update');
    if (updateBtnEl) updateBtnEl.addEventListener('click', async () => {
      const tier = Auth.getState().tier || 'standard';
      if (!['premium1', 'premium2'].includes(tier)) { Toast.error('Premium Required', 'Game updates are a Premium feature.'); return; }
      updateBtnEl.disabled = true;
      updateBtnEl.innerHTML = `<div class="spinner spinner--sm"></div>`;
      const result = await API.updateManifestOnly(_appid);
      updateBtnEl.disabled = false;
      updateBtnEl.innerHTML = `<svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><path d="M23 4v6h-6"/><path d="M1 20v-6h6"/><path d="M3.51 9a9 9 0 0 1 14.85-3.36L23 10M1 14l4.64 4.36A9 9 0 0 0 20.49 15"/></svg>`;
      if (result.ok && result.alreadyLatest) Toast.info('Already up to date', `${d.name} is already on the latest manifest`);
      else if (result.ok) Toast.success('Manifest Updated', `${d.name} manifest has been updated`);
      else Toast.error('Update Failed', result.error || 'An error occurred');
    });
  }

  function _fact(label, value) {
    return `<div class="gd-fact"><span class="gd-fact-label">${label}</span><span class="gd-fact-value">${value}</span></div>`;
  }

  function _factHTML(label, inner) {
    return `<div class="gd-fact"><span class="gd-fact-label">${label}</span><span class="gd-fact-value">${inner}</span></div>`;
  }

  function _platformsHTML(d) {
    if (!d.platforms || !d.platforms.length) return '';
    const platIcons = {
      windows: '<svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"><path d="M3 5.5L11 4.2v6.8H3zM3 18.5l8 1.3v-6.8H3zM13 4l8-1.3v8.5h-8zM13 20.8l8 1.3v-8.5h-8z"/></svg>',
      mac:     '<svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"><path d="M17.05 12.54c-.03-2.76 2.25-4.08 2.35-4.15-1.28-1.87-3.27-2.13-3.98-2.16-1.69-.17-3.3 1-4.16 1-.85 0-2.17-.97-3.57-.94-1.83.03-3.53 1.07-4.47 2.71-1.9 3.3-.49 8.2 1.37 10.88.91 1.31 1.99 2.79 3.41 2.73 1.37-.05 1.89-.88 3.54-.88 1.65 0 2.12.88 3.57.85 1.47-.02 2.41-1.33 3.31-2.65 1.04-1.53 1.47-3 1.5-3.07-.03-.02-2.87-1.1-2.9-4.37zM14.2 3.85c.76-.92 1.27-2.2 1.13-3.47-.09-.13-.44-.02-.89.19-.52.25-3.23 1.98-3.23 4.07-2.28 1.23-2.93 4.43-2.93 4.43.68.06 2.11-.50 2.98-2.2 1.37.55 2.63.87 3.32.82.47-1.39.43-2.78.05-3.84-.03-.08.26-.15.05-.15h.02z"/></svg>',
      linux:   '<svg width="13" height="13" viewBox="0 0 24 24" fill="currentColor"><path d="M10.5 2a1.5 1.5 0 1 1 3 0 1.5 1.5 0 0 1-3 0zm6.1 5.9c-.4-.2-.7-.7-.7-1.2 0-.3.1-.6.3-.8l.8-1.6c.2-.4-.1-.9-.6-.9h-8.8c-.5 0-.8.5-.6.9l.8 1.6c.2.2.3.5.3.8 0 .5-.3 1-.7 1.2L4.4 9.4c-.5.3-.5 1 0 1.3l1.9 1c.1.1.2.2.2.3v6.4c0 .9.7 1.6 1.6 1.6h8.2c.9 0 1.6-.7 1.6-1.6v-6.4c0-.1.1-.3.2-.4l1.9-1c.5-.3.5-1 0-1.3l-2.9-1.4zM12 19c-2.8 0-5-2-5-4.5S9.2 10 12 10s5 2 5 4.5S14.8 19 12 19z"/></svg>',
    };
    return d.platforms.map(p => `<span title="${p}" style="display:inline-flex;margin-right:4px">${platIcons[p] || p}</span>`).join('');
  }

  function formatNumber(n) {
    n = Number(n) || 0;
    if (n >= 1000000) return (n / 1000000).toFixed(1) + 'M';
    if (n >= 1000)    return (Math.round(n / 100) / 10).toFixed(1) + 'K';
    return String(n);
  }

  function _priceHTML(d) {
    if (d.is_free) return '<span class="gd-price gd-price--free">Free to Play</span>';
    const p = d.price || {};
    const final = p.final_formatted || (p.final ? '$' + (p.final / 100).toFixed(2) : '');
    if (!final) return '';
    let s = `<span class="gd-price">${escapeHtml(final)}</span>`;
    if (p.discount_percent > 0) {
      const init = p.initial_formatted || (p.initial ? '$' + (p.initial / 100).toFixed(2) : '');
      s = `<span class="gd-price-row"><span class="gd-price gd-price--old">${escapeHtml(init)}</span><span class="badge badge--green" style="font-size:0.7rem">-${p.discount_percent}%</span><span class="gd-price">${escapeHtml(final)}</span></span>`;
    }
    return s;
  }

  function stripTags(html) {
    const div = document.createElement('div');
    div.innerHTML = html || '';
    return div.textContent || '';
  }

  let _preloading = false;
  function preview(src) {
    if (!src || _preloading) return;
    let lb = document.getElementById('gd-lightbox');
    if (!lb) {
      lb = document.createElement('div');
      lb.id = 'gd-lightbox';
      lb.className = 'gd-lightbox';
      lb.innerHTML = `<button class="modal-close gd-lb-close" aria-label="Close"><svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg></button><img alt="Preview">`;
      lb.addEventListener('click', () => lb.classList.remove('open'));
      document.body.appendChild(lb);
      lb.querySelector('.gd-lb-close').addEventListener('click', () => lb.classList.remove('open'));
    }
    const img = lb.querySelector('img');
    const pre = new Image();
    _preloading = true;
    pre.onload = () => {
      img.src = src;
      img.onload = null;
      _preloading = false;
      lb.classList.add('open');
    };
    pre.onerror = () => {
      img.src = src;
      img.onload = null;
      _preloading = false;
      lb.classList.add('open');
    };
    pre.src = src;
  }

  document.addEventListener('DOMContentLoaded', () => {
    const closeBtn = document.getElementById('gd-close');
    const ov       = overlay();
    if (closeBtn) closeBtn.addEventListener('click', close);
    document.addEventListener('keydown', e => {
      if (e.key === 'Escape') { close(); const lb = document.getElementById('gd-lightbox'); if (lb) lb.classList.remove('open'); }
    });
    if (ov) ov.addEventListener('click', e => { if (e.target === ov) close(); });
  });

  return { open, close, preview };
})();