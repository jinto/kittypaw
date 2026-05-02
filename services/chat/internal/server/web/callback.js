(function () {
  const web = window.KittyChatWeb;
  const status = document.getElementById("callbackStatus");
  const fromHash = web.parseTokenParams(window.location.hash);
  const tokenPayload = fromHash.accessToken ? fromHash : web.parseTokenParams(window.location.search);

  if (!tokenPayload.accessToken) {
    if (status) {
      status.textContent = "Login callback did not include an access token.";
      status.classList.add("error");
    }
    return;
  }

  web.saveAuth(window.localStorage, tokenPayload);
  window.history.replaceState(null, "", "/auth/callback");
  window.location.replace("/app/");
})();
