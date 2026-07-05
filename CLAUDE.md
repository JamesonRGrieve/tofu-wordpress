<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
# tofu-wordpress — Agent Operating Guide

> **⛔ NO DIRECT APPLIES TO ANY DEVICE — EVER.**
>
> Direct changes to **any** device — router, firewall, switch, access point, hypervisor, mail gateway, or any other appliance — are **NEVER** permitted, by anyone, for any reason. This bans hand-run `tofu apply`, hand-run `ansible-playbook`, SSH/serial/CLI config writes, REST/API mutations, and web-GUI/console edits.
>
> **Every change MUST flow through the sanctioned pipeline:** declare intent in **prod-netbox** (the single source of truth), then realize it **only** through **prod-semaphore** (the sanctioned runner). A change that did not go **prod-netbox → prod-semaphore** must never reach a device.
>
> **Sole exception:** a specific direct action is permitted *only* when the operator authorizes that exact action in advance by answering an explicit, **alarm-flavored `AskUserQuestion`** — one that names the device, the precise action, and the risk — **in the affirmative**. No standing grants, no inferred permission, no carrying one approval to another action or device. Absent that in-the-moment "yes," the answer is no.
>
> **Never offload the work onto the operator.** When you are blocked, ask for the break-glass authorization that lets *you* do the job — never ask the operator to run a command, SSH in, or make the change on your behalf. The operator grants permission; they do not perform your labour.

Native OpenTofu/Terraform provider for **WordPress** — installed state (WP-CLI
core/plugin/theme), `wp-config.php`, system cron, and safe content-directory
relocation, over an SSH + WP-CLI transport. Sibling of `../tofu-opnsense` and
`../tofu-proxmox` (same generic-typed-resource philosophy, same toolchain). The
workspace-root `../CLAUDE.md` applies; this adds specifics.

## What this is / isn't

- **Is:** a provider that drives WordPress hosts entirely through **WP-CLI over
  SSH** (`wp …`) plus a few filesystem commands (rsync, symlink, cron file).
- **Isn't:** an HTTP/REST provider (WordPress has no management API), and not a
  page-content/CMS editor. It manages install-state, config defines, cron, and the
  content directory — not posts/pages/media.

## Design tenets

- **Transport/logic layer is framework-free.** `internal/wordpress/` imports no
  terraform-plugin-framework — SSH client, WP-CLI wrapper, and the pure builders
  (wp-config typing, relocation plan, cron rendering, version parsing) live here
  and are unit-tested through an **injected `Executor`**. The provider glue in
  `internal/provider/` wires it to the framework.
- **The relocation is a staged, rollback-armed plan** (`BuildRelocationPlan` +
  `ExecuteRelocation`) — never a naive `mv`; the original is retained until a
  real HTTP-200 health check passes. See `DESIGN.md` for the full contract.
- **Manage-declared-only diff** on `wordpress_config`: only declared constants are
  reconciled (`ReconcileConstant`); unmanaged defines are never clobbered. Fix a
  spurious diff in the subset logic, never by widening stored state.
- **Import to 0-diff is the bar** for every stateful resource.
- **Plugin and theme share one `componentResource`** parameterized by `kind` —
  don't fork them.

## Toolchain

- Go 1.26.4 (`/home/jameson/.local/go`), `terraform-plugin-framework` v1.19.0.
  Do **not** add or bump deps — `go.mod`/`go.sum` mirror `../tofu-opnsense`.
- Provider address: `registry.terraform.io/jamesonrgrieve/wordpress`; TypeName
  `wordpress` (resources `wordpress_*`).
- General Go / Terraform-provider standards are canonical at
  `/home/jameson/source/ai-prompts/go.md` and `.../tofu.md` — read them first;
  this file holds only repo-specific facts.
- `make check` (tidy + fmt + vet + test + build) is the gate; `.githooks/pre-commit`
  re-runs it. Enable with `git config core.hooksPath .githooks`. Never `--no-verify`.

## Hard rules

- **No secrets in the repo or state.** WP admin password, DB password, and AUTH
  salts are **write-only** attributes injected from OpenBao at apply. SSH auth is
  **key/cert only** — never a password, never `sshpass`.
- **Never apply against a production WordPress host by hand.** A bad `wp config
  set` or a botched content-dir move takes a live site down. Validate only against
  a lab/clone install, and drive live changes via Semaphore.
- **A content-dir relocation is a production-data move.** Prove it on a lab twin
  in byte-for-byte identical form, arm an out-of-band snapshot/rollback, and block
  on the real external health check before it touches production. The in-provider
  health gate is necessary, not sufficient.
- **NetBox is the source of truth** at the consumer layer (`netbox-wordpress`
  `WordPressSite`/`WordPressContentDirectory`; DB creds via
  `netbox-database.DatabaseUser.credential_ref` → OpenBao). Never hand-maintain
  competing tfvars.
