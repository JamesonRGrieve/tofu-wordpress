# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_cron — system cron entry driving wp-cron (pairs with DISABLE_WP_CRON=true).
# mode: wp_cron_php (default, runs wp-cron.php via PHP) | wp_cli (runs
#       `wp cron event run --due-now` — the live 301–321 fleet's form).
# Import:  tofu import wordpress_cron.site /var/www/html

resource "wordpress_cron" "site" {
  path       = "/var/www/html"
  minute     = "*/5" # cron cadence
  mode       = "wp_cron_php"
  php_binary = "/usr/bin/php"
  user       = "www-data"
}

# The live-fleet form: reconcile an imported fleet host to 0-diff.
resource "wordpress_cron" "fleet_site" {
  path      = "/var/www/html/wordpress"
  minute    = "*/5"
  mode      = "wp_cli"
  wp_binary = "/usr/bin/wp" # CT 322 uses /usr/local/bin/wp
  user      = "www-data"
}
