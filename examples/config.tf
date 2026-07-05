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

  db_password = var.wp_db_password
  salts       = var.wp_salts
}
