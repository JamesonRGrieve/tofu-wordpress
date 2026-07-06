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

## netbox-wordpress SoT consumption

netbox-wordpress is the source of truth; the consumer layer reads its REST API and
drives these resources. The mapping of each SoT field to what converges it:

| netbox-wordpress field | Converged by |
|---|---|
| `WordPressPlugin/Theme.state` (absent/present_inactive/active) | `wordpress_plugin` / `wordpress_theme` `state` — **`absent` uninstalls** (fleet cruft removal) |
| `WordPressConstant`, hardening defines | `wordpress_config.constants` (manage-declared-only) |
| `WordPressSite.install_path` (docroot) | `wordpress_config.path` (and the other resources' `path`), defaulting to the provider `docroot` |
| `WordPressSite.login_slug` (wps-hide-login `whl_page`) | `wordpress_config.login_slug` — installs+activates wps-hide-login and sets `whl_page`; blank leaves the default `wp-login.php` |
| `WordPressSite.object_cache_instance` (Valkey/Redis) | `wordpress_config.object_cache_host` / `object_cache_port` — defines `WP_REDIS_HOST`/`WP_REDIS_PORT`, installs+activates redis-cache, runs `wp redis enable` (the backend host/port from the FK are resolved at the consumer layer) |
| `WordPressSite.safe_opt` (PHP hardening) | `wordpress_config.safe_opt` — a managed php.ini drop-in (`disable_functions`, `allow_url_fopen = Off`) |
| `WordPressSite.trusted_proxies` (mod_remoteip) | `wordpress_config.trusted_proxies` — an Apache mod_remoteip drop-in (`RemoteIPHeader X-Forwarded-For` + `RemoteIPTrustedProxy`) |
| `WordPressSite.enable_hsts` | `wordpress_config.enable_hsts` — an Apache mod_headers `Strict-Transport-Security` stanza (shares the managed Apache drop-in with `trusted_proxies`) |
| system cron form | `wordpress_cron` `mode` — `wp_cli` renders `wp cron event run --due-now` (the live fleet form), `wp_cron_php` the greenfield form; both import to 0-diff |
| `WordPressContentDirectory` (wp-content move) | `wordpress_content_dir` (staged, rollback-armed) |

### Managed hardening drop-ins

`safe_opt`, `trusted_proxies`, and `enable_hsts` are realized as **provider-managed
files** written over the transport, each carrying a `# Managed by OpenTofu` marker
and rendered by a pure, unit-tested function in `internal/wordpress/hardening.go`:

- **`safe_opt`** → a php.ini drop-in at `php_ini_path` (default
  `/etc/php/conf.d/zz-wp-hardening.ini`). `safe_opt = false` removes the managed
  drop-in.
- **`trusted_proxies` + `enable_hsts`** → a single Apache drop-in at
  `apache_conf_path` (default `/etc/apache2/conf-enabled/zz-wp-hardening.conf`)
  composing the mod_remoteip and mod_headers stanzas. When neither contributes any
  content (blank proxies + HSTS off) the managed file is removed.

Both paths are `Optional+Computed` attributes so a distro with a different conf
layout retargets them declaratively. The renderers are pure; the resource's write
side goes through the injected transport and is exercised in a hermetic test
(`TestApplySiteConfig`) that records the command sequence without contacting a
device.

## Boundary — what this provider does NOT own (host/OS layer)

A deliberate scope line, matching the provider's WP-CLI-over-SSH transport: the
following netbox-wordpress fields describe broad **node-OS / fleet** state, not
per-site WordPress state, so they are converged by the **Ansible baremetal role /
a future host (`proxmox_host_config`) resource**, NOT here. netbox-wordpress
remains their SoT; this provider simply is not their consumer.

- `WordPressSite.fpm_profile` (PHP-FPM pool / OPcache sizing tiers) and the
  `prod_hardening` **umbrella** (fail2ban, unattended-upgrades, AIDE, rkhunter,
  Cloudflare-range allowlists, and other node-wide OS hardening) — fleet/OS
  surfaces sized from the host, not a single install. The narrow, per-site Apache /
  PHP drop-ins that used to sit under this umbrella — `safe_opt`,
  `trusted_proxies`, `enable_hsts` — are now realized **here** by `wordpress_config`
  (see the SoT table above); the WordPress-side defines that pair with them (e.g.
  `FORCE_SSL_ADMIN`) remain expressible via `wordpress_config.constants`.
- `WordPressSite.install_path` as a **whole-install / vhost DocumentRoot
  relocation** (e.g. moving WP **core** from `/var/www/html/wordpress` →
  `/var/www/html` and repointing the Apache vhost) is an Apache/filesystem
  operation outside a WP-CLI provider's remit; it stays the
  `wp-docroot-standardize.sh` normalizer / host layer. This provider consumes
  `install_path` only as the **docroot** (`wordpress_config.path` et al.);
  contrast `wordpress_content_dir`, which relocates only `wp-content` — a pure
  WP-CLI + rsync operation this provider *does* own.

The reference deployment is `ansible/playbooks/applications/wordpress/baremetal/`.
