import assert from "node:assert/strict";
import fs from "node:fs";
import test from "node:test";
import vm from "node:vm";

function makeElement() {
  return {
    value: "",
    textContent: "",
    disabled: false,
    className: "",
    classList: {
      add() {},
      toggle() {},
    },
    appendChild() {},
    addEventListener() {},
  };
}

function memoryStorage(initial = {}) {
  const values = new Map(Object.entries(initial));
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

async function loadAppWithFetch(fetchImpl) {
  const shared = fs.readFileSync(new URL("../web/shared.js", import.meta.url), "utf8");
  const app = fs.readFileSync(new URL("../web/app.js", import.meta.url), "utf8");
  const elements = new Map();
  const replacements = [];
  const storage = memoryStorage();

  const context = {
    console,
    Date,
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
    window: {
      localStorage: storage,
      fetch: fetchImpl,
      location: {
        replace(path) {
          replacements.push(path);
        },
      },
    },
  };
  context.window.window = context.window;
  context.window.document = context.document;
  context.window.Date = Date;
  context.globalThis = context.window;

  vm.runInNewContext(shared, context);
  vm.runInNewContext(app, context);
  await new Promise((resolve) => setImmediate(resolve));

  return { replacements, storage };
}

test("app uses BFF routes without browser bearer token", async () => {
  const paths = [];
  const authHeaders = [];
  const { replacements, storage } = await loadAppWithFetch(async (path, options = {}) => {
    paths.push(path);
    authHeaders.push(options.headers && options.headers.Authorization);
    return {
      ok: true,
      status: 200,
      statusText: "OK",
      text: async () => "{\"object\":\"list\",\"data\":[]}",
    };
  });

  assert.deepEqual(paths, ["/app/api/routes"]);
  assert.deepEqual(authHeaders, [undefined]);
  assert.equal(storage.getItem("kittychat-auth-v1"), null);
  assert.deepEqual(replacements, []);
});

test("app redirects to entry when BFF session is rejected", async () => {
  const { replacements, storage } = await loadAppWithFetch(async () => ({
    ok: false,
    status: 401,
    statusText: "Unauthorized",
    text: async () => "{\"error\":\"unauthorized\"}",
  }));

  assert.equal(storage.getItem("kittychat-auth-v1"), null);
  assert.deepEqual(replacements, ["/"]);
});
