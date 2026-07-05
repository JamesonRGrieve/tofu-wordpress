# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_cron — system cron entry driving wp-cron (pairs with DISABLE_WP_CRON=true).
# Import:  tofu import wordpress_cron.site /var/www/html

resource "wordpress_cron" "site" {
  path       = "/var/www/html"
  minute     = "*/5" # cron cadence
  php_binary = "/usr/bin/php"
  user       = "www-data"
}
