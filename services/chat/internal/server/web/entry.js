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
    link.href = loginURL.toString();
  }
})();
