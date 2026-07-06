# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_config — manage wp-config.php defines (manage-declared-only).
# Import:  tofu import wordpress_config.site /var/www/html

variable "wp_db_password" {
  description = "DB_PASSWORD. Injected from OpenBao; write-only, never stored in state."
  type        = string
  sensitive   = true
}

variable "wp_salts" {
  description = "AUTH salts map. Injected from OpenBao; write-only, never stored in state."
  type        = map(string)
  sensitive   = true
}

resource "wordpress_config" "site" {
  path         = "/var/www/html"
  table_prefix = "wp_"
  db_name      = "wordpress"
  db_user      = "wordpress"
  db_host      = "localhost"

  # Only the declared constants are managed; unmanaged defines are never touched.
  constants = {
    WP_DEBUG            = "false"
    WP_MEMORY_LIMIT     = "256M"
    DISABLE_WP_CRON     = "true" # paired with wordpress_cron below
    DISALLOW_FILE_EDIT  = "true"
    WP_AUTO_UPDATE_CORE = "minor"
    FORCE_SSL_ADMIN     = "true"
    # Object cache (Valkey/Redis):
    WP_REDIS_HOST            = "127.0.0.1"
    WP_REDIS_PORT            = "6379"
    WP_REDIS_DISABLE_COMMENT = "true"
  }

  # WordPressSite hardening / tuning surface (netbox-wordpress fields).
  # Hidden login (wps-hide-login): installs the plugin and sets whl_page.
  login_slug = "secret-login"

  # Object cache (Valkey/Redis) instance: defines WP_REDIS_HOST/PORT, installs +
  # activates redis-cache, and runs `wp redis enable`.
  object_cache_host = "10.0.0.5"
  object_cache_port = 6379

  # PHP safe-opt hardening: writes a php.ini drop-in (disable_functions,
  # allow_url_fopen = Off). Set false to remove the managed drop-in.
  safe_opt = true

  # Apache: mod_remoteip trusted proxies + HSTS header (shared managed drop-in).
  trusted_proxies = "10.0.0.1,192.168.1.0/24"
  enable_hsts     = true

  db_password = var.wp_db_password
  salts       = var.wp_salts
}
