# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_plugin / wordpress_theme — WP-CLI managed components.
# state: active | present_inactive | absent (absent uninstalls).
# Import:  tofu import wordpress_plugin.woocommerce /var/www/html/woocommerce

resource "wordpress_plugin" "woocommerce" {
  path    = "/var/www/html"
  slug    = "woocommerce"
  version = "9.4.0" # blank = latest
  state   = "active"
  source  = "wporg"
}

resource "wordpress_plugin" "redis_cache" {
  path  = "/var/www/html"
  slug  = "redis-cache"
  state = "active"
}

# Retire inherited cruft: state = "absent" uninstalls the plugin.
resource "wordpress_plugin" "w3_total_cache" {
  path  = "/var/www/html"
  slug  = "w3-total-cache"
  state = "absent"
}

# Install a plugin from a URL or an on-host zip (e.g. a premium/local build),
# present but not activated.
resource "wordpress_plugin" "custom" {
  path       = "/var/www/html"
  slug       = "acme-custom"
  source     = "url"
  source_url = "https://downloads.example.com/acme-custom.zip"
  state      = "present_inactive"
}

resource "wordpress_theme" "storefront" {
  path  = "/var/www/html"
  slug  = "storefront"
  state = "active"
}
