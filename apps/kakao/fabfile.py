"""
KittyKakao deployment — fab deploy / fab setup / fab logs / fab status / fab rollback
"""
import os
import subprocess
import sys
from pathlib import Path

from fabric import task

HOST = os.environ.get("DEPLOY_HOST", "second")
DOMAIN = os.environ.get("DEPLOY_DOMAIN", "")
REMOTE_DIR = os.environ.get("DEPLOY_REMOTE_DIR", "/home/jinto/kittykakao")
SERVICE_USER = os.environ.get("DEPLOY_USER", "jinto")
SERVICE_GROUP = os.environ.get("DEPLOY_GROUP", SERVICE_USER)
SERVICE = "kittykakao"
BINARY = "kittykakao"

LOCAL_ROOT = Path(__file__).resolve().parent
REPO_ROOT = LOCAL_ROOT.parent.parent


def _conn():
    from fabric import Connection
    return Connection(HOST)


def _local_build():
    """Cross-compile for Linux x86_64 (static binary, no CGO)."""
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
            "./cmd/kittykakao",
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
        print("ERROR: set DEPLOY_DOMAIN env var (e.g. relay.example.com)")
        sys.exit(1)

    # Upload config files
    c.put(str(LOCAL_ROOT / "deploy" / "kittykakao.service"), "/tmp/kittykakao.service")
    c.put(str(LOCAL_ROOT / "deploy" / "kittykakao.nginx"), "/tmp/kittykakao.nginx")

    # Replace {{DOMAIN}} placeholder on server
    c.run(f"sed -i 's/{{{{DOMAIN}}}}/{DOMAIN}/g' /tmp/kittykakao.nginx")
    c.run(f"sed -i 's#{{{{REMOTE_DIR}}}}#{REMOTE_DIR}#g' /tmp/kittykakao.nginx")
    c.run(f"sed -i 's#{{{{REMOTE_DIR}}}}#{REMOTE_DIR}#g' /tmp/kittykakao.service")
    c.run(f"sed -i 's#{{{{SERVICE_USER}}}}#{SERVICE_USER}#g' /tmp/kittykakao.service")
    c.run(f"sed -i 's#{{{{SERVICE_GROUP}}}}#{SERVICE_GROUP}#g' /tmp/kittykakao.service")

    c.sudo("cp /tmp/kittykakao.service /etc/systemd/system/kittykakao.service")
    c.sudo("cp /tmp/kittykakao.nginx /etc/nginx/sites-enabled/kittykakao")
    c.sudo("systemctl daemon-reload")
    c.sudo("systemctl enable kittykakao")
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

    # Backup current binary, then upload to a fresh inode. SFTP can fail when
    # writing directly over the executable that systemd is still running.
    c.run(f"cp {REMOTE_DIR}/{BINARY} {REMOTE_DIR}/{BINARY}.prev 2>/dev/null || true")
    c.put(str(binary_path), f"{REMOTE_DIR}/{BINARY}.new")
    c.run(f"chmod +x {REMOTE_DIR}/{BINARY}.new")
    c.run(f"mv {REMOTE_DIR}/{BINARY}.new {REMOTE_DIR}/{BINARY}")

    # Restart
    c.sudo(f"systemctl restart {SERVICE}")
    c.run("sleep 1")
    c.sudo(f"systemctl is-active {SERVICE}")
    print("Deployed.")
    smoke(ctx)


@task
def smoke(ctx):
    """Run prod smoke against kakao.kittypaw.app (or BASE_URL override)."""
    env = {**os.environ}
    if "BASE_URL" not in env and DOMAIN:
        env["BASE_URL"] = f"https://{DOMAIN}"
    result = subprocess.run(
        ["bash", str(LOCAL_ROOT / "deploy" / "smoke.sh")],
        cwd=LOCAL_ROOT,
        env=env,
    )
    if result.returncode != 0:
        print("Smoke failed — see above for the failing endpoint.")
        sys.exit(result.returncode)


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
