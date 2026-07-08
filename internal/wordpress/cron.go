// SPDX-License-Identifier: AGPL-3.0-or-later
//
// System-cron rendering for wp-cron. The production deployment sets
// DISABLE_WP_CRON=true in wp-config.php and drives WordPress's scheduler from a
// system cron entry on a fixed cadence instead of firing it on every page load.
// These pure helpers render and parse a /etc/cron.d entry that invokes
// wp-cron.php via the PHP binary. Unit-tested.
package wordpress

import (
	"fmt"
	"strings"
)

const cronManagedMarker = "# Managed by OpenTofu — do not edit manually"

// Cron command form. The two deployments differ: WPCronPHP invokes wp-cron.php
// via PHP directly (the greenfield default); WPCLI runs `wp cron event run
// --due-now` (the live 301–321 fleet's form). Modeling both lets the provider
// import a fleet host to 0-diff instead of showing a spurious command diff.
const (
	CronModeWPCronPHP = "wp_cron_php"
	CronModeWPCLI     = "wp_cli"
)

// RenderCronEntry renders a /etc/cron.d file body that drives wp-cron on the
// given minute spec as the given user, in the selected command form. The cron.d
// layout carries the user field between the schedule and the command (unlike a
// user crontab). wpBinary is only used by the WPCLI form.
func RenderCronEntry(minute, user, phpBinary, wpBinary, docroot, mode string) string {
	root := strings.TrimRight(docroot, "/")
	var cmd string
	if mode == CronModeWPCLI {
		cmd = fmt.Sprintf("%s --path=%s cron event run --due-now >/dev/null 2>&1", wpBinary, root)
	} else {
		cmd = fmt.Sprintf("%s %s/wp-cron.php >/dev/null 2>&1", phpBinary, root)
	}
	return fmt.Sprintf("%s\n%s * * * * %s %s\n", cronManagedMarker, minute, user, cmd)
}

// ParseCronMode detects the command form of a rendered cron entry so an imported
// host reconciles to 0-diff. Returns "" when no recognizable command is present.
func ParseCronMode(content string) string {
	switch {
	case strings.Contains(content, "cron event run"):
		return CronModeWPCLI
	case strings.Contains(content, "wp-cron.php"):
		return CronModeWPCronPHP
	default:
		return ""
	}
}

// ParseCronMinute extracts the minute field from a rendered cron.d entry,
// skipping comment and blank lines. Returns "" when no schedule line is present.
func ParseCronMinute(content string) string {
	for _, line := range strings.Split(content, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 5 {
			return fields[0]
		}
	}
	return ""
}
