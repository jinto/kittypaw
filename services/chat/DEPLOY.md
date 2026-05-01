# KittyChat Deployment

## Initial Setup

```bash
DEPLOY_DOMAIN=chat.kittypaw.app fab setup
```

The setup task creates `/home/jinto/kittychat/.env` with generated MVP tokens if
one does not already exist.

For production auth, set `KITTYCHAT_JWT_SECRET` in `/home/jinto/kittychat/.env`
to the same value as kittyapi's `JWT_SECRET`. That enables verification of
API-issued access tokens with `iss="kittyapi"`, `aud` containing `kittychat`,
`scope`, and `v=1`. The static `KITTYCHAT_API_TOKEN` remains only as an MVP
fallback when `KITTYCHAT_JWT_SECRET` is unset.

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
