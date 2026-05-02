# KittyChat Deployment

## Initial Setup

```bash
DEPLOY_DOMAIN=chat.kittypaw.app fab setup
```

The setup task creates `/home/jinto/kittychat/.env` with generated MVP tokens if
one does not already exist.

For production auth, set `KITTYCHAT_JWKS_URL` in `/home/jinto/kittychat/.env`
to the portal JWKS endpoint:

```env
KITTYCHAT_JWKS_URL=https://portal.kittypaw.app/.well-known/jwks.json
```

That enables verification of RS256 API-issued access tokens and daemon device
credentials with `iss="https://portal.kittypaw.app/auth"`, `aud` containing
`https://chat.kittypaw.app`, `scope`, and `v=2`. Static
`KITTYCHAT_API_TOKEN`/`KITTYCHAT_DEVICE_TOKEN` values remain only as MVP
fallbacks while issuance and pairing flows are being rolled out.

## Deploy

```bash
DEPLOY_DOMAIN=chat.kittypaw.app fab deploy
fab status
fab logs
```

## Verify

```bash
curl https://chat.kittypaw.app/health
```
