window.addEventListener('unhandledrejection', e => {
  if (e.reason?.name === 'AbortError') e.preventDefault();
});

const _http = () =>
  window.location.protocol === 'http:' ||
  window.location.protocol === 'https:';

function _makeSignal(ms, externalSignal) {
  const ctrl = new AbortController();
  const timeout = setTimeout(() => ctrl.abort(), ms);
  if (externalSignal) {
    externalSignal.addEventListener('abort', () => { clearTimeout(timeout); ctrl.abort(); }, { once: true });
  }
  return ctrl.signal;
}

async function _get(path, timeoutMs = 12000, signal) {
  const r = await fetch(path, { signal: _makeSignal(timeoutMs, signal) });
  if (!r.ok) throw new Error(`HTTP ${r.status}: ${path}`);
  return r.json();
}

async function _post(path, body = {}, timeoutMs = 12000, signal) {
  const r = await fetch(path, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
    signal: _makeSignal(timeoutMs, signal),
  });
  if (!r.ok) throw new Error(`HTTP ${r.status}: ${path}`);
  return r.json();
}

function _stream(taskId, { onStep, onLog, onProgress, onEvent } = {}) {
  return new Promise((resolve, reject) => {
    const es = new EventSource(`/api/events/${taskId}`);
    es.onmessage = (e) => {
      if (!e.data || e.data === '{}') return;
      let data;
      try { data = JSON.parse(e.data); } catch { return; }
      if (data.keep)             return;
      if (data.type === 'step')     onStep?.(data.stepId,  data.status,   data.detail  || '');
      if (data.type === 'log')      onLog?.(data.msg,      data.logType   || 'info');
      if (data.type === 'progress') onProgress?.(data.pct, data.label     || '');
      if (data.type === 'game_progress') onEvent?.(data);
      if (data.type === 'done') {
        es.close();
        resolve({
          ok:      data.ok,
          game:    data.game,
          appid:   data.appid,
          total:   data.total,
          success: data.success,
          failed:  data.failed,
          results: data.results,
          repairs: data.repairs,
          error:   data.error,
        });
      }
      if (data.type === 'error') {
        es.close();
        reject(new Error(data.error || 'Stream error'));
      }
    };
    es.onerror = () => { es.close(); reject(new Error('SSE connection lost')); };
  });
}

const _MOCK_GAMES = [];

const INSTALL_STEPS = [
  { id:'detect',   label:'Detect Steam installation' },
  { id:'dlls',     label:'Check & install BetterSteamTools (BST)' },
  { id:'stop',     label:'Stop Steam' },
  { id:'download', label:'Download from database' },
  { id:'extract',  label:'Extract archive' },
  { id:'copy',     label:'Copy .lua & .manifest files' },
  { id:'register', label:'Register in library' },
  { id:'restart',  label:'Restart Steam' },
];

const REPAIR_STEPS = [
  { id:'kill',    label:'Close Steam' },
  { id:'cache',   label:'Clear Steam caches' },
  { id:'temp',    label:'Remove temporary files' },
  { id:'package', label:'Reset package folder' },
  { id:'logs',    label:'Clean Steam logs' },
  { id:'dns',     label:'Flush DNS cache' },
  { id:'cloudredirect',   label:'CloudRedirect', optional: true },
  { id:'bettersteamtools', label:'BetterSteamTools (BST Fix)', optional: true },
  { id:'restart', label:'Restart Steam' },
];

const _delay = ms => new Promise(r => setTimeout(r, ms));

function _friendlyError(e) {
  const netErr = e instanceof TypeError || /failed to fetch|networkerror|network error/i.test(String(e));
  return netErr
    ? 'Please verify yourself on the Discord server to use SteamTools.'
    : String(e);
}

