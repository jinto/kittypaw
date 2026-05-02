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

test("shared helpers do not expose browser token storage APIs", () => {
  const helpers = loadHelpers();

  assert.equal(helpers.parseTokenParams, undefined);
  assert.equal(helpers.saveAuth, undefined);
  assert.equal(helpers.loadAuth, undefined);
  assert.equal(helpers.clearAuth, undefined);
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
