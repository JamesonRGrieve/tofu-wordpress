# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_plugin / wordpress_theme — WP-CLI managed components.
# Import:  tofu import wordpress_plugin.woocommerce /var/www/html/woocommerce

resource "wordpress_plugin" "woocommerce" {
  path    = "/var/www/html"
  slug    = "woocommerce"
  version = "9.4.0" # blank = latest
  active  = true
  source  = "wporg"
}

resource "wordpress_plugin" "redis_cache" {
  path   = "/var/www/html"
  slug   = "redis-cache"
  active = true
}

# Install a plugin from a URL or an on-host zip (e.g. a premium/local build).
resource "wordpress_plugin" "custom" {
  path       = "/var/www/html"
  slug       = "acme-custom"
  source     = "url"
  source_url = "https://downloads.example.com/acme-custom.zip"
  active     = false
}

resource "wordpress_theme" "storefront" {
  path   = "/var/www/html"
  slug   = "storefront"
  active = true
}
