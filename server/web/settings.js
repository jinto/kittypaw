// GoPaw Settings Panel — Channel & LLM status

const Settings = {
  mount(container) {
    container.innerHTML = `
      <div class="settings-view">
        <h1>Settings</h1>
        <div id="settings-content" class="settings-content">
          <p class="hint">Loading...</p>
        </div>
      </div>`;
    this._load(document.getElementById('settings-content'));
  },

  async _load(container) {
    try {
      const s = await apiRaw('/api/setup/status');
      container.innerHTML = '';

      // --- Channels section ---
      const chSection = document.createElement('section');
      chSection.className = 'settings-section';
      chSection.innerHTML = '<h2>Channels</h2>';

      chSection.appendChild(this._channelRow(
        'Telegram',
        s.has_telegram,
        s.has_telegram ? `Chat ID: ${esc(s.telegram_chat_id || '')}` : null,
      ));

      chSection.appendChild(this._channelRow(
        'KakaoTalk',
        s.has_kakao,
        s.kakao_available ? null : 'Relay not available',
      ));

      container.appendChild(chSection);

      // --- LLM section ---
      const llmSection = document.createElement('section');
      llmSection.className = 'settings-section';
      llmSection.innerHTML = '<h2>LLM Provider</h2>';
      const llmRow = document.createElement('div');
      llmRow.className = 'settings-row';
      if (s.existing_provider) {
        llmRow.innerHTML = `
          <div class="settings-row-icon connected"></div>
          <div class="settings-row-body">
            <div class="settings-row-title">${esc(s.existing_provider)}</div>
            <div class="settings-row-sub">Connected</div>
          </div>`;
      } else {
        llmRow.innerHTML = `
          <div class="settings-row-icon"></div>
          <div class="settings-row-body">
            <div class="settings-row-title">Not configured</div>
            <div class="settings-row-sub">Run the setup wizard to configure</div>
          </div>`;
      }
      llmSection.appendChild(llmRow);
      container.appendChild(llmSection);

      // --- Wizard button ---
      const actions = document.createElement('div');
      actions.className = 'settings-actions';
      const wizBtn = document.createElement('button');
      wizBtn.className = 'btn btn--ghost';
      wizBtn.textContent = 'Re-run Setup Wizard';
      wizBtn.onclick = () => App.launchWizard();
      actions.appendChild(wizBtn);
      container.appendChild(actions);
    } catch (e) {
      container.innerHTML = `<div class="error-box">Failed to load settings: ${esc(String(e))}</div>`;
    }
  },

  _channelRow(name, connected, detail) {
    const row = document.createElement('div');
    row.className = 'settings-row';
    const statusClass = connected ? 'connected' : '';
    const statusText = connected ? 'Connected' : 'Not connected';
    const detailHtml = detail ? `<span class="settings-row-detail">${esc(detail)}</span>` : '';
    row.innerHTML = `
      <div class="settings-row-icon ${statusClass}"></div>
      <div class="settings-row-body">
        <div class="settings-row-title">${esc(name)} ${detailHtml}</div>
        <div class="settings-row-sub">${statusText}</div>
      </div>`;
    return row;
  },
};
