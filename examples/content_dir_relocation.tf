# SPDX-License-Identifier: AGPL-3.0-or-later
# wordpress_content_dir — SAFE on-disk relocation of wp-content (the headline
# feature). Setting target_content_dir different from the current content_dir
# triggers a staged, verified, health-checked, rollback-armed move:
#
#   1. maintenance-mode activate    (quiesce writes)
#   2. rsync -aH --checksum + VERIFY (file-count check; never a naive mv)
#   3. wp config set WP_CONTENT_DIR / WP_CONTENT_URL + wp option update upload_path
#   4. leave old -> new symlink      (keep_symlink; original retained, renamed)
#   5. HEALTH CHECK: siteurl returns HTTP 200
#   6. on ANY failure: roll back the config defines + restore the original dir
#   7. maintenance-mode deactivate
#
# Re-applying after a completed relocation is a zero-diff no-op (content_dir is
# computed to the target).
#
# Import an already-relocated install to 0-diff:
#   tofu import wordpress_content_dir.site /var/www/html

resource "wordpress_content_dir" "site" {
  path               = "/var/www/html"
  content_dir        = "/var/www/html/wp-content" # current location
  target_content_dir = "/mnt/data/wp-content"     # desired location -> triggers the move
  uploads_dir        = "/mnt/data/wp-content/uploads"
  content_url        = "https://site.example.com/wp-content"
  keep_symlink       = true
}
