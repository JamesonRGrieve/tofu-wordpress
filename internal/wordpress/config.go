// SPDX-License-Identifier: AGPL-3.0-or-later
//
// wp-config.php define rendering helpers. WordPress constants are typed in PHP:
// some are booleans/ints written unquoted (raw), others are string literals.
// These pure helpers classify a constant and normalize its value so the
// wordpress_config resource can issue the correct `wp config set` for each.
package wordpress

import "strings"

// rawConstants are the wp-config.php constants whose values are PHP booleans or
// integers (written unquoted via `wp config set --raw`). Everything else is a
// string literal. Sourced from the production wp-config.php.j2 template.
var rawConstants = map[string]bool{
	"WP_DEBUG":                 true,
	"WP_DEBUG_LOG":             true,
	"WP_DEBUG_DISPLAY":         true,
	"DISALLOW_FILE_EDIT":       true,
	"DISALLOW_FILE_MODS":       true,
	"DISABLE_WP_CRON":          true,
	"FORCE_SSL_ADMIN":          true,
	"WP_POST_REVISIONS":        true,
	"EMPTY_TRASH_DAYS":         true,
	"WP_REDIS_PORT":            true,
	"WP_REDIS_DATABASE":        true,
	"WP_REDIS_MAXTTL":          true,
	"WP_REDIS_DISABLE_COMMENT": true,
	"WP_REDIS_DISABLE_BANNERS": true,
	"WP_CACHE":                 true,
	"WP_ALLOW_MULTISITE":       true,
	"MULTISITE":                true,
	"SUBDOMAIN_INSTALL":        true,
}

// IsRawConstant reports whether a wp-config.php constant is written unquoted
// (a PHP boolean/int) rather than as a string literal.
func IsRawConstant(name string) bool {
	return rawConstants[name]
}

// NormalizeConstantValue canonicalizes a constant's value for drift comparison
// so that state (what `wp config get` returns) and config match. `wp config get`
// returns booleans as "1"/"" and ints as their decimal form; a raw boolean
// declared as "true"/"false" is normalized to "1"/"" to match readback.
func NormalizeConstantValue(name, value string) string {
	if !IsRawConstant(name) {
		return value
	}
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true":
		return "1"
	case "false":
		return ""
	default:
		return strings.TrimSpace(value)
	}
}

// ReconcileConstant implements manage-declared-only diff for a single constant:
// given the declared value and the value read back from the device, it returns
// the value to store in state. When the two are semantically equal it keeps the
// declared form (so no spurious diff, e.g. declared "false" vs read-back ""),
// otherwise it surfaces the device value so drift is visible.
func ReconcileConstant(name, declared, live string) string {
	if NormalizeConstantValue(name, declared) == NormalizeConstantValue(name, live) {
		return declared
	}
	return live
}
