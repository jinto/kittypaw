# KittyChat Deployment

## Initial Setup

```bash
DEPLOY_DOMAIN=chat.kittypaw.app fab setup
```

The setup task creates `/home/jinto/kittychat/.env` with generated MVP tokens if
one does not already exist.

For production auth, set `KITTYCHAT_JWT_SECRET` in `/home/jinto/kittychat/.env`
to the same value as kittyapi's `JWT_SECRET`. That enables verification of
API-issued access tokens and daemon device credentials with
`iss="https://api.kittypaw.app/auth"`, `aud` containing
`https://chat.kittypaw.app`, `scope`, and `v=1`. During migration, legacy
`iss="kittyapi"` and `aud` containing `kittychat` are also accepted. Static
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
