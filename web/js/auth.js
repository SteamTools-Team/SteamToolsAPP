const Auth = (() => {
  let _state     = null;
  let _pollTimer = null;
  let _pairRequestId = null;
  let _pairTimer     = null;
  let _verifyFails   = 0;
  const VERIFY_MAX_FAILS = 3;

  async function init() {
    const state = await _check();
    if (state.blocked) {
      _showBlocked(state.block_reason);
      return state;
    }
    if (!state.authenticated) {
      _showOverlay();
    } else {
      _applyState(state);

      await _bgRefresh(0);
    }
    return _state;
  }

  async function logout() {
    try { await fetch('/api/auth/logout', { method: 'POST' }); } catch {}
    cancelCodeLogin();
    _verifyFails = 0;
    _state = { authenticated: false };
    _hideBlocked();
    _showOverlay();
  }

  function closeBlocked() {
    logout();
  }

  function openDiscordLogin() {
    const statusEl = document.getElementById('login-status');
    const fallbackEl = document.getElementById('login-link-fallback');
    const linkEl = document.getElementById('login-link-url');
    const copyBtn = document.getElementById('login-link-copy');
    if (statusEl) { statusEl.style.display = 'block'; statusEl.textContent = 'Fetching login link…'; }
    fetch('/api/auth/login-url')
      .then(r => r.json())
      .then(data => {
        if (!data.url) throw new Error();

        if (fallbackEl && linkEl) {
          linkEl.href = data.url;
          linkEl.textContent = data.url;
          fallbackEl.style.display = 'flex';
          if (copyBtn) {
            copyBtn.onclick = () => {
              navigator.clipboard.writeText(data.url).then(() => {
                copyBtn.textContent = 'Copied!';
                setTimeout(() => { copyBtn.textContent = 'Copy link'; }, 2000);
              }).catch(() => {});
            };
          }
        }
        if (statusEl) statusEl.textContent = 'Waiting for Discord login…';
        _startPoll();

        try {
          if (window.pywebview?.api?.open_browser) {
            window.pywebview.api.open_browser(data.url);
          } else {
            window.open(data.url, '_blank', 'noopener,noreferrer');
          }
        } catch(e) {}
      })
      .catch(() => { if (statusEl) statusEl.textContent = 'Error — please try again.'; });
  }

  function getState() { return _state; }

  function _pairEls() {
    return {
      panel: document.getElementById('login-code-panel'),
      code:  document.getElementById('login-code-value'),
      hint:  document.getElementById('login-code-hint'),
      status: document.getElementById('login-status'),
    };
  }

  function openCodeLogin() {
    const { panel, code, hint, status } = _pairEls();
    if (!panel) return;
    if (status) { status.style.display = 'block'; status.textContent = 'Generating code…'; }
    fetch('/api/auth/pair-begin', { method: 'POST', cache: 'no-store' })
      .then(async r => {
        const data = await r.json().catch(() => ({}));
        if (!data.code || !data.requestId) throw new Error(data.error || 'unavailable');
        return data;
      })
      .then(data => {
        _pairRequestId = data.requestId;
        code.textContent = data.code;
        hint.textContent = 'Valid for 5 minutes — waiting for /login on Discord…';
        hint.classList.remove('login-code-hint--error');
        panel.style.display = 'flex';
        const discordBtn = document.getElementById('login-discord-btn');
        const altBtn = document.getElementById('login-code-btn');
        if (discordBtn) discordBtn.style.display = 'none';
        if (altBtn) altBtn.style.display = 'none';
        if (status) { status.style.display = 'none'; status.textContent = ''; }
        _startPairPoll();
      })
      .catch(e => {
        const messages = {
          rate_limited: 'Too many attempts — wait a few minutes and try again.',
          hwid_banned:  'This machine is blocked. Please check the Discord server.',
          missing_hwid: 'Could not identify this machine — restart the app.',
        };
        const text = messages[e?.message] || 'Error — please try again.';
        if (status) { status.style.display = 'block'; status.textContent = text; }
      });
  }

  function cancelCodeLogin() {
    _stopPairPoll();
    _pairRequestId = null;
    const { panel, code, hint, status } = _pairEls();
    if (panel) panel.style.display = 'none';
    if (code) code.textContent = '----';
    if (hint) { hint.textContent = ''; hint.classList.remove('login-code-hint--error'); }
    if (status) { status.style.display = 'none'; status.textContent = ''; }
    const discordBtn = document.getElementById('login-discord-btn');
    const altBtn = document.getElementById('login-code-btn');
    if (discordBtn) discordBtn.style.display = '';
    if (altBtn) altBtn.style.display = '';
  }

  function _stopPairPoll() {
    if (_pairTimer) { clearInterval(_pairTimer); _pairTimer = null; }
  }

  function _startPairPoll() {
    _stopPairPoll();
    _pairTimer = setInterval(async () => {
      if (!_pairRequestId) { _stopPairPoll(); return; }
      try {
        const r = await fetch('/api/auth/pair-status?requestId=' + encodeURIComponent(_pairRequestId), { cache: 'no-store' });
        const data = await r.json();
        if (data.authenticated) {
          _stopPairPoll();
          cancelCodeLogin();
          _hideOverlay();
          _applyState(data);
          setTimeout(_bgRefresh, 1000);
          return;
        }
        if (data.status === 'expired') {
          _stopPairPoll();
          const { hint } = _pairEls();
          if (hint) {
            hint.textContent = 'Code expired — click "New code" to get another one.';
            hint.classList.add('login-code-hint--error');
          }
          return;
        }
        if (data.status === 'error' && data.error === 'not_found') {
          _stopPairPoll();
          const { hint } = _pairEls();
          if (hint) {
            hint.textContent = 'Code not found — click "New code" to retry.';
            hint.classList.add('login-code-hint--error');
          }
        }
      } catch {}
    }, 2000);
  }

  async function refreshUsage() {
    try {
      const r    = await fetch('/api/auth/refresh', { cache: 'no-store' });
      const data = await r.json();
      if (data.blocked) {
        _showBlocked(data.block_reason);
        return;
      }
      if (data.authenticated) {
        _state = data;
        _applyState(data);
      }
    } catch {}
  }

  async function _check() {
    try {
      const r = await fetch('/api/auth/status', { cache: 'no-store' });
      _state  = await r.json();
    } catch {
      _state = { authenticated: false };
    }
    return _state;
  }

  function _verifyHint(code) {
    if (code === 'clock')     return 'System clock is out of sync — set the correct date/time, then log in again.';
    if (code === 'signature') return 'Could not verify this machine — check your connection, then log in again.';
    if (code === 'token')     return 'Session expired — please log in again.';
    return '';
  }

  async function _bgRefresh(attempt) {
    attempt = attempt || 0;
    try {
      const r    = await fetch('/api/auth/refresh', { cache: 'no-store' });
      const data = await r.json();
      if (data.blocked) {
        _showBlocked(data.block_reason);
        return;
      }
      if (data.authenticated) {
        _verifyFails = 0;
        _state = data;
        _applyState(data);
        if (data.tier === 'standard' && attempt < 2) {
          setTimeout(() => _bgRefresh(attempt + 1), 5000);
        }
        return;
      }

      _verifyFails++;
      if (_verifyFails < VERIFY_MAX_FAILS) {
        setTimeout(() => _bgRefresh(attempt), 4000);
        return;
      }
      _verifyFails = 0;
      _state = { authenticated: false };
      _showOverlay(_verifyHint(data.verify_error));
    } catch {}
  }

  function _showOverlay(message) {
    const overlay = document.getElementById('login-overlay');
    if (!overlay) return;
    overlay.classList.remove('login-overlay--out');
    overlay.style.display = 'flex';
    const statusEl = document.getElementById('login-status');
    if (statusEl) {
      if (message) {
        statusEl.textContent = message;
        statusEl.style.display = 'block';
      } else {
        statusEl.style.display = 'none';
        statusEl.textContent = '';
      }
    }
  }

  function _showBlocked(reason) {
    const overlay = document.getElementById('login-overlay');
    if (!overlay) return;
    const card = overlay.querySelector('.login-card');
    if (card) card.style.opacity = '1';
    const statusEl = document.getElementById('login-status');
    if (statusEl) { statusEl.style.display = 'none'; statusEl.textContent = ''; }
    const blockedEl = document.getElementById('login-blocked');
    if (blockedEl) {
      blockedEl.textContent = reason || 'Your account has been restricted. Please verify the discord server.';
      blockedEl.style.display = 'block';
    }
    const btn = document.getElementById('login-discord-btn');
    if (btn) btn.style.display = 'none';
    const codeBtn = document.getElementById('login-code-btn');
    if (codeBtn) codeBtn.style.display = 'none';
    _stopPairPoll();
    const codePanel = document.getElementById('login-code-panel');
    if (codePanel) codePanel.style.display = 'none';
    const closeBtn = document.getElementById('login-close-blocked');
    if (closeBtn) closeBtn.style.display = '';
    const tiers = overlay.querySelector('.login-tiers');
    if (tiers) tiers.style.display = 'none';
    overlay.classList.remove('login-overlay--out');
    overlay.style.display = 'flex';
  }

  function _hideBlocked() {
    const blockedEl = document.getElementById('login-blocked');
    if (blockedEl) { blockedEl.style.display = 'none'; blockedEl.textContent = ''; }
    const btn = document.getElementById('login-discord-btn');
    if (btn) btn.style.display = '';
    const codeBtn = document.getElementById('login-code-btn');
    if (codeBtn) codeBtn.style.display = '';
    const closeBtn = document.getElementById('login-close-blocked');
    if (closeBtn) closeBtn.style.display = 'none';
    const overlay = document.getElementById('login-overlay');
    const tiers = overlay ? overlay.querySelector('.login-tiers') : null;
    if (tiers) tiers.style.display = '';
  }

  function _hideOverlay() {
    const overlay = document.getElementById('login-overlay');
    if (!overlay) return;
    overlay.classList.add('login-overlay--out');
    setTimeout(() => { overlay.style.display = 'none'; overlay.classList.remove('login-overlay--out'); }, 500);
  }

  function _avatarCandidates(user) {
    if (!user) return [];
    const out = [];
    const seen = new Set();
    const add = u => { if (u && typeof u === 'string' && !seen.has(u)) { seen.add(u); out.push(u); } };

    ['image', 'image_url', 'avatar_url', 'avatarURL', 'avatarUrl', 'pfp']
      .forEach(k => add(user[k]));

    const id = user.id || user.user_id || user.discord_id;
    const hash = user.avatar;
    if (id && hash) {
      const h = String(hash).trim();
      if (/^https?:\/\//i.test(h)) {
        add(h);
      } else if (/^[a-zA-Z0-9_]+$/.test(h)) {
        const base = `https://cdn.discordapp.com/avatars/${id}/${h}`;
        if (h.startsWith('a_')) {
          add(base + '.gif');
          add(base + '.png');
        } else {
          add(base + '.png');
          add(base + '.gif');
        }
      }
    }

    for (const u of out.slice()) {
      const m = u.match(/^(https?:\/\/cdn\.discord(?:app)?\.com\/avatars\/[^/]+\/[^/?]+)\.(png|jpe?g|webp|gif)(\?.*)?$/i);
      if (m && m[2] !== 'png') add(m[1] + '.png' + (m[3] || ''));
    }
    return out;
  }

  function _initials(user) {
    const name = user?.name || '';
    return name.trim().split(/\s+/).map(w => w[0] || '').join('').toUpperCase().slice(0, 2) || 'ST';
  }

  function _loadAvatar(el, user, candidates, idx) {
    const src = candidates && candidates[idx];
    if (!src) { el.textContent = _initials(user); return; }
    const img = document.createElement('img');
    img.src = src;
    img.alt = '';
    img.draggable = false;
    img.onerror = () => {
      if (!el.contains(img)) return;
      el.removeChild(img);
      _loadAvatar(el, user, candidates, idx + 1);
    };
    el.innerHTML = '';
    el.appendChild(img);
  }

  function _applyState(state) {
    window.dispatchEvent(new CustomEvent('authstatechange', { detail: state }));

    const nameEl = document.getElementById('sidebar-username');
    if (nameEl) nameEl.textContent = state.user?.name || 'SteamTools';

    const avatarEl = document.querySelector('.profile-avatar');
    if (avatarEl) {
      _loadAvatar(avatarEl, state.user, _avatarCandidates(state.user), 0);
    }

    const tierEl = document.getElementById('user-tier-label');
    if (tierEl) {
      const tier    = state.tier || 'standard';
      const classes = { standard: 'user-tier--standard', premium1: 'user-tier--premium-1', premium2: 'user-tier--premium-2' };
      const labels  = { standard: 'Standard', premium1: 'Premium T1', premium2: 'Premium T2' };
      tierEl.className   = `user-tier ${classes[tier] || classes.standard}`;
      tierEl.textContent = labels[tier] || 'Standard';
    }
  }

  function _startPoll() {
    if (_pollTimer) return;
    _pollTimer = setInterval(async () => {
      const state = await _check();
      if (state.blocked) {
        clearInterval(_pollTimer);
        _pollTimer = null;
        _showBlocked(state.block_reason);
        return;
      }
      if (state.authenticated) {
        clearInterval(_pollTimer);
        _pollTimer = null;
        _hideOverlay();
        _applyState(state);
        setTimeout(_bgRefresh, 3000);
      }
    }, 2000);
  }

  document.addEventListener('DOMContentLoaded', () => {
    document.getElementById('login-discord-btn')?.addEventListener('click', openDiscordLogin);
    document.getElementById('login-code-btn')?.addEventListener('click', openCodeLogin);
    document.getElementById('login-code-back')?.addEventListener('click', cancelCodeLogin);
    document.getElementById('login-code-refresh')?.addEventListener('click', openCodeLogin);
    document.getElementById('login-close-blocked')?.addEventListener('click', closeBlocked);
    document.getElementById('btn-signout')?.addEventListener('click', logout);
  });

  document.addEventListener('visibilitychange', () => {
    if (document.visibilityState === 'visible') {
      _check().then(state => {
        if (state.authenticated && !_state?.authenticated) {
          if (_pollTimer) { clearInterval(_pollTimer); _pollTimer = null; }
          _hideOverlay();
          _applyState(state);
          setTimeout(_bgRefresh, 1000);
        }
      }).catch(() => {});
    }
  });

  return { init, logout, openDiscordLogin, getState, refreshUsage };
})();
