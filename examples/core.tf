# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_core — install/update WordPress core via WP-CLI.
# Import an existing install to 0-diff:  tofu import wordpress_core.site /var/www/html

variable "wp_admin_password" {
  description = "WordPress admin password. Injected from OpenBao at apply; write-only, never stored in state."
  type        = string
  sensitive   = true
}

resource "wordpress_core" "site" {
  path        = "/var/www/html"
  version     = "6.5.2"
  locale      = "en_US"
  url         = "https://site.example.com"
  title       = "Example Site"
  admin_user  = "admin"
  admin_email = "admin@example.com"

  # Write-only: read from config at apply, never persisted.
  admin_password = var.wp_admin_password
}
