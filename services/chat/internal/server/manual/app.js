const storageKey = "kittychat-manual-state-v1";

const state = {
  token: "",
  routes: [],
  deviceID: "",
  accountID: "",
  model: "main",
  messages: [],
};

const els = {
  token: document.getElementById("apiTokenInput"),
  device: document.getElementById("deviceSelect"),
  account: document.getElementById("accountSelect"),
  model: document.getElementById("modelSelect"),
  messages: document.getElementById("messages"),
  input: document.getElementById("messageInput"),
  send: document.getElementById("sendButton"),
  status: document.getElementById("statusText"),
  composer: document.getElementById("composer"),
  loadRoutes: document.getElementById("loadRoutesButton"),
  loadModels: document.getElementById("loadModelsButton"),
  clear: document.getElementById("clearButton"),
};

function loadState() {
  try {
    const saved = JSON.parse(localStorage.getItem(storageKey) || "{}");
    Object.assign(state, {
      token: saved.token || "",
      deviceID: saved.deviceID || "",
      accountID: saved.accountID || "",
      model: saved.model || "main",
      messages: Array.isArray(saved.messages) ? saved.messages : [],
    });
  } catch {
    localStorage.removeItem(storageKey);
  }
}

function saveState() {
  localStorage.setItem(storageKey, JSON.stringify({
    token: state.token,
    deviceID: state.deviceID,
    accountID: state.accountID,
    model: state.model,
    messages: state.messages,
  }));
}

function setStatus(text, error = false) {
  els.status.textContent = text;
  els.status.classList.toggle("error", error);
}

function authHeaders() {
  const token = state.token.trim();
  return token ? { Authorization: `Bearer ${token}` } : {};
}

async function requestJSON(path, options = {}) {
  const resp = await fetch(path, {
    ...options,
    headers: {
      ...authHeaders(),
      ...(options.headers || {}),
    },
  });
  const text = await resp.text();
  let body = null;
  if (text) {
    try {
      body = JSON.parse(text);
    } catch {
      body = { error: text };
    }
  }
  if (!resp.ok) {
    const msg = body && body.error ? body.error : `${resp.status} ${resp.statusText}`;
    throw new Error(msg);
  }
  return body;
}

function routePath(suffix) {
  if (!state.deviceID || !state.accountID) {
    throw new Error("route is required");
  }
  return `/nodes/${encodeURIComponent(state.deviceID)}/accounts/${encodeURIComponent(state.accountID)}${suffix}`;
}

function renderMessages() {
  els.messages.textContent = "";
  if (state.messages.length === 0) {
    const empty = document.createElement("div");
    empty.className = "empty";
    empty.textContent = "No messages";
    els.messages.appendChild(empty);
    return;
  }
  for (const message of state.messages) {
    const node = document.createElement("article");
    node.className = `message ${message.role}`;
    node.textContent = message.content;
    els.messages.appendChild(node);
  }
  els.messages.scrollTop = els.messages.scrollHeight;
}

function renderSelect(select, values, selected) {
  select.textContent = "";
  for (const value of values) {
    const option = document.createElement("option");
    option.value = value;
    option.textContent = value;
    if (value === selected) {
      option.selected = true;
    }
    select.appendChild(option);
  }
}

function renderRoutes() {
  const deviceIDs = [...new Set(state.routes.map((r) => r.device_id).filter(Boolean))];
  if (state.deviceID && !deviceIDs.includes(state.deviceID)) {
    deviceIDs.unshift(state.deviceID);
  }
  renderSelect(els.device, deviceIDs, state.deviceID);
  renderAccounts();
}

function renderAccounts() {
  const route = state.routes.find((r) => r.device_id === state.deviceID);
  const accounts = route && Array.isArray(route.local_accounts) ? [...route.local_accounts] : [];
  if (accounts.length > 0 && !accounts.includes(state.accountID)) {
    state.accountID = accounts[0];
  }
  renderSelect(els.account, accounts, state.accountID);
}

function renderModels(models = []) {
  const ids = models.map((m) => m.id).filter(Boolean);
  if (state.model && !ids.includes(state.model)) {
    ids.unshift(state.model);
  }
  renderSelect(els.model, ids, state.model);
}

async function loadRoutes() {
  setBusy(true);
  try {
    setStatus("Loading routes");
    const body = await requestJSON("/v1/routes");
    state.routes = Array.isArray(body.data) ? body.data : [];
    if (state.routes.length > 0) {
      const first = state.routes[0];
      state.deviceID = state.deviceID || first.device_id || "";
      const accounts = Array.isArray(first.local_accounts) ? first.local_accounts : [];
      state.accountID = state.accountID || accounts[0] || "";
    }
    renderRoutes();
    saveState();
    setStatus(state.routes.length ? "Routes loaded" : "No routes");
  } catch (err) {
    setStatus(err.message, true);
  } finally {
    setBusy(false);
  }
}

async function loadModels() {
  setBusy(true);
  try {
    setStatus("Loading models");
    const body = await requestJSON(routePath("/v1/models"));
    renderModels(Array.isArray(body.data) ? body.data : []);
    saveState();
    setStatus("Models loaded");
  } catch (err) {
    setStatus(err.message, true);
  } finally {
    setBusy(false);
  }
}

async function sendMessage(event) {
  event.preventDefault();
  const text = els.input.value.trim();
  if (!text) {
    return;
  }
  state.messages.push({ role: "user", content: text });
  els.input.value = "";
  renderMessages();
  saveState();

  setBusy(true);
  try {
    setStatus("Sending");
    const body = await requestJSON(routePath("/v1/chat/completions"), {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({
        model: state.model || "main",
        messages: state.messages.map(({ role, content }) => ({ role, content })),
      }),
    });
    const content = body && body.choices && body.choices[0] && body.choices[0].message
      ? body.choices[0].message.content || ""
      : "";
    state.messages.push({ role: "assistant", content: content || "(empty)" });
    renderMessages();
    saveState();
    setStatus("Ready");
  } catch (err) {
    state.messages.push({ role: "system", content: err.message });
    renderMessages();
    saveState();
    setStatus(err.message, true);
  } finally {
    setBusy(false);
  }
}

function setBusy(busy) {
  els.send.disabled = busy;
  els.loadRoutes.disabled = busy;
  els.loadModels.disabled = busy;
}

function clearChat() {
  state.messages = [];
  renderMessages();
  saveState();
  setStatus("Cleared");
}

function syncFromInputs() {
  state.token = els.token.value;
  state.deviceID = els.device.value;
  state.accountID = els.account.value;
  state.model = els.model.value || "main";
  saveState();
}

function init() {
  loadState();
  els.token.value = state.token;
  renderRoutes();
  renderModels([]);
  renderMessages();

  els.token.addEventListener("input", syncFromInputs);
  els.device.addEventListener("change", () => {
    state.deviceID = els.device.value;
    renderAccounts();
    state.accountID = els.account.value;
    saveState();
  });
  els.account.addEventListener("change", syncFromInputs);
  els.model.addEventListener("change", syncFromInputs);
  els.loadRoutes.addEventListener("click", loadRoutes);
  els.loadModels.addEventListener("click", loadModels);
  els.clear.addEventListener("click", clearChat);
  els.composer.addEventListener("submit", sendMessage);
}

init();
