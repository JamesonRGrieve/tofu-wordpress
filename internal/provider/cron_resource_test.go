// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestSanitizeForFilename(t *testing.T) {
	cases := map[string]string{
		"/var/www/html": "var-www-html",
		"/srv/wp":       "srv-wp",
		"/var/www/a.b":  "var-www-a-b",
		"/":             "root",
		"":              "root",
	}
	for in, want := range cases {
		if got := sanitizeForFilename(in); got != want {
			t.Errorf("sanitizeForFilename(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCronFilePath(t *testing.T) {
	got := cronFilePath("/var/www/html")
	want := "/etc/cron.d/wordpress-cron-var-www-html"
	if got != want {
		t.Fatalf("cronFilePath = %q, want %q", got, want)
	}
	// cron.d filenames must not contain dots or slashes in the name component.
	name := strings.TrimPrefix(cronFilePath("/srv/wp.d"), "/etc/cron.d/")
	if strings.ContainsAny(name, "./") {
		t.Fatalf("cron.d filename component must not contain dots or slashes: %q", name)
	}
}

func TestShellQuote(t *testing.T) {
	if got := shellQuote("/etc/cron.d/x"); got != "'/etc/cron.d/x'" {
		t.Errorf("shellQuote plain = %q", got)
	}
	if got := shellQuote("a'b"); got != `'a'\''b'` {
		t.Errorf("shellQuote with quote = %q", got)
	}
}

func TestCronMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewCronResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
	if resp.TypeName != "wordpress_cron" {
		t.Fatalf("TypeName = %q, want wordpress_cron", resp.TypeName)
	}
}
