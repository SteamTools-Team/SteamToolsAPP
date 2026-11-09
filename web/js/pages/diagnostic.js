

const PageDiagnostic = (() => {
  let _running = false;

  function init() {
    document.getElementById('run-diagnostic-btn')?.addEventListener('click', _run);
  }

  async function _run() {
    if (_running) return;
    _running = true;

    const btn     = document.getElementById('run-diagnostic-btn');
    const idle    = document.getElementById('diagnostic-idle');
    const runEl   = document.getElementById('diagnostic-running');
    const results = document.getElementById('diagnostic-results');

    if (btn) {
      btn.disabled = true;
      btn.innerHTML = '<div class="spinner spinner--sm spinner--white"></div> Running...';
    }
    if (idle)    idle.style.display    = 'none';
    if (runEl)   runEl.style.display   = 'block';
    if (results) results.style.display = 'none';

    try {
      const report = await API.runDiagnostic({
        onResult: (id, data) => _handleResult(id, data),
      });

      if (runEl)   runEl.style.display   = 'none';
      if (results) results.style.display = 'block';

      _renderSummary(report);
      const issues = report.issues || [];
      State.addLog(`Diagnostic completed — ${issues.length === 0 ? 'all OK' : issues.join(', ')}`, report.ok ? 'success' : 'warning');
    } finally {
      _running = false;
      if (btn) {
        btn.disabled = false;
        btn.innerHTML = `
          <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
            <polyline points="22 12 18 12 15 21 9 3 6 12 2 12"/>
          </svg>
          Run Diagnostic`;
      }
    }
  }

  function _handleResult(id, data) {
    if (id === 'detect')    _renderDetect(data);
    if (id === 'files')     _renderFiles(data);
    if (id === 'dirs')      _renderDirs(data);
    if (id === 'users')     _renderUsers(data);
    if (id === 'processes') _renderProcesses(data);
    if (id === 'disk')      _renderDisk(data);
  }

  function _renderDetect(data) {
    const pathEl   = document.getElementById('diag-detect-path');
    const statusEl = document.getElementById('diag-detect-status');
    const iconEl   = document.querySelector('#diag-steam-detect .diag-card-icon');

    if (pathEl)   pathEl.textContent = data.path || 'Not detected';
    if (statusEl) statusEl.innerHTML = data.ok ? badgeHtml('Detected', 'green') : badgeHtml('Not found', 'red');
    if (iconEl) {
      iconEl.className = `diag-card-icon ${data.ok ? 'diag-card-icon--green' : 'diag-card-icon--red'}`;
      iconEl.innerHTML = data.ok
        ? `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>`
        : `<svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>`;
    }
  }

  function _renderFiles(data) {
    const sub    = document.getElementById('diag-files-sub');
    const status = document.getElementById('diag-files-status');
    const body   = document.getElementById('diag-files-body');
    const iconEl = document.querySelector('#diag-critical-files .diag-card-icon');

    const files = data.files || [];
    const allOk = files.every(f => f.ok);
    const okCnt = files.filter(f => f.ok).length;

    if (sub)    sub.textContent  = `${okCnt}/${files.length} files present`;
    if (status) status.innerHTML = allOk ? badgeHtml('All OK', 'green') : badgeHtml(`${files.length - okCnt} missing`, 'red');
    if (iconEl) iconEl.className = `diag-card-icon ${allOk ? 'diag-card-icon--green' : 'diag-card-icon--red'}`;

    if (body) {
      body.innerHTML = files.map(f => `
        <div class="diag-check-item">
          <span class="diag-check-name">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/>
            </svg>
            ${escapeHtml(f.name)}
          </span>
          ${statusBadgeHtml(f.ok, 'Found', 'Missing')}
        </div>
      `).join('');
    }
  }

  function _renderDirs(data) {
    const sub    = document.getElementById('diag-dirs-sub');
    const status = document.getElementById('diag-dirs-status');
    const body   = document.getElementById('diag-dirs-body');
    const iconEl = document.querySelector('#diag-directories .diag-card-icon');

    const dirs = data.dirs || [];
    const allOk = dirs.every(d => d.ok);
    const okCnt = dirs.filter(d => d.ok).length;

    if (sub)    sub.textContent  = `${okCnt}/${dirs.length} directories OK`;
    if (status) status.innerHTML = allOk
      ? badgeHtml('Complete', 'green')
      : badgeHtml(`${dirs.length - okCnt} missing`, dirs.length - okCnt > 2 ? 'red' : 'yellow');
    if (iconEl) iconEl.className = `diag-card-icon ${allOk ? 'diag-card-icon--green' : 'diag-card-icon--yellow'}`;

    if (body) {
      body.innerHTML = dirs.map(d => `
        <div class="diag-check-item">
          <span class="diag-check-name">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/>
            </svg>
            ${escapeHtml(d.name)}
          </span>
          ${statusBadgeHtml(d.ok, 'OK', 'Missing')}
        </div>
      `).join('');
    }
  }

  function _renderUsers(data) {
    const sub    = document.getElementById('diag-users-sub');
    const status = document.getElementById('diag-users-status');
    const body   = document.getElementById('diag-users-body');
    const iconEl = document.querySelector('#diag-users .diag-card-icon');

    if (sub) sub.textContent = data.count > 0
      ? `${data.count} user${data.count !== 1 ? 's' : ''} detected`
      : 'No users found';
    if (status) status.innerHTML = data.count > 0
      ? badgeHtml(`${data.count} user${data.count !== 1 ? 's' : ''}`, 'green')
      : badgeHtml('No users', 'yellow');
    if (iconEl) iconEl.className = `diag-card-icon ${data.count > 0 ? 'diag-card-icon--green' : 'diag-card-icon--yellow'}`;

    if (body && (data.users || []).length > 0) {
      body.innerHTML = (data.users || []).map(u => `
        <div class="diag-check-item">
          <span class="diag-check-name">
            <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M20 21v-2a4 4 0 0 0-4-4H8a4 4 0 0 0-4 4v2"/><circle cx="12" cy="7" r="4"/>
            </svg>
            User ${u.id}
          </span>
          ${statusBadgeHtml(u.configOk, 'Config OK', 'Config missing')}
        </div>
      `).join('');
    } else if (body) {
      body.innerHTML = '<p style="font-size:0.78rem;color:var(--text-3);padding:4px 0">No Steam user profiles found in userdata/</p>';
    }
  }

  function _renderProcesses(data) {
    const sub    = document.getElementById('diag-procs-sub');
    const status = document.getElementById('diag-procs-status');
    const body   = document.getElementById('diag-procs-body');
    const iconEl = document.querySelector('#diag-processes .diag-card-icon');

    if (sub) sub.textContent = data.processes.length > 0
      ? `${data.processes.length} process${data.processes.length !== 1 ? 'es' : ''} active`
      : 'No Steam processes';
    if (status) status.innerHTML = data.processes.length > 0
      ? badgeHtml('Running', 'green')
      : badgeHtml('Stopped', 'neutral');
    if (iconEl) iconEl.className = `diag-card-icon ${data.processes.length > 0 ? 'diag-card-icon--green' : ''}`;

    if (body) {
      body.innerHTML = data.processes.length > 0
        ? data.processes.map(p => `
          <div class="diag-check-item">
            <span class="diag-check-name">${escapeHtml(p.name)}</span>
            <span style="font-size:0.72rem;color:var(--text-3);font-family:monospace">PID ${p.pid}</span>
          </div>
        `).join('')
        : '<p style="font-size:0.78rem;color:var(--text-3);padding:4px 0">Steam is not currently running</p>';
    }
  }

  function _renderDisk(data) {
    const sub      = document.getElementById('diag-disk-sub');
    const status   = document.getElementById('diag-disk-status');
    const barEl    = document.getElementById('diag-disk-bar-container');
    const fillEl   = document.getElementById('diag-disk-bar-fill');
    const labelsEl = document.getElementById('diag-disk-bar-labels');
    const iconEl   = document.querySelector('#diag-disk .diag-card-icon');

    const pct  = Math.round((data.usedGB / data.totalGB) * 100);
    const warn = data.freeGB < 10;
    const crit = data.freeGB < 5;

    if (sub)    sub.textContent  = `${data.freeGB} GB free of ${data.totalGB} GB`;
    if (status) status.innerHTML = crit
      ? badgeHtml('Critical', 'red')
      : warn ? badgeHtml('Low space', 'yellow') : badgeHtml('OK', 'green');
    if (iconEl) iconEl.className = `diag-card-icon ${crit ? 'diag-card-icon--red' : warn ? 'diag-card-icon--yellow' : 'diag-card-icon--green'}`;

    if (barEl) barEl.style.display = 'block';
    if (fillEl) {
      fillEl.style.width = `${pct}%`;
      if (warn) fillEl.className = 'disk-bar-fill disk-bar-fill--warning';
    }
    if (labelsEl) labelsEl.innerHTML = `<span>${data.usedGB} GB used (${pct}%)</span><span>${data.freeGB} GB free</span>`;
  }

  function _renderSummary(report) {
    const el = document.getElementById('diagnostic-summary');
    if (!el) return;

    const issues = report.issues || [];

    if (report.ok) {
      el.className = 'diagnostic-summary ok';
      el.innerHTML = `
        <div class="diag-summary-icon">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><polyline points="20 6 9 17 4 12"/></svg>
        </div>
        <div class="diag-summary-text">
          <h4>Steam installation is healthy</h4>
          <p>All critical files, directories, and services are in good shape.</p>
        </div>
      `;
    } else {
      const issueCount = issues.length;
      el.className = issueCount > 1 ? 'diagnostic-summary error' : 'diagnostic-summary warn';
      el.innerHTML = `
        <div class="diag-summary-icon">
          <svg width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5">
            <path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/>
            <line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
          </svg>
        </div>
        <div class="diag-summary-text">
          <h4>${issueCount} issue${issueCount !== 1 ? 's' : ''} detected</h4>
          <p>${issues.join(' · ')} — Use the Repair page to fix these.</p>
        </div>
      `;
    }
  }

  return { init };
})();
