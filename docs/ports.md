# Ports

This file is the local development port registry. Production container ports may
use different conventions.

| Unit | Local Port | Purpose |
| --- | ---: | --- |
| apps/kittypaw | 0 | Local daemon chooses or configures its own port |
| services/api | 9712 | Cloud API service |
| services/chat | 9713 | Hosted chat and relay service |
| services/kakao | 8787 | Kakao gateway service |

Keep this file updated before adding local docker-compose or dev-server scripts.
