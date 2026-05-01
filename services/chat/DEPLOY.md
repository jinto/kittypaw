# KittyChat Deployment

## Initial Setup

```bash
DEPLOY_DOMAIN=chat.kittypaw.app fab setup
```

The setup task creates `/home/jinto/kittychat/.env` with generated MVP tokens if
one does not already exist.

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
