# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_muplugin — a must-use plugin file at wp-content/mu-plugins/<name>,
# managed by full content. Declarative home for the shim mu-plugins the
# integrate_*.yml playbooks deploy (Matomo, ntfy, Matrix, Tika, SearXNG, Frappe).
# Import:  tofu import wordpress_muplugin.matomo /var/www/html#zz-matomo.php

resource "wordpress_muplugin" "matomo" {
  path    = "/var/www/html"
  name    = "zz-matomo.php"
  content = file("${path.module}/files/zz-matomo.php")
}