const API = (() => {
  async function loadConfig() {
    if (_http()) {
      try { return await _get('/api/config'); }
      catch(e) { return { ok: false, error: String(e) }; }
    }
    await _delay(400);
    return { ok: true, version: '10854', local_version: '10854', update_available: false, site: '', discord: '', github: '', pseudo: 'SteamTools Team' };
  }

  let _steamCache = null;
  let _cacheTs    = 0;

  function getSteamStatus() {
    if (_steamCache && (Date.now() - _cacheTs) < 10000) return _steamCache;
    return _steamCache || { detected: false, path: '', running: false };
  }

  async function refreshSteamStatus() {
    if (_http()) {
      try {
        const s  = await _get('/api/steam/status');
        _steamCache = s;
        _cacheTs    = Date.now();
        return s;
      } catch {
        return { detected: false, path: '', running: false };
      }
    }
    const mock = { detected: true, path: 'C:\\Program Files (x86)\\Steam', running: false };
    _steamCache = mock; _cacheTs = Date.now();
    return mock;
  }

  function invalidateSteamCache() {
    _steamCache = null; _cacheTs = 0;
    if (_http()) _post('/api/steam/invalidate').catch(() => {});
  }

  function invalidateGamesCache() {
    _gamesCache = null;
  }

  function gamesCacheExists() {
    return _gamesCache !== null;
  }

  function getGamesCache() {
    return _gamesCache;
  }

  async function launchSteam() {
    if (_http()) return _post('/api/steam/launch');
    await _delay(1500);
    return { ok: true };
  }

  let _gamesCache = null;

  async function getAvailableGames(query = '', signal) {
    if (_http()) {
      if (!query && _gamesCache) return _gamesCache;
      const qs  = query ? `?q=${encodeURIComponent(query)}&limit=0` : '?limit=0';
      const r   = await _get(`/api/games/available${qs}`, 120000, signal);
      if (r.ok) {
        const result = { games: r.games, total: r.total, found: r.found };
        if (!query) _gamesCache = result;
        return result;
      }
      throw new Error(r.error || 'Failed to load games');
    }
    const q = query.toLowerCase();
    const games = q
      ? _MOCK_GAMES.filter(g => g.name.toLowerCase().includes(q) || g.appid.includes(q))
      : [..._MOCK_GAMES];
    return { games, total: _MOCK_GAMES.length, found: games.length };
  }

  async function getGamesTotal() {
    if (_http()) {
      try {
        const r = await _get('/api/games/total', 5000);
        return r.total || 0;
      } catch { return 0; }
    }
    return _MOCK_GAMES.length;
  }

  async function getGameDetails(appid) {
    if (_http()) {
      try {
        return await _get(`/api/games/details?appid=${encodeURIComponent(appid)}`, 20000);
      } catch(e) {
        return { ok: false, error: String(e) };
      }
    }
    return { ok: false, error: 'Details only available in HTTP mode' };
  }

  function getGameByAppId(appid) {
    return _MOCK_GAMES.find(g => g.appid === String(appid)) || null;
  }

  function searchGames(query) {
    const q = query.toLowerCase();
    return _MOCK_GAMES.filter(g => g.name.toLowerCase().includes(q) || g.appid.includes(q));
  }

  async function installGame(appid, { onStep, onLog, onProgress, skipRestart } = {}) {
    if (_http()) {
      try {
        const resp = await fetch('/api/install/start', {
          method:  'POST',
          headers: { 'Content-Type': 'application/json' },
          body:    JSON.stringify({ appid, skip_restart: !!skipRestart }),
        });
        const data = await resp.json();
        if (!resp.ok) {
          if (data.auth_required) Auth?.logout?.();
          return {
            ok:            false,
            error:         data.error || `HTTP ${resp.status}`,
            quota_exceeded: !!data.quota_exceeded,
            rate_limited:  resp.status === 429,
          };
        }
        return await _stream(data.task_id, { onStep, onLog, onProgress });
      } catch(e) {
        return { ok: false, error: _friendlyError(e) };
      }
    }

    const steps  = ['detect','stop','download','extract','copy','register','restart'];
    const delays = [400, 300, 2000, 600, 500, 300, 800];
    for (let i = 0; i < steps.length; i++) {
      onStep?.(steps[i], 'active');
      if (steps[i] === 'download') {
        for (let p = 0; p <= 100; p += 10) {
          onProgress?.(p, `${p}%`);
          await _delay(80);
        }
      } else {
        await _delay(delays[i]);
      }
      onStep?.(steps[i], 'done');
      onLog?.(`[demo] ${steps[i]} OK`, 'success');
    }
    State.addInstalled(appid, `Game_${appid}`, `Game_${appid}`);
    return { ok: true, game: `Game_${appid}`, appid: String(appid) };
  }

  async function installBatch(appids, { onLog, onProgress, onGameProgress } = {}) {
    if (_http()) {
      try {
        const resp = await fetch('/api/install/batch', {
          method:  'POST',
          headers: { 'Content-Type': 'application/json' },
          body:    JSON.stringify({ appids }),
        });
        const data = await resp.json();
        if (!resp.ok) {
          if (data.auth_required) Auth?.logout?.();
          return { ok: false, error: data.error || `HTTP ${resp.status}` };
        }
        return await _stream(data.task_id, {
          onLog,
          onProgress,
          onEvent: (ev) => {
            if (ev.type === 'game_progress') {
              onGameProgress?.(ev.appid, ev.pct, ev.label);
            }
          },
        });
      } catch(e) {
        return { ok: false, error: _friendlyError(e) };
      }
    }
    return { ok: false, error: 'No server' };
  }

  async function runDiagnostic({ onResult } = {}) {
    if (_http()) {
      try {
        const r   = await _post('/api/diagnostic');
        const res = r.results || {};
        if (res.detect)    onResult?.('detect',    res.detect);
        if (res.files)     onResult?.('files',     res.files);
        if (res.dirs)      onResult?.('dirs',      res.dirs);
        if (res.users)     onResult?.('users',     res.users);
        if (res.processes) onResult?.('processes', res.processes);
        if (res.disk)      onResult?.('disk',      res.disk);
        return r;
      } catch(e) {
        return { ok: false, issues: [String(e)], results: {} };
      }
    }

    await _delay(2000);
    onResult?.('detect',    { ok: true,  path: 'C:\\Program Files (x86)\\Steam', running: false });
    onResult?.('files',     { files: [{name:'steam.exe',ok:true},{name:'steamapps/libraryfolders.vdf',ok:true},{name:'config/loginusers.vdf',ok:true}] });
    onResult?.('dirs',      { dirs: [{name:'steamapps',ok:true},{name:'steamapps/common',ok:true},{name:'config',ok:true},{name:'userdata',ok:true},{name:'config/stplug-in',ok:true},{name:'config/depotcache',ok:true},{name:'logs',ok:true},{name:'appcache',ok:true}] });
    onResult?.('users',     { count: 1, users: [{id:'12345678', configOk: true}] });
    onResult?.('processes', { processes: [] });
    onResult?.('disk',      { totalGB: 500, usedGB: 180, freeGB: 320 });
    return { ok: true, issues: [] };
  }

  async function updateManifestOnly(appid) {
    if (_http()) {
      const ctrl = new AbortController();
      const tid  = setTimeout(() => ctrl.abort(), 35000);
      try {
        const resp = await fetch('/api/update-manifest/start', {
          method:  'POST',
          headers: { 'Content-Type': 'application/json' },
          body:    JSON.stringify({ appid }),
          signal:  ctrl.signal,
        });
        const data = await resp.json().catch(() => ({}));
        if (!resp.ok) {
          if (data.premium_required) return { ok: false, error: data.error, premium_required: true };
          return { ok: false, error: data.error || `Error ${resp.status}` };
        }
        return { ok: true, appid, alreadyLatest: data.alreadyLatest, message: data.ryuuMessage };
      } catch(e) {
        if (e.name === 'AbortError') return { ok: false, error: 'Request timed out. Please try again.' };
        return { ok: false, error: _friendlyError(e) };
      } finally {
        clearTimeout(tid);
      }
    }
    await _delay(800);
    return { ok: true, appid };
  }

  async function updateGame(appid, { onStep, onLog, onProgress } = {}) {
    if (_http()) {
      try {
        const r = await _post('/api/update/start', { appid });
        if (r.premium_required) return { ok: false, error: r.error, premium_required: true };
        const { task_id } = r;
        return await _stream(task_id, { onStep, onLog, onProgress });
      } catch(e) {
        return { ok: false, error: _friendlyError(e) };
      }
    }

    const steps = ['detect','download','extract','copy','register'];
    for (const sid of steps) {
      onStep?.(sid, 'active');
      await _delay(500);
      if (sid === 'download') {
        for (let p = 0; p <= 100; p += 20) { onProgress?.(p, `${p}%`); await _delay(80); }
      }
      onStep?.(sid, 'done');
      onLog?.(`[demo] ${sid} done`, 'success');
    }
    return { ok: true, game: 'Demo Game', appid };
  }

  async function checkMissingDlls() {
    if (_http()) {
      try { return await _post('/api/repair/check-dlls'); }
      catch { return { ok: false, missing: [], all_present: false }; }
    }
    return { ok: true, missing: [], all_present: true };
  }

  async function repairSteam({ onStep, onLog, options = {} } = {}) {
    if (_http()) {
      try {
        const { task_id } = await _post('/api/repair/start', options);
        return await _stream(task_id, { onStep, onLog });
      } catch(e) {
        return { ok: false, repairs: [], error: String(e) };
      }
    }

    const steps = ['kill','cache','temp','package','logs','dns'];
    if (options.steamtools)  steps.push('steamtools');
    if (options.plugin)   steps.push('plugin');
    steps.push('restart');
    for (const sid of steps) {
      onStep?.(sid, 'active');
      await _delay(500);
      onStep?.(sid, 'done');
      onLog?.(`[demo] ${sid} done`, 'success');
    }
    return { ok: true, repairs: steps.map(s => `${s} OK`) };
  }

  async function getSettings() {
    if (_http()) {
      const s = await _get('/api/settings');
      State.saveSettings(s);
      return s;
    }
    return State.getSettings();
  }

  async function saveSettings(patch) {
    if (_http()) {
      const r = await _post('/api/settings', patch);
      if (r.ok) State.saveSettings(patch);
      return r;
    }
    State.saveSettings(patch);
    return { ok: true };
  }

  async function validateSteamPath(path) {
    if (_http()) return _post('/api/steam/validate-path', { path });
    await _delay(400);
    return { ok: true, path, error: '' };
  }

  async function getInstalledGames() {
    if (_http()) {
      try { return await _get('/api/games/installed'); }
      catch (e) { return {}; }
    }
    return State.getInstalled();
  }

  async function removeGame(appid) {
    if (_http()) {
      const r = await _post('/api/games/remove', { appid: String(appid) });
      if (r.ok) State.removeInstalled(appid);
      return r;
    }
    try {
      if (window.pywebview && window.pywebview.api && window.pywebview.api.remove_game) {
        const result = await window.pywebview.api.remove_game(appid);
        if (result.ok) State.removeInstalled(appid);
        return result;
      }
    } catch (e) {}
    State.removeInstalled(appid);
    return { ok: true };
  }

  async function exportGamesJson() {
    if (_http()) {
      const r = await _get('/api/games/export');
      return r.data || '{}';
    }
    return State.exportInstalledJSON();
  }

  async function importGamesJson(jsonStr) {
    if (_http()) return _post('/api/games/import', { json: jsonStr });
    return State.importInstalledJSON(jsonStr);
  }

  function extractAppId(input) {
    const text = (input || '').trim();
    if (/^\d+$/.test(text)) return text;
    const patterns = [
      /steam:\/\/(?:run|rungameid)\/(\d+)/i,
      /store\.steampowered\.com\/app\/(\d+)/i,
      /\/app\/(\d+)/i,
      /appid\s*=?\s*(\d+)/i,
      /(\d{3,})/,
    ];
    for (const re of patterns) {
      const m = text.match(re);
      if (m) return m[1];
    }
    return '';
  }

  refreshSteamStatus().catch(() => {});

  async function getFixes() {
    if (_http()) return _get('/api/fixes');
    return { fixes: [] };
  }

  async function getGameFixes() {
    if (_http()) return _get('/api/game-fixes');
    return { fixes: [] };
  }

  async function detectFix(gameName, appid) {
    if (_http()) {
      return _post('/api/fixes/detect', { game_name: gameName, appid: appid || '' });
    }
    return { ok: false, found: false, error: 'Not available in mock mode' };
  }

  async function installFix(fixUrl, gamePath) {
    if (_http()) {
      return _post('/api/fixes/install', { fix_url: fixUrl, game_path: gamePath }, 120000);
    }
    return { ok: false, error: 'Not available in mock mode' };
  }

  async function applySpaceFix(appid) {
    if (_http()) {
      return _post('/api/fixes/space', { appid: appid, action: 'apply' });
    }
    return { ok: false, error: 'Not available in mock mode' };
  }

  async function removeSpaceFix(appid) {
    if (_http()) {
      return _post('/api/fixes/space', { appid: appid, action: 'remove' });
    }
    return { ok: false, error: 'Not available in mock mode' };
  }

  async function installCrackFix(fixUrl, gamePath) {
    if (_http()) {
      return _post('/api/fixes/crack', { fix_url: fixUrl, game_path: gamePath });
    }
    return { ok: false, error: 'Not available in mock mode' };
  }

  async function removeFix(appid, gamePath) {
    if (_http()) {
      return _post('/api/fixes/remove', { appid: appid, game_path: gamePath });
    }
    return { ok: false, error: 'Not available in mock mode' };
  }

  async function getLaunchOptions(appid, gamePath) {
    if (_http()) {
      return _post('/api/fixes/launch-options', { appid: appid, game_path: gamePath });
    }
    return { ok: false, launchOptions: '' };
  }

  async function pickFolder() {
    if (_http()) {
      var res = await _post('/api/pick-folder', {});
      return (res && res.path) || '';
    }
    if (window.pywebview && window.pywebview.api && window.pywebview.api.pick_folder) {
      try {
        return await window.pywebview.api.pick_folder() || '';
      } catch(e) {}
    }
    return '';
  }

  async function pickFile() {
    if (_http()) {
      var res = await _post('/api/pick-file', {});
      return (res && res.path) || '';
    }
    return '';
  }

  async function installFromManifest(files) {
    if (_http()) {
      const fd = new FormData();
      for (const f of files) {
        const blob = f.data instanceof Blob ? f.data : f;
        fd.append('file', blob, f.name || (f instanceof File ? f.name : 'file'));
      }
      const r = await fetch('/api/install/from-manifest', {
        method: 'POST',
        body: fd,
        signal: _makeSignal(120000),
      });
      if (!r.ok) throw new Error(`HTTP ${r.status}`);
      return r.json();
    }
    return { ok: false, error: 'Not available in mock mode' };
  }

  return {
    loadConfig,
    getSteamStatus, refreshSteamStatus, invalidateSteamCache, launchSteam,
    getAvailableGames, getGamesTotal, getGameByAppId, searchGames, invalidateGamesCache, gamesCacheExists, getGamesCache, getGameDetails,
    installGame, installBatch, updateGame, updateManifestOnly, extractAppId, INSTALL_STEPS, REPAIR_STEPS,
    runDiagnostic,
    repairSteam, checkMissingDlls,
    getSettings, saveSettings, validateSteamPath,
    getInstalledGames, removeGame, exportGamesJson, importGamesJson,
    getFixes, getGameFixes, detectFix, installFix, pickFolder, pickFile, installFromManifest,
    applySpaceFix, removeSpaceFix, installCrackFix, removeFix, getLaunchOptions,
  };
})();
