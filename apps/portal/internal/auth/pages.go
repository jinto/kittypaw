package auth

import (
	"fmt"
	"html"
)

const portalPageCSS = `
:root {
  color-scheme: light;
  --bg: #f3f6f8;
  --panel: #ffffff;
  --ink: #17212f;
  --muted: #5f6d7d;
  --line: #d8e0e8;
  --accent: #0f766e;
  --accent-dark: #115e59;
  --shadow: 0 18px 50px rgba(21, 32, 43, 0.08);
  font-family: Inter, ui-sans-serif, system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif;
}
* { box-sizing: border-box; }
body {
  min-height: 100vh;
  margin: 0;
  background: linear-gradient(180deg, #f8fafc 0, var(--bg) 360px), var(--bg);
  color: var(--ink);
}
.portal-shell {
  width: min(100vw - 32px, 960px);
  min-height: 100vh;
  margin: 0 auto;
  padding: 32px 0;
  display: flex;
  align-items: center;
  justify-content: center;
}
.portal-card {
  width: 100%;
  max-width: 700px;
  padding: 44px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--panel);
  box-shadow: var(--shadow);
}
.eyebrow {
  margin: 0 0 8px;
  color: var(--accent-dark);
  font-size: 13px;
  font-weight: 700;
  text-transform: uppercase;
}
h1 {
  margin: 0;
  font-size: 32px;
  line-height: 1.15;
}
.lede {
  max-width: 590px;
  margin: 16px 0 0;
  color: var(--muted);
  font-size: 17px;
  line-height: 1.55;
}
.actions {
  display: flex;
  flex-wrap: wrap;
  gap: 12px;
  margin-top: 24px;
}
.primary-action,
.secondary-action {
  min-height: 44px;
  border-radius: 8px;
  display: inline-flex;
  align-items: center;
  justify-content: center;
  padding: 0 18px;
  font-weight: 700;
  text-decoration: none;
}
.primary-action {
  background: var(--accent);
  color: #fff;
  box-shadow: 0 8px 20px rgba(15, 118, 110, 0.18);
}
.secondary-action {
  background: #e8eef5;
  color: var(--ink);
}
.fine-print {
  margin: 16px 0 0;
  color: var(--muted);
  font-size: 13px;
  line-height: 1.5;
}
.code {
  width: fit-content;
  margin: 24px 0 0;
  padding: 14px 18px;
  border: 1px solid #c7d2dc;
  border-radius: 8px;
  background: #f8fafc;
  color: var(--ink);
  font-family: "SFMono-Regular", Consolas, "Liberation Mono", monospace;
  font-size: clamp(32px, 8vw, 48px);
  font-weight: 800;
  letter-spacing: 0;
}
@media (max-width: 760px) {
  .portal-shell {
    width: min(100vw - 20px, 960px);
    padding: 16px 0;
  }
  .portal-card {
    padding: 24px;
  }
  h1 {
    font-size: 26px;
  }
}
`

// PortalPageCSS returns the shared minimal browser UI style for portal pages.
func PortalPageCSS() string {
	return portalPageCSS
}

// CLIAuthCodePageHTML renders the one-time code page shown by setup/login
// flows that cannot receive credentials through a localhost callback.
func CLIAuthCodePageHTML(displayCode string) string {
	return fmt.Sprintf(`<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>KittyPaw Portal</title>
  <style>%s</style>
</head>
<body>
  <main id="portal-cli-code" class="portal-shell">
    <section class="portal-card">
      <p class="eyebrow">KittyPaw Portal</p>
      <h1>Enter this code in your terminal</h1>
      <p class="lede">Keep this browser window open until setup confirms the account connection.</p>
      <div class="code">%s</div>
      <p class="fine-print">This code expires in 5 minutes.</p>
    </section>
  </main>
</body>
</html>`, portalPageCSS, html.EscapeString(displayCode))
}
