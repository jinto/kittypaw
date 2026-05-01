# Deployment — per-user daemon

KittyPaw ships as **one daemon per OS user**. Each user's data lives under
`~/.kittypaw/` and the daemon runs under that user's UID, so on multi-user
hosts the kernel's existing file-permission check already prevents cross-user
data access. This document covers registering the daemon with the platform
init system so it starts at login / boot and restarts on failure.

**Scope of this deployment model:**
- ✅ User A's kittypaw cannot read User B's `/home/B/.kittypaw` (Unix UID).
- ✅ Daemon auto-starts, auto-restarts, logs to the standard platform facility.
- ❌ A skill running inside User A's daemon can still touch anything User A
  has access to (`~/.ssh`, browser cookies, etc.). In-daemon skill isolation
  is out of scope for this document.

---

## Quick start

```bash
# One-shot: registers per-user unit/plist, starts the daemon.
kittypaw service install

# Second user on the same host picks a free port.
kittypaw service install --bind-port 3001
```

For a single-host, single-user install, the quick start stops here.
Everything below is detail for operators who need multi-user setups, custom
binary paths, logs, or uninstall.

---

## The `kittypaw service` command family

| Command | Effect |
|---|---|
| `kittypaw service install` | Write the unit/plist, start the daemon, enable auto-start |
| `kittypaw service install --bind-port 3001` | Same, but listen on a non-default port (for second users or when :3000 is taken) |
| `kittypaw service install --binary /custom/path` | Register an explicit binary path instead of the currently running one |
| `kittypaw service uninstall` | Stop the daemon and remove the unit/plist |
| `kittypaw service status` | Show active state, bind, PID, cgroup delegation (Linux) |
| `kittypaw service logs [-f]` | Tail `journalctl --user -u kittypaw` (Linux) or `~/.kittypaw/logs/stderr.log` (macOS) |

`install` is idempotent — the second run stops the existing daemon, rewrites
the unit/plist, and starts it again. A port probe runs before rewrite, so a
collision surfaces with a copy-paste hint:

```
error: port 127.0.0.1:3000 is already in use.

  Another process — likely another OS user's kittypaw daemon — is
  bound to this port. Pick a free port and retry:

    kittypaw service install --bind-port 3001

  Then point your client at the same port:
    kittypaw chat --server http://127.0.0.1:3001
```

---

## Data directory: `KITTYPAW_CONFIG_DIR`

`core.ConfigDir()` honours the `KITTYPAW_CONFIG_DIR` environment variable.
If set, that path is used verbatim (and `chmod 0700`-ed). Otherwise it falls
back to `~/.kittypaw`. Both init-system templates set this env explicitly so
the path stays predictable regardless of how the service was launched.

The directory is always forced to mode `0700`, including on installs that
were created under the older `0755` default.

---

## Onboarding flow

### Machine-wide binary install (once, by the first administrator)

```bash
curl -fsSL https://raw.githubusercontent.com/kittypaw-app/kittypaw/main/install.sh | sh
# → /usr/local/bin/kittypaw
```

This is a separate step from registering the service. Every OS user on the
host reuses the same binary; each user still needs to register their own
service under their own UID.

### First user (User A)

```bash
# Interactive wizard. Creates ~/.kittypaw/accounts/alice/ and local Web UI credentials.
kittypaw setup --account alice

# Non-interactive / CI equivalent:
printf '%s\n' "$LOCAL_WEB_PASSWORD" |
  kittypaw setup --account alice --password-stdin --no-service --provider anthropic --api-key "$KEY"
kittypaw service install
```

When `kittypaw setup` runs with TTY stdin/stdout it prompts for service
install right after the completion box, so a typical first-time user never
has to learn the `service` subcommand explicitly. `--no-service` skips the
prompt; the `service install` command stays available as an explicit path.

`service install` auto-detects the running binary via `os.Executable()` and
substitutes its absolute path into the unit/plist, so a user-local install
under `~/.local/bin` also works without hand editing.

Fresh setup writes account config under `~/.kittypaw/accounts/<accountID>/`.
Existing upgraded installs may still have `~/.kittypaw/accounts/default/`;
that legacy account remains valid. If more than one local account exists, CLI
commands that need account state must specify one explicitly:

```bash
KITTYPAW_ACCOUNT=alice kittypaw chat
kittypaw chat --account alice
```

Additional local accounts are provisioned with their own Web UI credentials:

```bash
printf '%s\n' "$BOB_WEB_PASSWORD" | kittypaw account add bob --password-stdin
```

**Web onboarding.** Once a daemon is listening, `kittypaw setup --web` opens
the wizard UI in a browser (served by the daemon itself at `http://127.0.0.1:3000/`).
If no daemon is up the command prints recovery steps instead of silently
spawning a foreground server — use `kittypaw service install` or
`kittypaw serve` first. Once `~/.kittypaw/auth.json` contains local users, the
Web UI requires login and setup/configuration applies to the logged-in account.

