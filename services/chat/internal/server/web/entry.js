(function () {
  async function init() {
    try {
      const resp = await window.fetch("/app/api/session", { headers: { Accept: "application/json" } });
      if (resp.ok) {
        window.location.replace("/app/");
      }
    } catch {
      // Entry still works when the session probe fails; the login link
      // starts a fresh PKCE flow.
    }
  }

  init();
})();
