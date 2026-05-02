(function () {
  const web = window.KittyChatWeb;
  const auth = web.loadAuth(window.localStorage);
  if (auth) {
    window.location.replace("/app/");
    return;
  }

  const callbackURL = `${window.location.origin}/auth/callback`;
  const loginURL = new URL("https://api.kittypaw.app/auth/web/google");
  loginURL.searchParams.set("redirect_uri", callbackURL);

  const link = document.getElementById("googleLoginLink");
  if (link) {
    link.dataset.pendingHref = loginURL.toString();
    link.addEventListener("click", () => {
      const status = document.getElementById("entryStatus");
      if (status) {
        status.textContent = "API web login endpoint is not live yet. Use Manual QA or open /auth/callback with a token fragment for integration testing.";
        status.classList.add("error");
      }
    });
  }
})();
