// KittyPaw Onboarding — 7-step flow

const Onboarding = {
  root: null,
  step: 1,
  totalSteps: 7,
  status: {},
  state: {
    llm: { choice: null, localUrl: 'http://localhost:11434/v1', localModel: 'qwen3.5:27b', apiKey: '' },
    telegram: { botToken: '', chatId: '' },
    kakao: { pairCode: '', channelUrl: '' },
    workspace: { path: '' },
  },

  start(root, status) {
    this.root = root;
    this.status = status;
    this.step = 1;
    this.render();
  },

  go(step) {
    if (step === 4 && !this.status.kakao_available) step = 5;
    this.step = Math.max(1, Math.min(step, this.totalSteps));
    this.render();
  },

  render() {
    this._kakaoPollActive = false;
    const steps = {
      1: () => this.stepWelcome(),
      2: () => this.stepLlm(),
      3: () => this.stepTelegram(),
      4: () => this.stepKakaoTalk(),
      5: () => this.stepWorkspace(),
      6: () => this.stepHttpAccess(),
      7: () => this.stepComplete(),
    };
    this.root.innerHTML = '';
    if (this.step >= 2 && this.step <= 6) this.root.appendChild(this.backButton());
    if (this.step >= 1 && this.step <= 6) this.root.appendChild(this.dots());
    const content = (steps[this.step] || steps[1])();
    this.root.appendChild(content);
  },

  backButton() {
    const btn = el('button', { className: 'back-btn', onclick: () => {
      let prev = this.step - 1;
      if (prev === 4 && !this.status.kakao_available) prev = 3;
      this.go(prev);
    }}, '\u2190 \uB4A4\uB85C');
    return btn;
  },

  dots() {
    const wrap = el('div', { className: 'onboarding-dots' });
    const steps = [1,2,3,4,5,6].filter(i => i !== 4 || this.status.kakao_available);
    steps.forEach(i => {
      const cls = i === this.step ? 'dot current' : i < this.step ? 'dot done' : 'dot';
      wrap.appendChild(el('div', { className: cls }));
    });
    return wrap;
  },

  // ── Step 1: Welcome ────────────────────────────────────

  stepWelcome() {
    const card = el('div', { className: 'card card--center' });
    card.innerHTML = `
      <h1>Kitty<span class="accent">Paw</span></h1>
      <p class="sub mt-12 mb-40">AI \uC790\uB3D9\uD654\uB97C 3\uBD84 \uC548\uC5D0 \uC2DC\uC791\uD558\uC138\uC694</p>`;
    const btn = el('button', {
      className: 'btn btn--primary',
      onclick: () => this.go(2),
    }, '\uC2DC\uC791\uD558\uAE30');
    card.appendChild(btn);
    return card;
  },

  // ── Step 2: LLM Selection ──────────────────────────────

  stepLlm() {
    if (this.status.existing_provider && this.state.llm.choice === null) {
      return this._stepLlmExisting();
    }
    return this._stepLlmSelect();
  },

  _stepLlmExisting() {
    const names = { anthropic: 'Claude API', claude: 'Claude API', openrouter: 'OpenRouter', openai: 'OpenAI', local: '\uB85C\uCEEC LLM' };
    const name = names[this.status.existing_provider] || esc(this.status.existing_provider);
    const card = el('div', { className: 'card' });
    card.innerHTML = `
      <h2 class="large">AI \uBAA8\uB378\uC744 \uC120\uD0DD\uD558\uC138\uC694</h2>
      <div class="flex flex-col gap-12 mt-28">
        <div class="info-box">
          <div style="font-weight:600;margin-bottom:4px">\uC774\uBBF8 \uC124\uC815\uB41C AI \uBAA8\uB378\uC774 \uC788\uC2B5\uB2C8\uB2E4</div>
          <div class="hint">${name}</div>
        </div>
      </div>`;
    const useBtn = el('button', {
      className: 'btn btn--primary btn--block mt-12',
      onclick: () => this.go(3),
    });
    useBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uC774 \uC124\uC815 \uC0AC\uC6A9</div><div class="hint">\uAE30\uC874 AI \uBAA8\uB378\uC744 \uADF8\uB300\uB85C \uC0AC\uC6A9\uD569\uB2C8\uB2E4</div>';
    const reBtn = el('button', {
      className: 'btn btn--ghost btn--block',
      onclick: () => { this.state.llm.choice = ''; this.render(); },
    });
    reBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uB2E4\uC2DC \uC120\uD0DD</div><div class="hint">\uB2E4\uB978 AI \uBAA8\uB378\uB85C \uBCC0\uACBD\uD569\uB2C8\uB2E4</div>';
    card.querySelector('.flex').appendChild(useBtn);
    card.querySelector('.flex').appendChild(reBtn);
    return card;
  },

  _stepLlmSelect() {
    const s = this.state.llm;
    const card = el('div', { className: 'card' });
    card.innerHTML = `<h2 class="large">AI \uBAA8\uB378\uC744 \uC120\uD0DD\uD558\uC138\uC694</h2>`;

    card.appendChild(this._llmCard('local', '\uB85C\uCEEC LLM (Ollama)', '\uBB34\uB8CC, \uB0B4 \uCEF4\uD4E8\uD130\uC5D0\uC11C \uC2E4\uD589', () => {
      if (s.choice !== 'local') return '';
      return `<div class="flex flex-col gap-10 mt-16">
        <div><label>\uC11C\uBC84 URL</label><input class="input" type="text" value="${esc(s.localUrl)}" data-field="localUrl"></div>
        <div><label>\uBAA8\uB378 \uC774\uB984</label><input class="input" type="text" value="${esc(s.localModel)}" data-field="localModel"></div>
      </div>`;
    }));

    card.appendChild(el('div', { style: 'height:12px' }));

    card.appendChild(this._llmCard('openrouter', 'OpenRouter (\uBB34\uB8CC)', '\uBB34\uB8CC AI \uBAA8\uB378\uB85C \uBC14\uB85C \uC2DC\uC791\uD558\uC138\uC694', () => {
      if (s.choice !== 'openrouter') return '';
      return `<div class="mt-16">
        <label>API \uD0A4</label>
        <input class="input input--mono" type="password" placeholder="sk-or-..." value="${esc(s.apiKey)}" data-field="apiKey">
        <div class="hint mt-12" style="line-height:1.8">
          1. <a href="https://openrouter.ai/settings/keys" style="color:var(--accent);text-decoration:underline">openrouter.ai</a> \uC5D0\uC11C \uBB34\uB8CC \uAC00\uC785<br>
          2. API Keys \u2192 Create Key<br>
          3. \uBC1C\uAE09\uB41C \uD0A4\uB97C \uC5EC\uAE30\uC5D0 \uBD99\uC5EC\uB123\uAE30
        </div>
      </div>`;
    }));

    card.appendChild(el('div', { style: 'height:12px' }));

    card.appendChild(this._llmCard('claude', 'Claude API', '\uACE0\uD488\uC9C8, API \uD0A4 \uD544\uC694', () => {
      if (s.choice !== 'claude') return '';
      return `<div class="mt-16">
        <label>API \uD0A4</label>
        <input class="input input--mono" type="password" placeholder="sk-ant-..." value="${esc(s.apiKey)}" data-field="apiKey">
      </div>`;
    }));

    const canProceed = s.choice === 'local' ? s.localUrl && s.localModel
      : s.choice === 'openrouter' || s.choice === 'claude' ? s.apiKey
      : false;

    const actions = el('div', { className: 'flex justify-end mt-28' });
    const nextBtn = el('button', {
      className: 'btn btn--primary',
      disabled: !canProceed,
      onclick: async () => {
        nextBtn.disabled = true;
        nextBtn.textContent = '\uC800\uC7A5 \uC911...';
        const body = { provider: s.choice };
        if (s.choice === 'local') { body.local_url = s.localUrl; body.local_model = s.localModel; }
        else { body.api_key = s.apiKey; }
        await apiPost('/api/setup/llm', body);
        this.go(3);
      },
    }, '\uB2E4\uC74C');
    actions.appendChild(nextBtn);
    card.appendChild(actions);

    card.addEventListener('input', e => {
      if (e.target.dataset.field) {
        s[e.target.dataset.field] = e.target.value;
        const cp = s.choice === 'local' ? s.localUrl && s.localModel
          : s.choice === 'openrouter' || s.choice === 'claude' ? s.apiKey : false;
        nextBtn.disabled = !cp;
      }
    });

    return card;
  },

  _llmCard(value, title, desc, extraHtml) {
    const s = this.state.llm;
    const isActive = s.choice === value;
    const card = el('div', {
      className: `radio-card ${isActive ? 'active' : ''}`,
      onclick: () => { s.choice = value; this.render(); },
    });
    card.innerHTML = `
      <div class="flex items-center gap-10 mb-4">
        <div class="radio-dot ${isActive ? 'active' : ''}"></div>
        <span style="font-size:14px;font-weight:600">${title}</span>
      </div>
      <p class="hint" style="margin-left:26px">${desc}</p>
      ${extraHtml()}`;
    return card;
  },

  // ── Step 3: Telegram ───────────────────────────────────

  stepTelegram() {
    const s = this.state.telegram;
    const hasExisting = this.status.has_telegram;
    const card = el('div', { className: 'card' });

    card.innerHTML = `
      <div class="text-center mb-32">
        <div style="font-size:40px;margin-bottom:12px">\u{1F4F1}</div>
        <h2>\uD154\uB808\uADF8\uB7A8\uC744 \uC5F0\uACB0\uD560\uAE4C\uC694?</h2>
        <p class="note">\uC2A4\uD0AC \uC2E4\uD589 \uACB0\uACFC\uB97C \uD154\uB808\uADF8\uB7A8\uC73C\uB85C \uBC1B\uC544\uBCF4\uC138\uC694</p>
      </div>`;

    if (hasExisting && !s._showForm) {
      const existing = el('div', { className: 'flex flex-col gap-12' });
      existing.innerHTML = `
        <div class="info-box">
          <div style="font-weight:600;margin-bottom:4px">\uC774\uBBF8 \uC5F0\uACB0\uB41C \uD154\uB808\uADF8\uB7A8 \uBD07\uC774 \uC788\uC2B5\uB2C8\uB2E4</div>
          <div class="hint" style="font-family:monospace">Chat ID: ${esc(this.status.telegram_chat_id)}</div>
        </div>`;
      const useBtn = el('button', { className: 'btn btn--primary btn--block', onclick: () => this.go(4) });
      useBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uC774 \uC124\uC815 \uC0AC\uC6A9</div><div class="hint" style="color:var(--accent-dark);opacity:0.7">\uAE30\uC874 \uC5F0\uACB0\uC744 \uADF8\uB300\uB85C \uC720\uC9C0\uD569\uB2C8\uB2E4</div>';
      const newBtn = el('button', { className: 'btn btn--ghost btn--block', onclick: () => { s._showForm = true; this.render(); } });
      newBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uC0C8\uB85C \uC5F0\uACB0</div><div class="hint">\uB2E4\uB978 \uBD07\uC73C\uB85C \uB2E4\uC2DC \uC5F0\uACB0\uD569\uB2C8\uB2E4</div>';
      existing.appendChild(useBtn);
      existing.appendChild(newBtn);
      card.appendChild(existing);
      return card;
    }

    if (!s._showForm && !hasExisting) {
      const choices = el('div', { className: 'flex flex-col gap-12' });
      const yesBtn = el('button', { className: 'btn btn--ghost btn--block', onclick: () => { s._showForm = true; this.render(); } });
      yesBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uB124, \uC5F0\uACB0\uD560\uAC8C\uC694</div><div class="hint">BotFather\uC5D0\uC11C \uBD07\uC744 \uB9CC\uB4E4\uACE0 \uD1A0\uD070\uC744 \uC785\uB825\uD569\uB2C8\uB2E4</div>';
      const noBtn = el('button', { className: 'btn btn--ghost btn--block', style: 'color:var(--text-muted)', onclick: () => this.go(4) });
      noBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uB098\uC911\uC5D0 \uD560\uAC8C\uC694</div><div class="hint">\uC124\uC815\uC5D0\uC11C \uC5B8\uC81C\uB4E0 \uC5F0\uACB0\uD560 \uC218 \uC788\uC5B4\uC694</div>';
      choices.appendChild(yesBtn);
      choices.appendChild(noBtn);
      card.appendChild(choices);
      return card;
    }

    // Telegram form
    const form = el('div');
    form.innerHTML = `
      <div class="warn-box mb-20">
        <ol style="margin:0;padding-left:20px">
          <li>\uD154\uB808\uADF8\uB7A8\uC5D0\uC11C <strong>@BotFather</strong> \u2192 <strong>/newbot</strong></li>
          <li>\uBC1C\uAE09\uB41C \uD1A0\uD070\uC744 \uC544\uB798\uC5D0 \uBD99\uC5EC\uB123\uAE30</li>
        </ol>
      </div>
      <div class="flex flex-col gap-12 mb-20">
        <div>
          <label class="muted">\uBD07 \uD1A0\uD070</label>
          <input class="input input--mono" placeholder="1234567890:ABCdefGHIjklMNOpqrSTUvwxYZ" value="${s.botToken}" id="tg-token">
          <p class="hint mt-8" id="tg-hint" style="color:#2563eb;display:${s.botToken ? 'block' : 'none'}">
            \u{1F449} \uD154\uB808\uADF8\uB7A8\uC5D0\uC11C \uB9CC\uB4E0 \uBD07\uC5D0\uAC8C \uC544\uBB34 \uBA54\uC2DC\uC9C0\uB97C \uD558\uB098 \uBCF4\uB0B8 \uD6C4, \uC544\uB798 \uBC84\uD2BC\uC744 \uB20C\uB7EC\uC8FC\uC138\uC694
          </p>
        </div>
        <div>
          <label class="muted">\uCC44\uD305 ID</label>
          <div class="flex gap-8">
            <input class="input input--mono flex-1" placeholder="\uC790\uB3D9\uC73C\uB85C \uAC00\uC838\uC635\uB2C8\uB2E4" value="${s.chatId}" id="tg-chatid">
            <button class="btn btn--dark btn--sm nowrap" id="tg-fetch" style="min-width:120px"
              ${s.botToken ? '' : 'disabled'}>\uCC44\uD305 ID \uAC00\uC838\uC624\uAE30</button>
          </div>
        </div>
      </div>`;

    const actions = el('div', { className: 'flex justify-end gap-8' });
    const skipBtn = el('button', { className: 'btn btn--ghost btn--sm', onclick: () => this.go(4) }, '\uAC74\uB108\uB6F0\uAE30');
    const saveBtn = el('button', {
      className: 'btn btn--dark btn--sm',
      disabled: !s.botToken || !s.chatId,
      id: 'tg-save',
    }, '\uC800\uC7A5 \uD6C4 \uB2E4\uC74C');
    actions.appendChild(skipBtn);
    actions.appendChild(saveBtn);
    form.appendChild(actions);
    card.appendChild(form);

    setTimeout(() => {
      const tokenInput = document.getElementById('tg-token');
      const chatIdInput = document.getElementById('tg-chatid');
      const fetchBtn = document.getElementById('tg-fetch');
      const saveBtnEl = document.getElementById('tg-save');
      const hint = document.getElementById('tg-hint');

      if (tokenInput) tokenInput.oninput = e => {
        s.botToken = e.target.value;
        fetchBtn.disabled = !s.botToken;
        hint.style.display = s.botToken ? 'block' : 'none';
        saveBtnEl.disabled = !s.botToken || !s.chatId;
      };
      if (chatIdInput) chatIdInput.oninput = e => {
        s.chatId = e.target.value;
        saveBtnEl.disabled = !s.botToken || !s.chatId;
      };
      if (fetchBtn) fetchBtn.onclick = async () => {
        fetchBtn.disabled = true;
        fetchBtn.textContent = '\uAC00\uC838\uC624\uB294 \uC911...';
        const res = await apiPost('/api/setup/telegram/chat-id', { token: s.botToken });
        if (res.chat_id) {
          s.chatId = res.chat_id;
          chatIdInput.value = res.chat_id;
        } else {
          s.chatId = '';
          chatIdInput.value = '\uC624\uB958: ' + (res.error || 'unknown');
        }
        fetchBtn.disabled = false;
        fetchBtn.textContent = '\uCC44\uD305 ID \uAC00\uC838\uC624\uAE30';
        saveBtnEl.disabled = !s.botToken || !s.chatId;
      };
      if (saveBtnEl) saveBtnEl.onclick = async () => {
        saveBtnEl.disabled = true;
        saveBtnEl.textContent = '\uC800\uC7A5 \uC911...';
        await apiPost('/api/setup/telegram', { bot_token: s.botToken, chat_id: s.chatId });
        this.go(4);
      };
    }, 0);

    return card;
  },

  // ── Step 4: KakaoTalk ──────────────────────────────────

  stepKakaoTalk() {
    const s = this.state.kakao;
    const hasExisting = this.status.has_kakao;
    const card = el('div', { className: 'card' });

    card.innerHTML = `
      <div class="text-center mb-32">
        <div style="font-size:40px;margin-bottom:12px">\u{1F4AC}</div>
        <h2>\uCE74\uCE74\uC624\uD1A1\uC744 \uC5F0\uACB0\uD560\uAE4C\uC694?</h2>
        <p class="note">\uCE74\uCE74\uC624\uD1A1\uC73C\uB85C AI \uB2F5\uBCC0\uC744 \uBC1B\uC544\uBCF4\uC138\uC694</p>
      </div>`;

    if (s.pairCode) {
      const display = el('div', { className: 'text-center' });
      display.innerHTML = `
        <div style="background:var(--info-bg);border:2px solid var(--accent);border-radius:12px;padding:32px;margin-bottom:20px">
          <p style="font-size:13px;color:var(--accent-dark);font-weight:600;margin-bottom:12px">
            \uCE74\uCE74\uC624\uD1A1 \uCC44\uB110\uC5D0\uC11C \uC544\uB798 \uCF54\uB4DC\uB97C \uC785\uB825\uD558\uC138\uC694
          </p>
          <div class="pair-code">${esc(s.pairCode)}</div>
          <p class="hint mt-12">5\uBD84 \uC774\uB0B4\uC5D0 \uC785\uB825\uD574\uC8FC\uC138\uC694</p>
        </div>`;

      if (s.channelUrl) {
        const safeUrl = /^https?:\/\//.test(s.channelUrl) ? s.channelUrl : '#';
        const kakaoBox = el('div', { className: 'kakao-box mb-20' });

        const qrImg = el('img');
        qrImg.src = `https://api.qrserver.com/v1/create-qr-code/?size=100x100&data=${encodeURIComponent(safeUrl)}`;
        qrImg.alt = 'KakaoTalk channel QR';
        qrImg.width = 80;
        qrImg.height = 80;
        qrImg.style.borderRadius = '6px';
        qrImg.style.flexShrink = '0';
        kakaoBox.appendChild(qrImg);

        const textRight = el('div', { className: 'text-left' });
        const p = el('p', { style: 'font-size:13px;font-weight:600;margin-bottom:6px' }, 'QR \uC2A4\uCE94 \uB610\uB294 \uBC84\uD2BC\uC73C\uB85C \uCC44\uB110 \uC811\uC18D');
        textRight.appendChild(p);
        const link = el('a', { href: safeUrl, className: 'kakao-btn' }, '\uCE74\uCE74\uC624\uD1A1 \uCC44\uB110 \uC5F4\uAE30');
        textRight.appendChild(link);
        kakaoBox.appendChild(textRight);
        display.appendChild(kakaoBox);
      }

      const warnBox = el('div', { className: 'warn-box text-left mb-24' });
      warnBox.textContent = '\uCC44\uB110\uC5D0\uC11C \uC704 \uCF54\uB4DC\uB97C \uC785\uB825\uD558\uBA74 \uC5F0\uACB0\uB429\uB2C8\uB2E4. \uC544\uC9C1 \uC785\uB825\uD558\uC9C0 \uC54A\uC558\uB2E4\uBA74 \uB098\uC911\uC5D0 \uC124\uC815\uC5D0\uC11C \uB2E4\uC2DC \uC5F0\uACB0\uD560 \uC218 \uC788\uC5B4\uC694.';
      display.appendChild(warnBox);

      const statusLine = el('p', { className: 'hint mt-8', id: 'kakao-poll-status', style: 'text-align:center' });
      statusLine.textContent = '\uC5F0\uACB0 \uB300\uAE30 \uC911...';
      display.appendChild(statusLine);

      const nextBtn = el('div', { className: 'flex justify-end' });
      nextBtn.innerHTML = '<button class="btn btn--primary btn--sm" id="kakao-next">\uB2E4\uC74C</button>';
      display.appendChild(nextBtn);
      card.appendChild(display);
      setTimeout(() => {
        document.getElementById('kakao-next').onclick = () => {
          this._kakaoPollActive = false;
          this.go(5);
        };
        this._startPairPolling();
      }, 0);
      return card;
    }

    if (hasExisting) {
      const existing = el('div', { className: 'flex flex-col gap-12' });
      existing.innerHTML = `
        <div class="info-box">
          <div style="font-weight:600;margin-bottom:4px">\uC774\uBBF8 \uC5F0\uACB0\uB41C \uCE74\uCE74\uC624\uD1A1\uC774 \uC788\uC2B5\uB2C8\uB2E4</div>
          <div class="hint">\uAE30\uC874 \uD398\uC5B4\uB9C1\uC774 \uC720\uC9C0\uB429\uB2C8\uB2E4</div>
        </div>`;
      const useBtn = el('button', { className: 'btn btn--primary btn--block', onclick: () => this.go(5) });
      useBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uC774 \uC124\uC815 \uC0AC\uC6A9</div>';
      const newBtn = el('button', { className: 'btn btn--ghost btn--block', id: 'kakao-re' });
      newBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uC0C8\uB85C \uC5F0\uACB0</div><div class="hint">\uC0C8 \uD398\uC5B4\uB9C1 \uCF54\uB4DC\uB97C \uBC1C\uAE09\uBC1B\uC2B5\uB2C8\uB2E4</div>';
      existing.appendChild(useBtn);
      existing.appendChild(newBtn);
      card.appendChild(existing);
      setTimeout(() => {
        document.getElementById('kakao-re').onclick = () => this._kakaoRegister();
      }, 0);
    } else {
      const choices = el('div', { className: 'flex flex-col gap-12' });
      const yesBtn = el('button', { className: 'btn btn--ghost btn--block', id: 'kakao-yes' });
      yesBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uB124, \uC5F0\uACB0\uD560\uAC8C\uC694</div><div class="hint">\uCE74\uCE74\uC624\uD1A1 \uCC44\uB110\uC5D0\uC11C \uC778\uC99D \uCF54\uB4DC\uB97C \uC785\uB825\uD569\uB2C8\uB2E4</div>';
      const noBtn = el('button', { className: 'btn btn--ghost btn--block', style: 'color:var(--text-muted)', onclick: () => this.go(5) });
      noBtn.innerHTML = '<div style="font-weight:600;margin-bottom:4px">\uB098\uC911\uC5D0 \uD560\uAC8C\uC694</div><div class="hint">\uC124\uC815\uC5D0\uC11C \uC5B8\uC81C\uB4E0 \uC5F0\uACB0\uD560 \uC218 \uC788\uC5B4\uC694</div>';
      choices.appendChild(yesBtn);
      choices.appendChild(noBtn);
      card.appendChild(choices);
      setTimeout(() => {
        document.getElementById('kakao-yes').onclick = () => this._kakaoRegister();
      }, 0);
    }

    return card;
  },

  async _kakaoRegister() {
    const s = this.state.kakao;
    this.root.querySelector('.card').innerHTML = `
      <div class="text-center" style="padding:40px 0">
        <p class="note">\uC5F0\uACB0 \uC900\uBE44 \uC911...</p>
      </div>`;
    const res = await apiPost('/api/setup/kakao/register', {});
    if (res.pair_code) {
      s.pairCode = res.pair_code;
      s.channelUrl = res.channel_url || '';
      this.render();
    } else {
      const errCard = this.root.querySelector('.card');
      errCard.innerHTML = '';
      const errBox = el('div', { className: 'error-box' });
      errBox.textContent = res.error || '\uC5F0\uACB0 \uC2E4\uD328';
      const retryBtn = el('button', {
        className: 'btn btn--ghost btn--sm mt-12',
        onclick: () => this.render(),
      }, '\uB2E4\uC2DC \uC2DC\uB3C4');
      errCard.appendChild(errBox);
      errCard.appendChild(retryBtn);
    }
  },

  _startPairPolling() {
    this._kakaoPollActive = true;
    let polls = 0;
    const tick = async () => {
      if (!this._kakaoPollActive) return;
      if (++polls > 100) {
        const statusEl = document.getElementById('kakao-poll-status');
        if (statusEl) statusEl.textContent = '\uC2DC\uAC04 \uCD08\uACFC \u2014 \uB2E4\uC2DC \uC2DC\uB3C4\uD574\uC8FC\uC138\uC694';
        return;
      }
      try {
        const res = await apiRaw('/api/setup/kakao/pair-status');
        if (!this._kakaoPollActive) return;
        if (res.paired) {
          this._kakaoPollActive = false;
          const statusEl = document.getElementById('kakao-poll-status');
          if (statusEl) statusEl.textContent = '\u2705 \uC5F0\uACB0 \uC644\uB8CC!';
          setTimeout(() => this.go(5), 800);
          return;
        }
      } catch (_) { /* retry on network error */ }
      if (this._kakaoPollActive) setTimeout(tick, 3000);
    };
    setTimeout(tick, 3000);
  },

  // ── Step 5: Workspace ──────────────────────────────────

  stepWorkspace() {
    const s = this.state.workspace;
    const card = el('div', { className: 'card' });
    card.innerHTML = `
      <div class="text-center mb-32">
        <div style="font-size:40px;margin-bottom:12px">\u{1F4C1}</div>
        <h2>\uC791\uC5C5 \uD3F4\uB354\uB97C \uC120\uD0DD\uD574\uC8FC\uC138\uC694</h2>
        <p class="note">KittyPaw\uAC00 \uC811\uADFC\uD560 \uC218 \uC788\uB294 \uD3F4\uB354\uB97C \uC9C0\uC815\uD569\uB2C8\uB2E4</p>
      </div>
      <div class="flex flex-col gap-12">
        <div>
          <input class="input" type="text" placeholder="\uD3F4\uB354 \uACBD\uB85C\uB97C \uC785\uB825\uD558\uC138\uC694 (ex: /Users/me/workspace)" value="${s.path}" id="ws-path">
        </div>
        <p class="hint">\uC774 \uD3F4\uB354 \uC548\uC5D0\uC11C\uB9CC \uD30C\uC77C\uC744 \uC77D\uACE0 \uC4F8 \uC218 \uC788\uC2B5\uB2C8\uB2E4. \uC124\uC815\uC5D0\uC11C \uB098\uC911\uC5D0 \uBCC0\uACBD\uD560 \uC218 \uC788\uC5B4\uC694.</p>
      </div>
      <div class="flex justify-end gap-8 mt-24">
        <button class="btn btn--ghost btn--sm" id="ws-skip">\uAC74\uB108\uB6F0\uAE30</button>
        <button class="btn btn--primary btn--sm" id="ws-next" ${s.path ? '' : 'disabled'}>\uB2E4\uC74C</button>
      </div>`;

    setTimeout(() => {
      const input = document.getElementById('ws-path');
      const nextBtn = document.getElementById('ws-next');
      const skipBtn = document.getElementById('ws-skip');
      input.oninput = e => { s.path = e.target.value; nextBtn.disabled = !s.path; };
      skipBtn.onclick = () => this.go(6);
      nextBtn.onclick = async () => {
        nextBtn.disabled = true;
        await apiPost('/api/setup/workspace', { path: s.path });
        this.go(6);
      };
    }, 0);

    return card;
  },

  // ── Step 6: HTTP Access ────────────────────────────────

  stepHttpAccess() {
    const card = el('div', { className: 'card card--center' });
    card.innerHTML = `
      <h2 class="large">\uC6F9\uC5D0 \uC811\uC18D\uC744 \uD5C8\uC6A9\uD560\uAE4C\uC694?</h2>
      <p class="note mt-12" style="max-width:420px;margin-left:auto;margin-right:auto">
        \uC2A4\uD0AC\uC774 \uB0A0\uC528, \uB274\uC2A4, \uD658\uC728 \uB4F1 \uC678\uBD80 \uC815\uBCF4\uB97C \uAC00\uC838\uC624\uB824\uBA74 \uC778\uD130\uB137 \uC811\uC18D\uC774 \uD544\uC694\uD569\uB2C8\uB2E4.
      </p>
      <p class="hint mt-12">\uD5C8\uC6A9\uD558\uC9C0 \uC54A\uC73C\uBA74 Http/Web \uC2A4\uD0AC\uC740 \uC2A4\uCF00\uC904 \uC2E4\uD589 \uC2DC \uCC28\uB2E8\uB429\uB2C8\uB2E4.</p>
      <div class="flex justify-center gap-12 mt-40">
        <button class="btn btn--primary" id="http-yes">\uD5C8\uC6A9</button>
        <button class="btn btn--secondary" id="http-no">\uB098\uC911\uC5D0</button>
      </div>`;

    setTimeout(() => {
      document.getElementById('http-yes').onclick = async () => {
        await apiPost('/api/setup/http-access', {});
        this.go(7);
      };
      document.getElementById('http-no').onclick = () => this.go(7);
    }, 0);

    return card;
  },

  // ── Step 7: Complete ───────────────────────────────────

  stepComplete() {
    const card = el('div', { className: 'card card--center' });
    card.innerHTML = `
      <div style="font-size:48px;margin-bottom:20px">\u{2728}</div>
      <h2 style="font-size:32px">\uC900\uBE44 \uC644\uB8CC!</h2>
      <p class="note mt-12 mb-40">\uCC44\uD305\uC5D0\uC11C \uC790\uC720\uB86D\uAC8C \uB300\uD654\uD558\uAC70\uB098, \uC2A4\uD0AC\uC744 \uC124\uCE58\uD558\uACE0 \uC2E4\uD589\uD574\uBCF4\uC138\uC694</p>
      <button class="btn btn--primary" id="complete-btn">\uC2DC\uC791\uD558\uAE30</button>`;

    setTimeout(() => {
      document.getElementById('complete-btn').onclick = async () => {
        const btn = document.getElementById('complete-btn');
        btn.disabled = true;
        btn.textContent = '\uC124\uC815 \uC644\uB8CC \uC911...';
        await apiPost('/api/setup/complete', {});
        await App.bootstrap();
        App.showShell();
      };
    }, 0);

    return card;
  },
};

// ── DOM helper ───────────────────────────────────────────

function el(tag, attrs, text) {
  const e = document.createElement(tag);
  if (attrs) Object.assign(e, attrs);
  if (text) e.textContent = text;
  return e;
}