### Second user (User B, same host)

```bash
# Log in as User B.
kittypaw setup --account bob --no-service     # wizard alone; skip the install prompt
kittypaw service install --bind-port 3001     # :3000 is taken by User A
```

User B passes `--no-service` because the default port probe in the prompt
would fail against User A's live daemon. Installing with an explicit
`--bind-port` surfaces the port choice clearly.

The CLI probes the target port before rewriting any files — if User A is
still on :3000, the install aborts early with the hint above. Pick a free
port; kittypaw's chat REPL, web UI, and any scripted client must connect to
the same `--server http://127.0.0.1:3001`.

---

## Linux details — systemd user unit

### Prerequisites that require admin rights

`kittypaw service install` runs without root, but two system-wide settings
change the experience; the CLI warns about both if absent.

**1. Cgroup delegation** — `MemoryMax` / `CPUQuota` / `TasksMax` in the unit
are silently ignored in user scope unless cgroup controllers are delegated
to user slices:

```bash
sudo mkdir -p /etc/systemd/system/user-.slice.d
printf '[Slice]\nDelegate=yes\n' |
  sudo tee /etc/systemd/system/user-.slice.d/10-delegate.conf
sudo systemctl daemon-reload
```

These directives are only post-mortem observability ("find the runaway
skill"), not a security boundary — a skill that trips the limit OOM-kills
the whole daemon, which is what you want as a signal.

**2. Linger** — without linger, the user systemd manager exits when you log
out, taking the daemon with it. To keep kittypaw alive across logouts and
to start it at boot:

```bash
sudo loginctl enable-linger $USER
```

### Raw unit location

`~/.config/systemd/user/kittypaw.service` (written by the CLI). The source
template lives at `packaging/linux/systemd/user/kittypaw.service` — inspect
or copy it for custom packaging.

### Low-level alternative (no CLI)

Package maintainers who want to ship the unit without relying on the CLI
can call the standalone script directly:

```bash
sh packaging/linux/register-service.sh
KITTYPAW_BIND_PORT=3001 sh packaging/linux/register-service.sh
```

The script does the same work as `kittypaw service install` but in POSIX
sh — useful for AUR / .deb / .rpm post-install hooks.

---

## macOS details — launchd LaunchAgent

### Raw plist location

`~/Library/LaunchAgents/dev.kittypaw.daemon.plist` (written by the CLI).
The source template lives at `packaging/macos/LaunchAgents/dev.kittypaw.daemon.plist`.

launchd does not expand `~` or `$HOME` inside plist values, so the CLI
substitutes absolute-path tokens at install time. Never copy the template
directly without substitution.

### Full Disk Access is not granted to LaunchAgents

If a future kittypaw feature needs to read files outside `~/.kittypaw/`
(e.g. scanning another app's data), a LaunchAgent is insufficient — that
feature would need an `.app` bundle. Current skills that stay inside
`~/.kittypaw/` are unaffected.

### Low-level alternative (no CLI)

```bash
sh packaging/macos/register-service.sh
KITTYPAW_BIND_PORT=3001 sh packaging/macos/register-service.sh
```

---

## Verification

```bash
kittypaw service status
# active: yes
# ExecStart={ argv[...] }
# MainPID=12345
# ActiveState=active
# Delegate=yes       (Linux only)

kittypaw service logs -f
```

### Linux — check cgroup limits are actually applied

```bash
UNIT=$(systemctl --user show -p ControlGroup --value kittypaw.service)
cat /sys/fs/cgroup${UNIT}/memory.max     # should match MemoryMax in the unit
cat /sys/fs/cgroup${UNIT}/cpu.max        # should match CPUQuota
```

If these files show `max` / `max 100000`, cgroup delegation is not enabled
(see Prerequisites above) — the directives in the unit are being ignored.

---

## Uninstall

```bash
kittypaw service uninstall
```

This stops the daemon and removes the unit/plist. It does NOT remove
`~/.kittypaw/` (your data). If you want that too:

```bash
rm -rf ~/.kittypaw
```

---

## Known pitfalls

- **`XDG_RUNTIME_DIR` not set over SSH without linger** — `systemctl --user`
  fails with `Failed to connect to bus` when you SSH into a host that has
  no active session for the user and linger disabled. Enable linger.
- **Port 3000 as a default collides with other tools** — Grafana, React dev
  servers, etc. also default to :3000. If anything is already listening
  there the install aborts; pick a free port with `--bind-port`.
- **Auto-update of `/usr/local/bin/kittypaw`** — in-place binary replacement
  works because the unit's ExecStart resolves at restart time. Binary path
  *moves* (e.g. `/usr/local/bin` → `/opt/kittypaw/bin`) require
  `kittypaw service install` to be re-run.
- **Second user who forgets `--bind-port`** — CLI catches this with the
  port probe, but the failure mode is a loud error rather than auto-port
  allocation. This is deliberate: auto-allocating ports would hide from
  the user which port their client needs to connect to.
