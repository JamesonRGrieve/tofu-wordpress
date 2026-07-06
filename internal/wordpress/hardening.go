// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Pure renderers for the per-site production surface that netbox-wordpress's
// WordPressSite models beyond the wp-config defines: the PHP safe-opt ini
// drop-in (safe_opt), the Apache mod_remoteip trusted-proxy stanza
// (trusted_proxies), the HSTS header block (enable_hsts), the object-cache
// (Redis/Valkey) wp-config defines (object_cache_instance), and the
// wps-hide-login slug option (login_slug). Everything here is I/O-free and
// unit-tested; the wordpress_config resource writes the rendered output to the
// host through the injected transport and NEVER touches a device in a unit test.
package wordpress

import (
	"fmt"
	"strconv"
	"strings"
)

// Marker headers so an operator (and a future import) can tell a drop-in is
// owned by tofu-wordpress. Apache/PHP-ini comment syntaxes differ (`#` vs `;`).
const (
	apacheManagedMarker = "# Managed by OpenTofu (tofu-wordpress) — do not edit manually"
	iniManagedMarker    = "; Managed by OpenTofu (tofu-wordpress) — do not edit manually"
)

// hstsMaxAgeSeconds is the Strict-Transport-Security max-age (one year) emitted
// when a site opts into enable_hsts.
const hstsMaxAgeSeconds = 31536000

// phpHardeningDisableFunctions is the process-exec / source-disclosure family
// disabled by safe_opt — functions a hardened WordPress install never needs.
var phpHardeningDisableFunctions = []string{
	"exec", "passthru", "shell_exec", "system", "proc_open", "popen",
	"parse_ini_file", "show_source",
}

// RenderPHPHardeningINI renders the php.ini drop-in for safe_opt: disable the
// process-exec functions and turn allow_url_fopen off. Pure — unit-tested.
func RenderPHPHardeningINI() string {
	var b strings.Builder
	b.WriteString(iniManagedMarker + "\n")
	b.WriteString("disable_functions = " + strings.Join(phpHardeningDisableFunctions, ",") + "\n")
	b.WriteString("allow_url_fopen = Off\n")
	return b.String()
}

// RenderRemoteIPBlock renders the Apache mod_remoteip stanza trusting the given
// comma-separated proxy IPs/CIDRs (X-Forwarded-For). Blank/all-empty input
// yields "" (nothing to manage). Pure.
func RenderRemoteIPBlock(trustedProxies string) string {
	cidrs := splitCSV(trustedProxies)
	if len(cidrs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("<IfModule mod_remoteip.c>\n")
	b.WriteString("    RemoteIPHeader X-Forwarded-For\n")
	for _, c := range cidrs {
		b.WriteString("    RemoteIPTrustedProxy " + c + "\n")
	}
	b.WriteString("</IfModule>\n")
	return b.String()
}

// RenderHSTSBlock renders the Apache stanza that always emits the
// Strict-Transport-Security header. Pure.
func RenderHSTSBlock() string {
	return "<IfModule mod_headers.c>\n" +
		fmt.Sprintf("    Header always set Strict-Transport-Security \"max-age=%d; includeSubDomains\"\n", hstsMaxAgeSeconds) +
		"</IfModule>\n"
}

// RenderApacheHardeningConf composes the managed Apache drop-in from the
// trusted-proxy stanza (trusted_proxies) and the HSTS stanza (enable_hsts).
// Returns "" when neither contributes anything, signalling the caller to remove
// the managed file. Pure.
func RenderApacheHardeningConf(trustedProxies string, enableHSTS bool) string {
	var blocks []string
	if b := RenderRemoteIPBlock(trustedProxies); b != "" {
		blocks = append(blocks, b)
	}
	if enableHSTS {
		blocks = append(blocks, RenderHSTSBlock())
	}
	if len(blocks) == 0 {
		return ""
	}
	return apacheManagedMarker + "\n" + strings.Join(blocks, "\n")
}

// RedisCacheDefines returns the WP_REDIS_* wp-config constants that target the
// object-cache instance. A blank host or zero port omits its constant, so a
// partially-declared cache yields only the declared defines. WP_REDIS_HOST is a
// string constant, WP_REDIS_PORT is raw (see IsRawConstant). Pure.
func RedisCacheDefines(host string, port int64) map[string]string {
	defs := map[string]string{}
	if strings.TrimSpace(host) != "" {
		defs["WP_REDIS_HOST"] = strings.TrimSpace(host)
	}
	if port != 0 {
		defs["WP_REDIS_PORT"] = strconv.FormatInt(port, 10)
	}
	return defs
}

// LoginSlugOptionArgs renders the WP-CLI arguments that set the wps-hide-login
// hidden-login slug (stored in the `whl_page` option). Pure.
func LoginSlugOptionArgs(slug string) []string {
	return []string{"option", "update", "whl_page", slug}
}

// WriteFileCommand renders a remote command that writes stdin to path (creating
// the parent directory first) — used to lay down a managed drop-in over the
// transport. The content is piped as stdin (never shell-interpolated).
func WriteFileCommand(path string) string {
	return "mkdir -p " + shQuote(dirOf(path)) + " && tee " + shQuote(path) + " >/dev/null"
}

// RemoveFileCommand renders a remote command that removes a managed drop-in
// (idempotent — `rm -f` on an absent file succeeds).
func RemoveFileCommand(path string) string {
	return "rm -f " + shQuote(path)
}

// dirOf returns the parent directory of an absolute POSIX path.
func dirOf(p string) string {
	if i := strings.LastIndex(p, "/"); i > 0 {
		return p[:i]
	}
	return "/"
}

// splitCSV splits a comma-separated string, trimming whitespace and dropping
// empty fields (deterministic order preserved from the input).
func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out = append(out, p)
		}
	}
	return out
}
