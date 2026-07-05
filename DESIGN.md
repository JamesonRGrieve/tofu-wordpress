<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
# tofu-wordpress — Design

Native OpenTofu/Terraform provider for WordPress installed state, `wp-config.php`,
system cron, and safe content-directory relocation, over an SSH + WP-CLI
transport. This document is the architectural summary and the relocation
contract; it describes the target end state.

## Architecture

Two layers, mirroring `tofu-opnsense` / `tofu-proxmox`:

- **`internal/wordpress/`** — the transport and pure logic, **free of any
  terraform-plugin-framework import** so it is unit-testable in isolation:
  - `ssh.go` — an `os/exec`-based SSH client (no in-process SSH library, so
    `go.mod` stays identical to the siblings). Key/cert auth only; a `key_pem` is
    materialized to a temp `0600` file per call and removed after. Transient
    connection resets are retried.
  - `wpcli.go` — a `WPCLI` wrapper over an injected `Executor`. `wpCommand`
    renders a shell-safe `wp --path=… --allow-root …` string (pure); the run
    methods go through the `Executor` so apply logic is exercised in tests with a
    fake — **no unit test ever contacts a device**.
  - `config.go` — wp-config constant typing (`IsRawConstant`), value
    normalization, and `ReconcileConstant` (manage-declared-only diff).
  - `cron.go` — cron.d entry rendering/parsing.
  - `version.go` — version parse/compare for the core update decision.
  - `relocate.go` — the relocation plan builder + executor (see below).
- **`internal/provider/`** — the framework glue: `provider.go` (host/port/user +
  SSH auth + docroot default) and one `*_resource.go` per resource. Resources
  wrap the transport in a `WPCLI` and are configured with a shared
  `*providerClient` (SSH client + default docroot). Plugin and theme share one
  `componentResource` implementation parameterized by `kind` (DRY).

`main.go` serves the provider at `registry.terraform.io/jamesonrgrieve/wordpress`.

## Transport: SSH + WP-CLI

WordPress exposes no management API. All reads and writes are WP-CLI (`wp …`)
invocations plus a few filesystem commands (rsync, symlink, cron file write),
executed on the host over SSH. WP-CLI runs with `--allow-root` (the transport
user is typically root). The request `ctx` timeout bounds each command; the
content-dir rsync uses a longer default (120s) because a large uploads tree can
take a while.

## Schema conventions

- Every stateful resource implements `ImportState` to **0-diff**; `path` defaults
  to the provider `docroot`, is `Optional+Computed`, and is filled on
  create/read/import so the plan is stable.
- **Manage-declared-only** diff on `wordpress_config`: only the constants the
  configuration declares are read back and reconciled; `ReconcileConstant` keeps
  the declared form when it is semantically equal to the device value (e.g.
  declared `"false"` vs read-back `""`), and surfaces the live value on real
  drift. Unmanaged defines are never touched.
- Singleton-ish resources (`core`, `config`, `content_dir`) use a no-op `Delete`
  — the underlying install/config persists; the resource just stops managing it.

## Secrets

`admin_password` (core), `db_password` and `salts` (config) are **write-only**
attributes: read from config at apply via `req.Config.GetAttribute`, never written
to plan or state. They are injected from OpenBao at apply. SSH auth is key/cert
only. State stores no plaintext credential.

## Content-directory relocation contract (the headline)

Moving `wp-content` on disk is **interruption-unsafe** — a naive `mv` that dies
mid-copy leaves the site with a half-populated content directory and no path back.
The relocation is therefore modeled as a **pure staged plan** (`BuildRelocationPlan`,
fully unit-tested) executed behind the injected `Executor` (`ExecuteRelocation`),
with a rollback path.

### Stages (forward)

1. **`maintenance-activate`** — `wp maintenance-mode activate` quiesces writes so
   nothing mutates content mid-copy.
2. **`mkdir-target`** + **`rsync-copy`** — `rsync -aH --checksum <src>/ <dst>/`.
   A **copy, never a move**: the original is left intact.
3. **`verify-copy`** (`StepVerify`) — compares `find … | wc -l` file counts of
   source and target; a mismatch **aborts** before any config change.
4. **`config-wp-content-dir`** (+ optional `config-wp-content-url`,
   `option-upload-path`) — repoints WordPress at the new location. This is the
   **commit point**.
5. **`keep-symlink`** (optional) — leaves a `<src> → <dst>` symlink so hardcoded
   old paths still resolve. The original bytes are **renamed aside**
   (`<src>.pre-tofu`), never deleted, so a rollback can restore them. Idempotent
   (skipped when `<src>` is already a symlink).
6. **`health-check`** (`StepHealth`) — resolves `wp option get siteurl` and does
   an HTTP GET; the live site **must** return `200`.
7. **`maintenance-deactivate`** — releases maintenance mode on success.

### Rollback (on any forward-step failure)

`ExecuteRelocation` runs the rollback steps **best-effort**:

1. **`rollback-config-wp-content-dir`** — restores `WP_CONTENT_DIR` to the
   original path.
2. **`rollback-restore-original`** (when `keep_symlink`) — removes the new symlink
   and moves `<src>.pre-tofu` back to `<src>`.
3. **`rollback-maintenance-deactivate`** — always releases maintenance mode.

The **original content directory is never deleted** — before, during, or after.
The move only ever *adds* the target copy; cleanup of the retained original is a
deliberate, separate operator action, not part of this resource.

### Idempotency

`content_dir` is `Computed`: after a completed move it equals `target_content_dir`,
so a re-apply produces a **no-op** plan (`BuildRelocationPlan` returns `NoOp` when
the two are equal). `Read` refreshes `content_dir` from the live `WP_CONTENT_DIR`
define, so an out-of-band move back is detected as drift.

### Risk & verification owed

This is a **production-data move on a live site**. Per the workspace change-safety
protocol it is driven **only** through the sanctioned pipeline (prod-netbox →
prod-semaphore), **proven on a lab twin first in byte-for-byte identical form**,
with a snapshot/rollback armed out-of-band. The in-provider health check is a
real HTTP-200 gate, but a green plan is not proof — the lab-twin drill (see
`todo.json`) is the verification still owed before any production relocation. The
provider never applies to a device directly; the device-apply paths are exercised
in unit tests exclusively through an injected fake executor.

## Fleet / hardening parity roadmap

The reference deployment is `ansible/playbooks/applications/wordpress/baremetal/`.
This provider covers install-state, wp-config defines, cron, and content-dir; the
following parity items are tracked in `todo.json`:

- **FPM/OPcache traffic tiers** (`low`/`medium`/`high`) and `wordpress_prod_hardening`
  (fail2ban, mod_remoteip/Cloudflare ranges, unattended-upgrades, AIDE, rkhunter,
  Apache/PHP hardening drop-ins) live in Ansible today; the node-OS hardening
  belongs behind `proxmox_host_config` / a future host resource, the WordPress-side
  defines are already expressible via `wordpress_config`.
- The production cron invokes `wp cron event run --due-now` as the web user; this
  provider renders a `wp-cron.php`-via-PHP cron.d entry (per the resource spec).
  Reconciling the two forms is a tracked parity item.
- Object cache (Valkey/Redis) `WP_REDIS_*` defines are covered by `wordpress_config`;
  installing/enabling the Redis Object Cache plugin is covered by `wordpress_plugin`.
