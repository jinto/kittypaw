# Workflows

Future GitHub Actions workflows should be product-scoped.

Expected release workflows:

- `release-kittypaw.yml` for `kittypaw/v*`
- `release-api.yml` for `api/v*`
- `release-chat.yml` for `chat/v*`
- `release-kakao.yml` for `kakao/v*`

Do not trigger product releases from a plain `v*` tag.
