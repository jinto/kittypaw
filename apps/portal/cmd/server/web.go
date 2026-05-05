package main

import (
	"fmt"
	"html"
	"net/http"
	"strings"

	"github.com/kittypaw-app/kittyportal/internal/auth"
	"github.com/kittypaw-app/kittyportal/internal/config"
)

func handlePortalHome(cfg *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		chatURL := strings.TrimSpace(cfg.ChatRelayURL)
		if chatURL == "" {
			chatURL = "https://chat.kittypaw.app"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; style-src 'unsafe-inline'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>KittyPaw Portal</title>
  <style>%s</style>
</head>
<body>
  <main id="portal-home" class="portal-shell">
    <section class="portal-card">
      <p class="eyebrow">KittyPaw Portal</p>
      <h1>Identity and pairing services for KittyPaw</h1>
      <p class="lede">This portal handles browser sign-in, daemon pairing, and short-lived credentials for KittyPaw apps.</p>
      <div class="actions">
        <a class="primary-action" href="%s">Open KittyChat</a>
        <a class="secondary-action" href="/discovery">View discovery</a>
      </div>
      <p class="fine-print">Setup windows opened from the CLI will return to the terminal after authentication completes.</p>
    </section>
  </main>
</body>
</html>`, auth.PortalPageCSS(), html.EscapeString(chatURL))
	}
}

func handleHostRoot(cfg *config.Config) http.HandlerFunc {
	portalHome := handlePortalHome(cfg)
	connectHome := handleConnectHome(cfg)
	connectHost := canonicalURLHost(cfg.ConnectBaseURL)
	portalHost := canonicalURLHost(cfg.BaseURL)

	return func(w http.ResponseWriter, r *http.Request) {
		requestHost := canonicalHost(r.Host)
		if connectHost != "" && requestHost == connectHost {
			connectHome(w, r)
			return
		}
		if requestHost == portalHost || isLocalRequestHost(requestHost) || isLocalRequestHost(portalHost) {
			portalHome(w, r)
			return
		}
		http.NotFound(w, r)
	}
}

func handleConnectHome(_ *config.Config) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'; style-src 'unsafe-inline'")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = fmt.Fprintf(w, `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>KittyPaw Connect</title>
  <style>%s</style>
</head>
<body>
  <main id="connect-home" class="portal-shell">
    <section class="portal-card">
      <p class="eyebrow">KittyPaw Connect</p>
      <h1>Connect external accounts to local KittyPaw</h1>
      <p class="lede">This surface handles account connection flows for services such as Gmail while keeping runtime tokens in your local KittyPaw account.</p>
      <p class="fine-print">Setup windows opened from the CLI will return to the terminal after authorization completes.</p>
    </section>
  </main>
</body>
</html>`, auth.PortalPageCSS())
	}
}
