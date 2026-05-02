import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

function loadManualHelpers() {
  const source = fs.readFileSync(new URL("../manual/app.js", import.meta.url), "utf8");
  const elements = new Map();
  const makeElement = () => ({
    value: "",
    textContent: "",
    disabled: false,
    className: "",
    classList: { toggle() {} },
    appendChild() {},
    addEventListener() {},
  });
  const context = {
    console,
    clearTimeout() {},
    setTimeout() { return 1; },
    localStorage: {
      getItem() { return null; },
      setItem() {},
      removeItem() {},
    },
    document: {
      createElement() {
        return makeElement();
      },
      getElementById(id) {
        if (!elements.has(id)) {
          elements.set(id, makeElement());
        }
        return elements.get(id);
      },
    },
  };
  context.window = context;
  vm.runInNewContext(source, context);
  return context.__kittychatManual;
}

test("formatHTTPError reports HTTP status for HTML gateway bodies", () => {
  const helpers = loadManualHelpers();

  const message = helpers.formatHTTPError(
    { status: 502, statusText: "Bad Gateway" },
    { error: "<!DOCTYPE html><html><body><div id=\"cf-wrapper\">gateway</div></body></html>" },
    "<!DOCTYPE html><html><body><div id=\"cf-wrapper\">gateway</div></body></html>",
  );

  assert.equal(message, "HTTP 502 Bad Gateway");
});

test("formatHTTPError combines status with concise JSON error messages", () => {
  const helpers = loadManualHelpers();

  const message = helpers.formatHTTPError(
    { status: 502, statusText: "Bad Gateway" },
    { error: "device connection replaced" },
    "{\"error\":\"device connection replaced\"}",
  );

  assert.equal(message, "HTTP 502 Bad Gateway: device connection replaced");
});
