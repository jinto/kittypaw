"""
KittyChat deployment — fab setup / fab deploy / fab smoke / fab logs / fab status / fab rollback
"""
import os
import secrets
import subprocess
import sys
from pathlib import Path

from fabric import task

HOST = os.environ.get("DEPLOY_HOST", "second")
DOMAIN = os.environ.get("DEPLOY_DOMAIN", "")
REMOTE_DIR = os.environ.get("DEPLOY_REMOTE_DIR", "/home/jinto/kittychat")
SERVICE_USER = os.environ.get("DEPLOY_USER", "jinto")
SERVICE_GROUP = os.environ.get("DEPLOY_GROUP", SERVICE_USER)
SERVICE = "kittychat"
BINARY = "kittychat"

LOCAL_ROOT = Path(__file__).resolve().parent


def _conn():
    from fabric import Connection

    return Connection(HOST)


def _local_build():
    """Cross-compile for Linux x86_64 (static binary, no CGO)."""
    print(f"Building {BINARY} for linux/amd64 ...")
    env = {**os.environ, "GOOS": "linux", "GOARCH": "amd64", "CGO_ENABLED": "0"}
    result = subprocess.run(
        ["go", "build", "-o", f"{BINARY}-linux", "./cmd/kittychat"],
        cwd=LOCAL_ROOT,
        env=env,
    )
    if result.returncode != 0:
        print("Build failed.")
        sys.exit(1)
    return LOCAL_ROOT / f"{BINARY}-linux"


def _remote_binary_path(suffix=""):
    return f"{REMOTE_DIR}/{BINARY}{suffix}"


@task
def setup(ctx):
    """Initial server setup: directories, nginx, systemd, env template."""
    c = _conn()

    c.run(f"mkdir -p {REMOTE_DIR}")

    if not DOMAIN:
        print("ERROR: set DEPLOY_DOMAIN env var (e.g. chat.kittypaw.app)")
        sys.exit(1)

    c.put(str(LOCAL_ROOT / "deploy" / "kittychat.service"), "/tmp/kittychat.service")
    c.put(str(LOCAL_ROOT / "deploy" / "kittychat.nginx"), "/tmp/kittychat.nginx")

    c.run(f"sed -i 's/{{{{DOMAIN}}}}/{DOMAIN}/g' /tmp/kittychat.nginx")
    c.run(f"sed -i 's#{{{{REMOTE_DIR}}}}#{REMOTE_DIR}#g' /tmp/kittychat.service")
    c.run(f"sed -i 's#{{{{SERVICE_USER}}}}#{SERVICE_USER}#g' /tmp/kittychat.service")
    c.run(f"sed -i 's#{{{{SERVICE_GROUP}}}}#{SERVICE_GROUP}#g' /tmp/kittychat.service")

    c.sudo("cp /tmp/kittychat.service /etc/systemd/system/kittychat.service")
    c.sudo("cp /tmp/kittychat.nginx /etc/nginx/sites-enabled/kittychat")
    c.sudo("systemctl daemon-reload")
    c.sudo("systemctl enable kittychat")
    c.sudo("nginx -t && systemctl reload nginx")

    exists = c.run(f"test -f {REMOTE_DIR}/.env", warn=True)
    if not exists.ok:
        rendered = _render_env_example()
        tmp = LOCAL_ROOT / "deploy" / ".env.generated"
        try:
            tmp.write_text(rendered, encoding="utf-8")
            c.put(str(tmp), f"{REMOTE_DIR}/.env")
        finally:
            tmp.unlink(missing_ok=True)
        print(f"\n>>> .env created at {REMOTE_DIR}/.env with generated MVP tokens.")


@task
def deploy(ctx):
    """Build, upload binary, restart service, then run prod smoke."""
    binary_path = _local_build()
    c = _conn()

    c.run(f"cp {_remote_binary_path()} {_remote_binary_path('.prev')} 2>/dev/null || true")
    c.put(str(binary_path), _remote_binary_path(".new"))
    c.run(f"chmod +x {_remote_binary_path('.new')}")
    c.run(f"mv {_remote_binary_path('.new')} {_remote_binary_path()}")

    c.sudo(f"systemctl restart {SERVICE}")
    c.run("sleep 2")
    c.sudo(f"systemctl is-active {SERVICE}")
    print("Deployed.")

    smoke(ctx)


@task
def smoke(ctx):
    """Run prod smoke against chat.kittypaw.app (or BASE_URL override)."""
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
    c.run(f"cp {_remote_binary_path('.prev')} {_remote_binary_path()}")
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


def _render_env_example():
    template = (LOCAL_ROOT / "deploy" / "env.example").read_text(encoding="utf-8")
    return (
        template.replace("CHANGE_ME_API_TOKEN", "kp_api_" + secrets.token_urlsafe(32))
        .replace("CHANGE_ME_DEVICE_TOKEN", "kp_dev_" + secrets.token_urlsafe(32))
    )
