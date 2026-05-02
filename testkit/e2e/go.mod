module github.com/kittypaw-app/kitty/testkit/e2e

go 1.25.0

require (
	github.com/golang-migrate/migrate/v4 v4.19.1
	github.com/jinto/kittypaw v0.0.0
)

require (
	github.com/jackc/pgerrcode v0.0.0-20220416144525-469b46aa5efa // indirect
	github.com/jackc/pgpassfile v1.0.0 // indirect
	github.com/jackc/pgservicefile v0.0.0-20221227161230-091c0ba34f0a // indirect
	github.com/jackc/pgx/v5 v5.5.4 // indirect
	github.com/jackc/puddle/v2 v2.2.1 // indirect
	golang.org/x/crypto v0.50.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/text v0.36.0 // indirect
	nhooyr.io/websocket v1.8.17 // indirect
)

replace github.com/jinto/kittypaw => ../../apps/kittypaw
