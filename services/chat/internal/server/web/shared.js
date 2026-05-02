(function () {
  const authStorageKey = "kittychat-auth-v1";

  function parseTokenParams(searchOrHash) {
    const raw = (searchOrHash || "").replace(/^[?#]/, "");
    const params = new URLSearchParams(raw);
    const expiresIn = Number.parseInt(params.get("expires_in") || "0", 10);
    return {
      accessToken: params.get("access_token") || "",
      refreshToken: params.get("refresh_token") || "",
      tokenType: params.get("token_type") || "Bearer",
      expiresIn: Number.isFinite(expiresIn) ? expiresIn : 0,
    };
  }

  function errorStatus(resp) {
    const label = resp.statusText ? ` ${resp.statusText}` : "";
    return `HTTP ${resp.status}${label}`;
  }

  function looksLikeHTML(text) {
    return /^\s*(<!doctype\s+html|<html[\s>]|<head[\s>]|<body[\s>])/i.test(text);
  }

  function compactErrorText(text) {
    return String(text || "").replace(/\s+/g, " ").trim().slice(0, 220);
  }

  function errorMessageFromJSON(body) {
    if (!body || typeof body !== "object") {
      return "";
    }
    if (typeof body.error === "string") {
      return body.error;
    }
    if (body.error && typeof body.error.message === "string") {
      return body.error.message;
    }
    if (typeof body.message === "string") {
      return body.message;
    }
    return "";
  }

  function formatHTTPError(resp, body, rawText) {
    const status = errorStatus(resp);
    const jsonMessage = compactErrorText(errorMessageFromJSON(body));
    if (jsonMessage && !looksLikeHTML(jsonMessage)) {
      return `${status}: ${jsonMessage}`;
    }
    const textMessage = compactErrorText(rawText || "");
    if (textMessage && !looksLikeHTML(textMessage)) {
      return `${status}: ${textMessage}`;
    }
    return status;
  }

  function selectFirstAvailableRoute(current, routes) {
    const list = Array.isArray(routes) ? routes : [];
    if (list.length === 0) {
      return { deviceID: "", accountID: "" };
    }
    let route = list.find((r) => r.device_id === current.deviceID);
    if (!route) {
      route = list[0];
    }
    const accounts = Array.isArray(route.local_accounts) ? route.local_accounts : [];
    const accountID = accounts.includes(current.accountID) ? current.accountID : accounts[0] || "";
    return { deviceID: route.device_id || "", accountID };
  }

  function saveAuth(storage, tokenPayload, now = Date.now()) {
    const expiresAt = tokenPayload.expiresIn > 0 ? now + tokenPayload.expiresIn * 1000 : 0;
    storage.setItem(authStorageKey, JSON.stringify({
      accessToken: tokenPayload.accessToken,
      refreshToken: tokenPayload.refreshToken,
      tokenType: tokenPayload.tokenType || "Bearer",
      expiresAt,
    }));
  }

  function loadAuth(storage, now = Date.now()) {
    try {
      const auth = JSON.parse(storage.getItem(authStorageKey) || "{}");
      if (!auth.accessToken) {
        return null;
      }
      if (auth.expiresAt && auth.expiresAt <= now + 30000) {
        return null;
      }
      return auth;
    } catch {
      return null;
    }
  }

  function clearAuth(storage) {
    storage.removeItem(authStorageKey);
  }

  const api = {
    authStorageKey,
    parseTokenParams,
    formatHTTPError,
    selectFirstAvailableRoute,
    saveAuth,
    loadAuth,
    clearAuth,
  };

  if (typeof window !== "undefined") {
    window.KittyChatWeb = api;
  }
  if (typeof globalThis !== "undefined") {
    globalThis.KittyChatWeb = api;
  }
})();
