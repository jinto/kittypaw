# Manual Chat UI Design

Date: 2026-05-02

## Goal

Provide a tiny frontend-only manual QA surface for the deployed KittyChat OpenAI-compatible relay. It should let an operator open a browser, paste a KittyChat API token, select the current device/account route, and send chat completion requests through the same production path already verified by curl.

## Shape

- Serve static files from `kittychat` under `/manual/` so the browser uses the same origin as `https://chat.kittypaw.app`.
- Do not inject secrets server-side. The operator pastes a Bearer token in the browser.
- Store editable settings in browser local storage only: API token, device ID, account ID, model, and messages.
- Use the existing OpenAI-compatible endpoints:
  - `GET /v1/routes`
  - `GET /nodes/{device_id}/accounts/{account_id}/v1/models`
  - `POST /nodes/{device_id}/accounts/{account_id}/v1/chat/completions`

## UX

The page is a compact test console, not a product shell. It has:

- Connection fields for token, device, account, and model.
- A route loader that fills device/account from `/v1/routes`.
- A model loader that fills model choices from `/v1/models`.
- A chat transcript and message composer.
- Clear error and status states without exposing token values.

## Security

- `/manual/` is static and contains no embedded API token.
- The token remains in the operator's browser local storage if they choose to keep the page state.
- This is acceptable for manual QA, but not a public user product surface.

## Testing

- Router test: `/manual/` returns HTML even when the OpenAI handler is mounted at `/`.
- Router test: `/manual/app.js` returns JavaScript.
- Build/test/lint remain clean before deploy.
