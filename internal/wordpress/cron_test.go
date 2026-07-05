// SPDX-License-Identifier: AGPL-3.0-or-later

package wordpress

import (
	"strings"
	"testing"
)

func TestRenderCronEntry(t *testing.T) {
	got := RenderCronEntry("*/5", "www-data", "/usr/bin/php", "/var/www/html")
	want := cronManagedMarker + "\n*/5 * * * * www-data /usr/bin/php /var/www/html/wp-cron.php >/dev/null 2>&1\n"
	if got != want {
		t.Fatalf("RenderCronEntry =\n%q\nwant\n%q", got, want)
	}
	// A docroot with a trailing slash must not double the slash before wp-cron.php.
	got = RenderCronEntry("*/10", "web", "/usr/bin/php8.2", "/srv/wp/")
	if strings.Contains(got, "//wp-cron.php") {
		t.Fatalf("trailing slash produced a double slash: %q", got)
	}
}

func TestParseCronMinute(t *testing.T) {
	content := RenderCronEntry("*/5", "www-data", "/usr/bin/php", "/var/www/html")
	if got := ParseCronMinute(content); got != "*/5" {
		t.Fatalf("ParseCronMinute = %q, want */5", got)
	}
	if got := ParseCronMinute("# only a comment\n\n"); got != "" {
		t.Fatalf("comment-only content should yield empty minute, got %q", got)
	}
	if got := ParseCronMinute("15 3 * * * root /usr/bin/php /w/wp-cron.php\n"); got != "15" {
		t.Fatalf("ParseCronMinute numeric = %q, want 15", got)
	}
}
