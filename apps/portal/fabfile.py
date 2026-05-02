"""
Kitty Portal deployment - fab deploy / fab setup / fab logs / fab status / fab rollback
"""
import os
import subprocess
import sys
from pathlib import Path

from fabric import task

HOST = os.environ.get("DEPLOY_HOST", "second")
DOMAIN = os.environ.get("DEPLOY_DOMAIN", "")
REMOTE_DIR = "/home/jinto/kittyportal"
SERVICE = "kittyportal"
BINARY = "kittyportal"

LOCAL_ROOT = Path(__file__).resolve().parent
REPO_ROOT = LOCAL_ROOT.parent.parent


def _conn():
    from fabric import Connection
    return Connection(HOST)


def _local_build():
    version, commit = _build_metadata()
    print(f"Building {BINARY} for linux/amd64 ({version} {commit}) ...")
    env = {**os.environ, "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"}
    result = subprocess.run(
        [
            "go",
            "build",
            "-ldflags",
            f"-s -w -X main.version={version} -X main.commit={commit}",
            "-o",
            f"{BINARY}-linux",
            "./cmd/server",
        ],
        cwd=LOCAL_ROOT,
        env=env,
    )
    if result.returncode != 0:
        print("Build failed.")
        sys.exit(1)
    return LOCAL_ROOT / f"{BINARY}-linux"


def _git(*args, default="unknown"):
    try:
        return subprocess.check_output(["git", *args], cwd=REPO_ROOT, text=True).strip()
    except Exception:
        return default


def _build_metadata():
    version = os.environ.get("VERSION") or _git("describe", "--tags", "--always", default="dev")
    commit = os.environ.get("COMMIT") or _git("rev-parse", "--short=12", "HEAD")
    return version, commit


@task
def setup(ctx):
    """Initial server setup: directories, nginx, systemd."""
    c = _conn()
    c.run(f"mkdir -p {REMOTE_DIR}")

    if not DOMAIN:
        print("ERROR: set DEPLOY_DOMAIN env var (e.g. portal.kittypaw.app)")
        sys.exit(1)

    c.put(str(LOCAL_ROOT / "deploy" / "kittyportal.service"), "/tmp/kittyportal.service")
    c.put(str(LOCAL_ROOT / "deploy" / "kittyportal.nginx"), "/tmp/kittyportal.nginx")
    c.run(f"sed -i 's/{{{{DOMAIN}}}}/{DOMAIN}/g' /tmp/kittyportal.nginx")

    c.sudo("cp /tmp/kittyportal.service /etc/systemd/system/kittyportal.service")
    c.sudo("cp /tmp/kittyportal.nginx /etc/nginx/sites-enabled/kittyportal")
    c.sudo("systemctl daemon-reload")
    c.sudo("systemctl enable kittyportal")
    c.sudo("nginx -t && systemctl reload nginx")

    exists = c.run(f"test -f {REMOTE_DIR}/.env", warn=True)
    if not exists.ok:
        c.put(str(LOCAL_ROOT / "deploy" / "env.example"), f"{REMOTE_DIR}/.env")
        print(f"\n>>> .env created from template; edit {REMOTE_DIR}/.env on server.")


@task
def deploy(ctx):
    """Build, upload binary, restart service, then run smoke."""
    binary_path = _local_build()
    c = _conn()

    c.run(f"mv {REMOTE_DIR}/{BINARY} {REMOTE_DIR}/{BINARY}.prev 2>/dev/null || true")
    c.put(str(binary_path), f"{REMOTE_DIR}/{BINARY}")
    c.run(f"chmod +x {REMOTE_DIR}/{BINARY}")
    c.sudo(f"systemctl restart {SERVICE}")
    c.run("sleep 2")
    c.sudo(f"systemctl is-active {SERVICE}")
    print("Deployed.")
    smoke(ctx)


@task
def smoke(ctx):
    """Run smoke against portal.kittypaw.app and api.kittypaw.app."""
    result = subprocess.run(
        ["bash", str(LOCAL_ROOT / "deploy" / "smoke.sh")],
        cwd=LOCAL_ROOT,
    )
    if result.returncode != 0:
        print("Smoke failed; see above for failing endpoints.")
        sys.exit(result.returncode)


@task
def migrate(ctx):
    """Upload and run portal-owned database migrations on server."""
    c = _conn()
    c.run(f"mkdir -p {REMOTE_DIR}/migrations")
    for f in sorted((LOCAL_ROOT / "migrations").glob("*.sql")):
        c.put(str(f), f"{REMOTE_DIR}/migrations/{f.name}")
    c.run(
        f"set -a && . {REMOTE_DIR}/.env && set +a && "
        f"migrate -path {REMOTE_DIR}/migrations -database \"$DATABASE_URL\" up"
    )


@task
def status(ctx):
    c = _conn()
    c.sudo(f"systemctl status {SERVICE} --no-pager", warn=True)


@task
def logs(ctx, lines=100):
    c = _conn()
    c.sudo(f"journalctl -u {SERVICE} -n {lines} --no-pager")


@task
def rollback(ctx):
    c = _conn()
    c.run(f"test -f {REMOTE_DIR}/{BINARY}.prev")
    c.run(f"mv {REMOTE_DIR}/{BINARY}.prev {REMOTE_DIR}/{BINARY}")
    c.run(f"chmod +x {REMOTE_DIR}/{BINARY}")
    c.sudo(f"systemctl restart {SERVICE}")
    print("Rolled back.")
