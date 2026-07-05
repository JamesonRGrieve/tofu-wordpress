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

// RenderCronEntry renders a /etc/cron.d file body that runs wp-cron.php on the
// given minute spec as the given user. The cron.d layout carries the user field
// between the schedule and the command (unlike a user crontab).
func RenderCronEntry(minute, user, phpBinary, docroot string) string {
	cmd := fmt.Sprintf("%s %s/wp-cron.php >/dev/null 2>&1", phpBinary, strings.TrimRight(docroot, "/"))
	return fmt.Sprintf("%s\n%s * * * * %s %s\n", cronManagedMarker, minute, user, cmd)
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
