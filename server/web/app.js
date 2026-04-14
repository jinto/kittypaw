// KittyPaw Web App — Router + Auth Bootstrap + Tab Navigation

const App = {
  root: null,
  apiKey: null,
  wsUrl: null,
  activeTab: null,
  _dashboardInterval: null,

  async init() {
    this.root = document.getElementById('app');
    const status = await apiRaw('/api/setup/status');
    if (!status.completed) {
      Onboarding.start(this.root, status);
    } else {
      await this.bootstrap();
      this.showShell();
    }
  },

  async bootstrap() {
    try {
      const data = await apiRaw('/api/bootstrap');
      this.apiKey = data.api_key;
      this.wsUrl = data.ws_url;
    } catch (e) {
      console.error('Bootstrap failed:', e);
    }
  },

  _chatMounted: false,

  _teardown() {
    if (this._chatMounted) {
      if (Chat.ws) { Chat.ws.onclose = null; Chat.ws.close(); Chat.ws = null; }
      if (Chat.reconnectTimer) { clearTimeout(Chat.reconnectTimer); Chat.reconnectTimer = null; }
    }
    if (this._dashboardInterval) { clearInterval(this._dashboardInterval); this._dashboardInterval = null; }
    this._chatMounted = false;
    this.activeTab = null;
  },

  showShell() {
    this._teardown();

    // Override #app centering from stylesheet
    this.root.style.display = 'block';
    this.root.style.alignItems = '';
    this.root.style.justifyContent = '';
    this.root.innerHTML = `
      <div class="shell">
        <aside class="sidebar">
          <div class="sidebar-logo">Kitty<span class="accent">Paw</span></div>
          <nav class="sidebar-nav">
            <button class="nav-item" data-tab="chat">Chat</button>
            <button class="nav-item" data-tab="dashboard">Dashboard</button>
            <button class="nav-item" data-tab="skills">Skills</button>
            <button class="nav-item" data-tab="settings">Settings</button>
          </nav>
          <div class="sidebar-footer">
            <button class="sidebar-wizard-btn" id="wizard-btn">\uC124\uC815 \uC704\uC790\uB4DC</button>
            <span class="sidebar-version">v0.1.0</span>
          </div>
        </aside>
        <main class="main-content">
          <div id="chat-panel" style="display:none"></div>
          <div id="tab-content"></div>
        </main>
      </div>`;

    this.root.querySelectorAll('.nav-item').forEach(btn => {
      btn.addEventListener('click', () => this.switchTab(btn.dataset.tab));
    });

    document.getElementById('wizard-btn').addEventListener('click', () => this.launchWizard());

    this.switchTab('chat');
  },

  switchTab(tab) {
    if (tab === this.activeTab) return;
    const prev = this.activeTab;
    this.activeTab = tab;

    this.root.querySelectorAll('.nav-item').forEach(btn => {
      btn.classList.toggle('active', btn.dataset.tab === tab);
    });

    if (this._dashboardInterval) {
      clearInterval(this._dashboardInterval);
      this._dashboardInterval = null;
    }

    const chatPanel = document.getElementById('chat-panel');
    const content = document.getElementById('tab-content');

    // Hide/destroy previous
    if (prev === 'chat') {
      chatPanel.style.display = 'none';
    } else if (prev) {
      content.innerHTML = '';
    }

    // Show/mount new
    if (tab === 'chat') {
      chatPanel.style.display = 'flex';
      content.style.display = 'none';
      if (!this._chatMounted) {
        Chat.mount(chatPanel);
        this._chatMounted = true;
      }
    } else {
      content.style.display = '';
      if (tab === 'dashboard') {
        this._showDashboard(content);
      } else if (tab === 'skills') {
        Skills.mount(content);
      } else if (tab === 'settings') {
        Settings.mount(content);
      }
    }
  },

  async launchWizard() {
    await apiPost('/api/setup/reset', {});
    this._teardown();
    this.apiKey = null;
    // Restore #app centering (undo showShell inline overrides)
    this.root.style.cssText = '';
    const status = await apiRaw('/api/setup/status');
    Onboarding.start(this.root, status);
  },

  _showDashboard(container) {
    container.innerHTML = `
      <div class="dashboard">
        <h1>\u{1F43E} KittyPaw Dashboard</h1>
        <p class="hint">Auto-refreshes every 30s</p>
        <div class="stats-grid" id="stats"></div>
        <h2>Agents</h2>
        <table><thead><tr><th>Agent ID</th><th>Turns</th><th>Created</th><th>Last Active</th></tr></thead>
        <tbody id="agents"></tbody></table>
        <h2 class="mt-20">Recent Executions</h2>
        <table><thead><tr><th>Time</th><th>Skill</th><th>Status</th><th>Duration</th><th>Summary</th></tr></thead>
        <tbody id="exec"></tbody></table>
      </div>`;
    this._refreshDashboard();
    this._dashboardInterval = setInterval(() => this._refreshDashboard(), 30000);
  },

  async _refreshDashboard() {
    try {
      const s = await api('/api/v1/status');
      const statsEl = document.getElementById('stats');
      if (statsEl) {
        statsEl.innerHTML =
          statCard(s.total_runs || 0, "Today's Runs") +
          statCard(s.successful || 0, 'Successful', 'ok') +
          statCard(s.failed || 0, 'Failed', 'fail') +
          statCard(s.total_tokens || 0, 'Tokens');
      }

      const agentsData = await api('/api/v1/agents');
      const agents = agentsData.agents || [];
      const agentsEl = document.getElementById('agents');
      if (agentsEl) {
        agentsEl.innerHTML = agents.length
          ? agents.map(a =>
            `<tr><td>${esc(a.AgentID || a.agent_id)}</td><td>${esc(String(a.TurnCount || a.turn_count || 0))}</td>` +
            `<td>${esc(((a.CreatedAt || a.created_at) || '').slice(0,19))}</td>` +
            `<td>${esc(((a.UpdatedAt || a.updated_at) || '').slice(0,19))}</td></tr>`
          ).join('')
          : '<tr><td colspan="4">No agents yet</td></tr>';
      }

      const execData = await api('/api/v1/executions');
      const execs = execData.executions || [];
      const execEl = document.getElementById('exec');
      if (execEl) {
        execEl.innerHTML = execs.length
          ? execs.map(r =>
            `<tr><td>${esc(((r.StartedAt || r.started_at) || '').slice(0,19))}</td>` +
            `<td>${esc(r.SkillName || r.skill_name || '')}</td>` +
            `<td class="${(r.Success || r.success) ? 'ok' : 'fail'}">${(r.Success || r.success) ? 'OK' : 'FAIL'}</td>` +
            `<td>${esc(String(r.DurationMs || r.duration_ms || 0))}ms</td>` +
            `<td>${esc(((r.ResultSummary || r.result_summary) || '').slice(0,60))}</td></tr>`
          ).join('')
          : '<tr><td colspan="5">No executions yet</td></tr>';
      }
    } catch (e) { console.error('Dashboard refresh failed:', e); }
  },
};

function statCard(value, label, cls) {
  return `<div class="stat-card"><div class="value ${cls||''}">${esc(String(value))}</div><div class="label">${esc(label)}</div></div>`;
}

// ── Helpers ──────────────────────────────────────────────

function esc(s) {
  const d = document.createElement('div');
  d.textContent = s == null ? '' : String(s);
  return d.innerHTML;
}

/** Fetch without auth (for setup/bootstrap endpoints). */
async function apiRaw(url, opts) {
  const res = await fetch(url, opts);
  return res.json();
}

/** Fetch with Bearer auth header. */
async function api(url, opts = {}) {
  if (App.apiKey) {
    opts.headers = Object.assign({}, opts.headers || {}, { Authorization: `Bearer ${App.apiKey}` });
  }
  const res = await fetch(url, opts);
  return res.json();
}

async function apiPost(url, body) {
  return apiRaw(url, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(body),
  });
}

// ── Boot ─────────────────────────────────────────────────

document.addEventListener('DOMContentLoaded', () => App.init());
