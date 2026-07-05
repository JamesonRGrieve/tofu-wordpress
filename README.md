<!-- SPDX-License-Identifier: AGPL-3.0-or-later -->
# tofu-wordpress

A native OpenTofu/Terraform provider for **WordPress** — installed state (WP-CLI
core/plugin/theme), `wp-config.php` defines, the system-cron `wp-cron` entry, and
a content-directory management system including **safe on-disk relocation** —
driven over an **SSH + WP-CLI** transport.

Sibling of `tofu-opnsense` / `tofu-proxmox`: same toolchain (Go 1.26.4,
`terraform-plugin-framework` v1.19.0), same house standards. General Go /
Terraform-provider rules are canonical at `/home/jameson/source/ai-prompts/go.md`
and `.../tofu.md`.

Address: `registry.terraform.io/jamesonrgrieve/wordpress` · TypeName `wordpress`.

## Why a provider (not a shell module)

WordPress has no HTTP management API; everything is done by running `wp` (WP-CLI)
and a few filesystem commands on the host. A native provider gives typed schemas,
manage-declared-only diffs, import-to-0-diff, and a **testable, rollback-armed**
relocation orchestration that a `local-exec` script cannot.

## Resources

| Resource | Manages | WP-CLI / mechanism |
|---|---|---|
| `wordpress_core` | Core install/update | `wp core version` / `download` / `install` / `update --version=` |
| `wordpress_config` | `wp-config.php` defines (manage-declared-only) | `wp config get` / `set` (`--raw` for bool/int constants) |
| `wordpress_plugin` | A plugin (install/activate/version/delete) | `wp plugin install/activate/deactivate/update/delete/get` |
| `wordpress_theme` | A theme (same shape) | `wp theme …` (shares the plugin implementation) |
| `wordpress_cron` | System cron for wp-cron | `/etc/cron.d` entry running `wp-cron.php` (pairs with `DISABLE_WP_CRON=true`) |
| `wordpress_content_dir` | Content directory + **safe relocation** | staged rsync + verify + config repoint + health check + rollback |

## Import IDs

| Resource | Import ID | Example |
|---|---|---|
| `wordpress_core` | `<docroot>` | `tofu import wordpress_core.site /var/www/html` |
| `wordpress_config` | `<docroot>` | `tofu import wordpress_config.site /var/www/html` |
| `wordpress_plugin` | `<docroot>/<slug>` (or bare `<slug>`) | `tofu import wordpress_plugin.wc /var/www/html/woocommerce` |
| `wordpress_theme` | `<docroot>/<slug>` | `tofu import wordpress_theme.sf /var/www/html/storefront` |
| `wordpress_cron` | `<docroot>` | `tofu import wordpress_cron.site /var/www/html` |
| `wordpress_content_dir` | `<docroot>` | `tofu import wordpress_content_dir.site /var/www/html` |

Every stateful resource imports to **0-diff** — onboard a live install by
importing, then planning to confirm no changes.

## Secrets

Zero secrets in state, ever. The WP **admin password**, **DB password**, and the
**AUTH salts** are `WriteOnly` attributes — read from config at apply and never
persisted. Supply them from OpenBao via `TF_VAR_*` (or an ephemeral
`vault_kv_secret_v2`). SSH auth is **key/cert only** (an OpenBao-signed key via
`ssh_key_pem`, or an on-disk `ssh_key_file`) — never a password, never `sshpass`.

## Source of truth (consumer layer)

At the `tofu/` consumer layer this provider reads its intent from **NetBox**, the
single source of truth — never from hand-maintained tfvars:

- **`netbox-wordpress`** — a `WordPressSite` object supplies the docroot, site
  URL, core version, plugin/theme set, and the cron cadence; a
  `WordPressContentDirectory` object supplies `content_dir` / `target_content_dir`
  / `uploads_dir` / `content_url` for `wordpress_content_dir`.
- **`netbox-database`** — `DatabaseUser.credential_ref` points at the OpenBao path
  holding `DB_PASSWORD`; the consumer reads it with an ephemeral vault resource
  and passes it to `wordpress_config.db_password`.

The consumer module maps NetBox objects → these resources; the provider itself is
source-of-truth-agnostic (it takes attributes).

## Usage

See [`examples/`](examples/): `provider.tf`, `core.tf`, `config.tf`,
`plugin_theme.tf`, `cron.tf`, and `content_dir_relocation.tf` (the headline safe
move).

## Development

```
export PATH=$PATH:/home/jameson/.local/go/bin
make check    # go mod tidy + gofmt + vet + test + build
```

`make check` is mirrored by `.githooks/pre-commit` (enable with
`git config core.hooksPath .githooks`). `--no-verify` is forbidden without
explicit authorization. Architecture and the relocation contract: see
[`DESIGN.md`](DESIGN.md).

## ⛔ No direct applies

This provider drives production WordPress hosts. **Never** apply by hand — declare
intent in prod-netbox and realize it only through prod-semaphore. See
[`CLAUDE.md`](CLAUDE.md).

## License

AGPL-3.0-or-later. Every source file carries an SPDX header.
