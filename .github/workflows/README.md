# Workflows

Active workflows live only in this root `.github/workflows/` directory. Workflow
files preserved under imported app subdirectories are historical and are not
executed by GitHub from the monorepo.

Current release workflows:

- `release-kittypaw.yml` for `kittypaw/v*`

Hosted service binaries are currently deployed from local build/deploy scripts,
not from product release workflows. Add future release workflows here only when
the corresponding `.yml` file exists in this directory.

Do not trigger product releases from a plain `v*` tag.
