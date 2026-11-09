

const State = (() => {
  const KEYS = {
    INSTALLED:    'st_installed_games',
    SETTINGS:     'st_settings',
    ACTIVITY_LOG: 'st_activity_log',
    CREDITS:      'st_credits_info',
    QUEUE:        'st_download_queue',
  };

  const DEFAULT_SETTINGS = {
    custom_steam_path:  '',
    auto_restart:       true,
    auto_close_steam:   true,
    confirm_remove:     true,
    animations:         true,
    compact:            false,
    theme:              'default',
    provider:           'steamtools',
    hubcap_api_key:     '',
    image_cache:        true,
    download_queue:     false,
  };

  function read(key, fallback) {
    try {
      const raw = localStorage.getItem(key);
      return raw ? JSON.parse(raw) : fallback;
    } catch {
      return fallback;
    }
  }

  function write(key, value) {
    try {
      localStorage.setItem(key, JSON.stringify(value));
      return true;
    } catch {
      return false;
    }
  }

  function getInstalled() {
    return read(KEYS.INSTALLED, {});
  }

  function setInstalled(records) {
    return write(KEYS.INSTALLED, records);
  }

  function addInstalled(appid, name, installdir) {
    const records = getInstalled();
    records[String(appid)] = {
      name:       name,
      installdir: installdir || sanitizeDir(name, appid),
      timestamp:  Date.now(),
    };
    setInstalled(records);
    _dispatchChange('installed');
  }

  function removeInstalled(appid) {
    const records = getInstalled();
    delete records[String(appid)];
    setInstalled(records);
    _dispatchChange('installed');
  }

  function removeAllInstalled() {
    setInstalled({});
    _dispatchChange('installed');
  }

  function getInstalledCount() {
    return Object.keys(getInstalled()).length;
  }

  function isInstalled(appid) {
    return !!getInstalled()[String(appid)];
  }

  function getSettings() {
    const saved = read(KEYS.SETTINGS, {});
    return { ...DEFAULT_SETTINGS, ...saved };
  }

  function saveSettings(patch) {
    const current = getSettings();
    const merged  = { ...current, ...patch };
    write(KEYS.SETTINGS, merged);
    _dispatchChange('settings');
    return merged;
  }

  function getSetting(key) {
    return getSettings()[key];
  }

  function getLog() {
    return read(KEYS.ACTIVITY_LOG, []);
  }

  function addLog(msg, type = 'info') {
    const log   = getLog();
    const entry = {
      msg,
      type,
      time: new Date().toLocaleTimeString('en-US', { hour12: false }),
      ts:   Date.now(),
    };
    log.unshift(entry);
    if (log.length > 100) log.length = 100;
    write(KEYS.ACTIVITY_LOG, log);
    _dispatchChange('log');
    return entry;
  }

  function clearLog() {
    write(KEYS.ACTIVITY_LOG, []);
    _dispatchChange('log');
  }

  function getQueue() {
    return read(KEYS.QUEUE, []);
  }

  function setQueue(items) {
    return write(KEYS.QUEUE, items);
  }

  function addToQueue(item) {
    const items = getQueue();
    items.push(item);
    setQueue(items);
    _dispatchChange('queue');
  }

  function updateQueueItem(appid, patch) {
    const items = getQueue().map(it => it.appid === String(appid) ? { ...it, ...patch } : it);
    setQueue(items);
    _dispatchChange('queue');
  }

  function removeFromQueue(appid) {
    const items = getQueue().filter(it => it.appid !== String(appid));
    setQueue(items);
    _dispatchChange('queue');
  }

  function clearQueue() {
    setQueue([]);
    _dispatchChange('queue');
  }

  function getCredits() {
    return read(KEYS.CREDITS, { site: '', discord: '', github: '', pseudo: '' });
  }

  function saveCredits(data) {
    write(KEYS.CREDITS, data);
  }

  function sanitizeDir(name, appid) {
    const base = (name || '').trim().replace(/[<>:"/\\|?*\x00-\x1F]/g, '_').slice(0, 60).trim();
    return base || `Game_${appid || 'unknown'}`;
  }

  function _dispatchChange(area) {
    window.dispatchEvent(new CustomEvent('statechange', { detail: { area } }));
  }

  function clearAll() {
    Object.values(KEYS).forEach(k => localStorage.removeItem(k));
    _dispatchChange('all');
  }

  function exportInstalledJSON() {
    const records = getInstalled();
    return JSON.stringify({ installed: records }, null, 2);
  }

  function importInstalledJSON(jsonStr) {
    try {
      const data    = JSON.parse(jsonStr);
      const records = data.installed || data;
      if (typeof records !== 'object' || Array.isArray(records)) throw new Error('Invalid format');
      const games = [];
      for (const [appid, entry] of Object.entries(records)) {
        if (!String(appid).match(/^\d+$/)) continue;
        games.push({ appid: String(appid), name: entry.name || `Game_${appid}` });
      }
      return { ok: true, games };
    } catch (e) {
      return { ok: false, error: e.message };
    }
  }

  return {
    getInstalled, setInstalled, addInstalled, removeInstalled,
    removeAllInstalled, getInstalledCount, isInstalled,
    getSettings, saveSettings, getSetting,
    getLog, addLog, clearLog,
    getCredits, saveCredits,
    sanitizeDir, clearAll,
    exportInstalledJSON, importInstalledJSON,
    getQueue, setQueue, addToQueue, updateQueueItem, removeFromQueue, clearQueue,
  };
})();
