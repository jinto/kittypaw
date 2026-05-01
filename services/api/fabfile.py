"""
KittyAPI deployment — fab deploy / fab setup / fab logs / fab status / fab rollback
"""
import os
import subprocess
import sys
from pathlib import Path

from fabric import task

HOST = os.environ.get("DEPLOY_HOST", "second")
DOMAIN = os.environ.get("DEPLOY_DOMAIN", "")
REMOTE_DIR = "/home/jinto/kittyapi"
SERVICE = "kittyapi"
BINARY = "kittypaw-api"

LOCAL_ROOT = Path(__file__).resolve().parent


def _conn():
    from fabric import Connection
    return Connection(HOST)


def _local_build():
    """Cross-compile for Linux x86_64 (static binary, no CGO)."""
    print(f"Building {BINARY} for linux/amd64 ...")
    env = {**os.environ, "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"}
    result = subprocess.run(
        ["go", "build", "-o", f"{BINARY}-linux", "./cmd/server"],
        cwd=LOCAL_ROOT,
        env=env,
    )
    if result.returncode != 0:
        print("Build failed.")
        sys.exit(1)
    return LOCAL_ROOT / f"{BINARY}-linux"


@task
def setup(ctx):
    """Initial server setup: directories, nginx, systemd."""
    c = _conn()

    c.run(f"mkdir -p {REMOTE_DIR}")

    if not DOMAIN:
        print("ERROR: set DEPLOY_DOMAIN env var (e.g. api.kittypaw.app)")
        sys.exit(1)

    # Upload config files
    c.put(str(LOCAL_ROOT / "deploy" / "kittyapi.service"), "/tmp/kittyapi.service")
    c.put(str(LOCAL_ROOT / "deploy" / "kittyapi.nginx"), "/tmp/kittyapi.nginx")

    # Replace {{DOMAIN}} placeholder on server
    c.run(f"sed -i 's/{{{{DOMAIN}}}}/{DOMAIN}/g' /tmp/kittyapi.nginx")

    c.sudo("cp /tmp/kittyapi.service /etc/systemd/system/kittyapi.service")
    c.sudo("cp /tmp/kittyapi.nginx /etc/nginx/sites-enabled/kittyapi")
    c.sudo("systemctl daemon-reload")
    c.sudo("systemctl enable kittyapi")
    c.sudo("nginx -t && systemctl reload nginx")

    # Remind about .env
    exists = c.run(f"test -f {REMOTE_DIR}/.env", warn=True)
    if not exists.ok:
        c.put(str(LOCAL_ROOT / "deploy" / "env.example"), f"{REMOTE_DIR}/.env")
        print(f"\n>>> .env created from template — edit {REMOTE_DIR}/.env on server!")


@task
def deploy(ctx):
    """Build, upload binary, restart service, then run prod smoke."""
    binary_path = _local_build()

    c = _conn()

    # Backup current binary
    c.run(f"cp {REMOTE_DIR}/{BINARY} {REMOTE_DIR}/{BINARY}.prev 2>/dev/null || true")

    # Upload new binary
    c.put(str(binary_path), f"{REMOTE_DIR}/{BINARY}")
    c.run(f"chmod +x {REMOTE_DIR}/{BINARY}")

    # Restart
    c.sudo(f"systemctl restart {SERVICE}")
    c.run("sleep 2")
    c.sudo(f"systemctl is-active {SERVICE}")
    print("Deployed.")

    # Plan 8 v2 D4(A): post-deploy smoke verifies prod responds before
    # declaring success. Failures here exit non-zero so CI/manual deploys
    # surface regressions immediately.
    smoke(ctx)


@task
def smoke(ctx):
    """Run prod smoke against api.kittypaw.app (or BASE_URL override)."""
    result = subprocess.run(
        ["bash", str(LOCAL_ROOT / "deploy" / "smoke.sh")],
        cwd=LOCAL_ROOT,
    )
    if result.returncode != 0:
        print("Smoke failed — see above for the failing endpoints.")
        sys.exit(result.returncode)


@task
def migrate(ctx):
    """Upload and run database migrations on server."""
    c = _conn()

    c.run(f"mkdir -p {REMOTE_DIR}/migrations")
    for f in sorted((LOCAL_ROOT / "migrations").glob("*.sql")):
        c.put(str(f), f"{REMOTE_DIR}/migrations/{f.name}")

    c.run(f"set -a && . {REMOTE_DIR}/.env && set +a && "
          f"migrate -path {REMOTE_DIR}/migrations -database \"$DATABASE_URL\" up")


@task
def rollback(ctx):
    """Restore previous binary and restart."""
    c = _conn()
    c.run(f"cp {REMOTE_DIR}/{BINARY}.prev {REMOTE_DIR}/{BINARY}")
    c.sudo(f"systemctl restart {SERVICE}")
    c.sudo(f"systemctl is-active {SERVICE}")
    print("Rolled back.")


@task
def status(ctx):
    """Show service status."""
    c = _conn()
    c.sudo(f"systemctl status {SERVICE}", warn=True)


@task
def logs(ctx):
    """Tail service logs."""
    c = _conn()
    c.sudo(f"journalctl -u {SERVICE} -f -n 50", pty=True)
