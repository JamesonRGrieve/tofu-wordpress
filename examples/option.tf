# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_option — a single wp_options row via `wp option update/get/delete`.
# The consumer layer maps netbox-wordpress WordPressSite.login_slug to the
# wps-hide-login `whl_page` option here, so the login health check reads one SoT.
# Import:  tofu import wordpress_option.login_slug /var/www/html#whl_page

resource "wordpress_option" "login_slug" {
  path  = "/var/www/html"
  name  = "whl_page"
  value = "KzfYwpdC"
}

# format = "json" manages a serialized plugin-settings array (the shape the
# integrate_*.yml playbooks write with `wp option update --format=json`).
# ALWAYS emit the value with jsonencode() so it is canonical (sorted keys,
# compact) and matches the canonicalized device read-back → 0-diff. Non-secret
# values come from netbox-services IntegrationParam; secrets stay in OpenBao.
resource "wordpress_option" "ai_engine_settings" {
  path   = "/var/www/html"
  name   = "mwai_options"
  format = "json"
  value = jsonencode({
    ai_default_env    = "ollama"
    ai_envs           = [{ type = "openai", name = "Ollama", apikey = "-", endpoint = "http://ollama:11434/v1" }]
    module_assistants = true
  })
}
