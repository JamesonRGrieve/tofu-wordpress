// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/wordpress"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestOptionMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewOptionResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
	if resp.TypeName != "wordpress_option" {
		t.Fatalf("TypeName = %q, want wordpress_option", resp.TypeName)
	}
}

func TestOptionIDAndImport(t *testing.T) {
	if got := optionID("/var/www/html", "whl_page"); got != "/var/www/html#whl_page" {
		t.Fatalf("optionID = %q", got)
	}
	// Trailing slash on the path is normalized.
	if got := optionID("/var/www/html/", "whl_page"); got != "/var/www/html#whl_page" {
		t.Fatalf("optionID trailing slash = %q", got)
	}
	p, name := parseOptionImportID("/var/www/html#whl_page")
	if p != "/var/www/html" || name != "whl_page" {
		t.Fatalf("parseOptionImportID = (%q,%q)", p, name)
	}
	// Bare name → empty path (provider docroot used).
	p, name = parseOptionImportID("whl_page")
	if p != "" || name != "whl_page" {
		t.Fatalf("bare import = (%q,%q)", p, name)
	}
}

// TestOptionWriteCommand verifies the update runs `wp option update <name>
// <value>` through the injected executor (hermetic; no device). funcExec is
// defined in plugin_resource_test.go (same package).
func TestOptionWriteCommand(t *testing.T) {
	f := &funcExec{}
	wp := wordpress.NewWPCLI(f, "/var/www/html")
	if _, err := wp.Command("option", "update", "whl_page", "KzfYwpdC"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(f.calls, "\n")
	if !strings.Contains(joined, "option") || !strings.Contains(joined, "update") ||
		!strings.Contains(joined, "whl_page") || !strings.Contains(joined, "KzfYwpdC") {
		t.Fatalf("expected `option update whl_page KzfYwpdC`; got:\n%s", joined)
	}
}

func TestValidOptionFormat(t *testing.T) {
	for _, s := range []string{optionFormatString, optionFormatJSON} {
		if !validOptionFormat(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "yaml", "JSON", "serialized"} {
		if validOptionFormat(s) {
			t.Errorf("%q should be invalid", s)
		}
	}
}

func TestOptionUpdateArgs(t *testing.T) {
	got := strings.Join(optionUpdateArgs("whl_page", "KzfYwpdC", optionFormatString), " ")
	if got != "option update whl_page KzfYwpdC" {
		t.Fatalf("string args = %q", got)
	}
	got = strings.Join(optionUpdateArgs("mwai_options", `{"a":1}`, optionFormatJSON), " ")
	if got != `option update mwai_options {"a":1} --format=json` {
		t.Fatalf("json args = %q", got)
	}
}

func TestCanonicalizeJSON(t *testing.T) {
	// Key order is normalized (sorted) and whitespace stripped, so WP's
	// insertion-ordered read-back matches a jsonencode() config value.
	got, err := canonicalizeJSON(`{ "b": 2, "a": 1 }`)
	if err != nil {
		t.Fatal(err)
	}
	if got != `{"a":1,"b":2}` {
		t.Fatalf("canonicalizeJSON = %q, want {\"a\":1,\"b\":2}", got)
	}
	// Arrays preserve order.
	if got, _ := canonicalizeJSON(`["x","y"]`); got != `["x","y"]` {
		t.Fatalf("array canonicalize = %q", got)
	}
	if _, err := canonicalizeJSON(`not json`); err == nil {
		t.Fatalf("expected error on invalid JSON")
	}
}
