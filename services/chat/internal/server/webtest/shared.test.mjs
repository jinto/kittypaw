import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

function loadHelpers() {
  const source = fs.readFileSync(new URL("../web/shared.js", import.meta.url), "utf8");
  const context = { URLSearchParams };
  context.globalThis = context;
  vm.runInNewContext(source, context);
  return context.KittyChatWeb;
}

function memoryStorage() {
  const values = new Map();
  return {
    getItem(key) {
      return values.has(key) ? values.get(key) : null;
    },
    setItem(key, value) {
      values.set(key, value);
    },
    removeItem(key) {
      values.delete(key);
    },
  };
}

test("parseTokenParams reads callback fragment tokens", () => {
  const helpers = loadHelpers();

  const tokens = helpers.parseTokenParams("#access_token=a&refresh_token=r&token_type=Bearer&expires_in=900");

  assert.equal(tokens.accessToken, "a");
  assert.equal(tokens.refreshToken, "r");
  assert.equal(tokens.tokenType, "Bearer");
  assert.equal(tokens.expiresIn, 900);
});

test("formatHTTPError combines status with JSON error message", () => {
  const helpers = loadHelpers();

  const message = helpers.formatHTTPError(
    { status: 503, statusText: "Service Unavailable" },
    { error: "device offline" },
    "{\"error\":\"device offline\"}",
  );

  assert.equal(message, "HTTP 503 Service Unavailable: device offline");
});

test("formatHTTPError reports only status for HTML bodies", () => {
  const helpers = loadHelpers();

  const message = helpers.formatHTTPError(
    { status: 502, statusText: "Bad Gateway" },
    null,
    "<!DOCTYPE html><html><body>proxy error</body></html>",
  );

  assert.equal(message, "HTTP 502 Bad Gateway");
});

test("selectFirstAvailableRoute replaces stale device and account", () => {
  const helpers = loadHelpers();

  const selected = helpers.selectFirstAvailableRoute(
    { deviceID: "old", accountID: "ghost" },
    [{ device_id: "dev", local_accounts: ["jinto"] }],
  );

  assert.equal(JSON.stringify(selected), JSON.stringify({ deviceID: "dev", accountID: "jinto" }));
});

test("loadAuth rejects expired tokens", () => {
  const helpers = loadHelpers();
  const storage = memoryStorage();
  helpers.saveAuth(storage, {
    accessToken: "token",
    refreshToken: "refresh",
    tokenType: "Bearer",
    expiresIn: 10,
  }, 1000);

  assert.equal(helpers.loadAuth(storage, 2000), null);
});
