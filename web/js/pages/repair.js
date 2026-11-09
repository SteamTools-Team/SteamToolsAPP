

const PageRepair = (() => {
  let _running = false;

  function init() {
    _initSteps();
    document.getElementById('start-repair-btn')?.addEventListener('click', _startRepair);
    document.getElementById('repair-done-btn')?.addEventListener('click', _resetRepair);
    document.getElementById('repair-restart-steam-btn')?.addEventListener('click', _restartSteam);

  }

  function _initSteps() {
    renderRepairSteps('repair-steps', API.REPAIR_STEPS);
  }

  async function _startRepair() {
    if (_running) return;

    const steam = API.getSteamStatus();
    if (!steam.detected) {
      Toast.error('Steam not found', 'Cannot repair: Steam installation not detected. Check Settings.');
      return;
    }

    const cloudredirect    = document.getElementById('repair-cloudredirect-toggle')?.checked   ?? false;
    let bettersteamtools   = document.getElementById('repair-bettersteamtools-toggle')?.checked ?? false;

    if (!bettersteamtools) {
      const res = await API.checkMissingDlls();
      if (res.ok && !res.all_present && res.missing?.length) {
        const bstEl = document.getElementById('repair-bettersteamtools-toggle');
        if (bstEl) bstEl.checked = true;
        bettersteamtools = true;
        appendLog('repair-log', `Missing DLLs detected (${res.missing.join(', ')}) — BetterSteamTools (BST Fix) auto-selected`, 'info');
      }
    }

    const confirmed = await Modal.open({
      titleText:   'Start Steam Repair',
      bodyHtml:    `<p>This will perform a full Steam repair:</p>
        <ul style="margin:10px 0 0 16px;font-size:0.82rem;line-height:2;color:var(--text-2)">
          <li>Close Steam if running</li>
          <li>Clear all Steam caches</li>
          <li>Fix corrupted manifests</li>
          <li>Flush DNS cache</li>
          ${cloudredirect    ? '<li>CloudRedirect (CloudRedirect CLI + DLL)</li>' : ''}
          ${bettersteamtools ? '<li>BetterSteamTools (BST Fix) — dwmapi.dll, xinput1_4.dll, OpenSteamTool.dll</li>' : ''}
        </ul>
        <p style="margin-top:12px;font-size:0.78rem;color:var(--yellow)">Steam will be closed and automatically restarted after repair.</p>`,
      confirmText: 'Start Repair',
      cancelText:  'Cancel',
    });

    if (!confirmed) return;

    _running = true;
    _resetUI();

    const startBtn = document.getElementById('start-repair-btn');
    const badge    = document.getElementById('repair-status-badge');

    if (startBtn) {
      startBtn.disabled = true;
      startBtn.innerHTML = '<div class="spinner spinner--sm spinner--white"></div> Repairing...';
    }
    if (badge) {
      badge.className = 'badge badge--accent';
      badge.textContent = 'Running';
    }

    const log = document.getElementById('repair-log');
    if (log) log.innerHTML = '';

    try {
      const result = await API.repairSteam({
        onStep:  (id, status, detail) => updateRepairStep(id, status, detail),
        onLog:   (msg, type)          => appendLog('repair-log', msg, type),
        options: { cloudredirect, bettersteamtools },
      });

      if (badge) {
        badge.className  = result.ok ? 'badge badge--green' : 'badge badge--yellow';
        badge.textContent = result.ok ? 'Complete' : 'Partial';
      }

      _showComplete(result);

      Toast[result.ok ? 'success' : 'warning'](
        result.ok ? 'Repair Complete' : 'Repair Partial',
        result.ok
          ? `${result.repairs.length} repairs applied successfully`
          : 'Some repair steps encountered issues'
      );
    } catch (err) {
      if (badge) { badge.className = 'badge badge--red'; badge.textContent = 'Error'; }
      Toast.error('Repair Failed', err.message);
      appendLog('repair-log', `Repair error: ${err.message}`, 'error');
    } finally {
      _running = false;
      if (startBtn) {
        startBtn.disabled = false;
        startBtn.innerHTML = `
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <path d="M14.7 6.3a1 1 0 0 0 0 1.4l1.6 1.6a1 1 0 0 0 1.4 0l3.77-3.77a6 6 0 0 1-7.94 7.94l-6.91 6.91a2.12 2.12 0 0 1-3-3l6.91-6.91a6 6 0 0 1 7.94-7.94l-3.76 3.76z"/>
          </svg>
          Start Repair`;
      }
    }
  }

  function _showComplete(result) {
    const actionsEl  = document.getElementById('repair-complete-actions');
    const messageEl  = document.getElementById('repair-complete-message');
    if (!actionsEl) return;

    actionsEl.style.display = 'block';

    if (messageEl) {
      messageEl.className = `repair-complete-message ${result.ok ? 'success' : 'warn'}`;
      if (result.ok) {
        messageEl.innerHTML = `
          <h4>
            <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" style="margin-right:4px"><polyline points="20 6 9 17 4 12"/></svg>
            Repair completed successfully!
          </h4>
          <p>${result.repairs.length} repair action${result.repairs.length !== 1 ? 's' : ''} applied. Error should now be resolved. Restart Steam to apply changes.</p>
        `;
      } else {
        messageEl.innerHTML = `
          <h4 style="color:var(--yellow)">Repair finished with warnings</h4>
          <p>Some steps may have encountered issues. Check the log for details and consider running the repair again or reinstalling Steam.</p>
        `;
      }
    }
  }

  function _resetUI() {
    const actionsEl = document.getElementById('repair-complete-actions');
    if (actionsEl) actionsEl.style.display = 'none';
    _initSteps();
  }

  function _resetRepair() {
    _resetUI();
    const badge = document.getElementById('repair-status-badge');
    if (badge) { badge.className = 'badge'; badge.textContent = 'Idle'; }
    const log = document.getElementById('repair-log');
    if (log) log.innerHTML = '';
  }

  async function _restartSteam() {
    const btn = document.getElementById('repair-restart-steam-btn');
    if (btn) {
      btn.disabled = true;
      btn.innerHTML = '<div class="spinner spinner--sm spinner--white"></div> Restarting...';
    }

    try {
      await API.launchSteam();
      Toast.success('Steam Restarted', 'Steam has been restarted');
      appendLog('repair-log', 'Steam restarted successfully', 'success');
    } catch {
      Toast.error('Restart Failed', 'Could not restart Steam');
    } finally {
      if (btn) {
        btn.disabled = false;
        btn.textContent = 'Restart Steam';
      }
    }
  }

  return { init };
})();
