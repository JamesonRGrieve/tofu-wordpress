// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestComponentInstallTarget(t *testing.T) {
	if got := componentInstallTarget("wporg", "woocommerce", ""); got != "woocommerce" {
		t.Errorf("wporg source should install by slug, got %q", got)
	}
	if got := componentInstallTarget("url", "woocommerce", "https://x/wc.zip"); got != "https://x/wc.zip" {
		t.Errorf("url source should install from source_url, got %q", got)
	}
	if got := componentInstallTarget("zip", "wc", "/tmp/wc.zip"); got != "/tmp/wc.zip" {
		t.Errorf("zip source should install from path, got %q", got)
	}
	// url source but no url → fall back to slug.
	if got := componentInstallTarget("url", "wc", ""); got != "wc" {
		t.Errorf("url source without url should fall back to slug, got %q", got)
	}
}

func TestComponentInstallArgs(t *testing.T) {
	got := strings.Join(componentInstallArgs("plugin", "woocommerce", "9.4.0", true), " ")
	want := "plugin install woocommerce --version=9.4.0 --activate"
	if got != want {
		t.Fatalf("componentInstallArgs = %q, want %q", got, want)
	}
	// No version, not active.
	got = strings.Join(componentInstallArgs("theme", "twentytwentyfour", "", false), " ")
	if got != "theme install twentytwentyfour" {
		t.Fatalf("componentInstallArgs bare = %q", got)
	}
}

func TestComponentStatusActive(t *testing.T) {
	for _, s := range []string{"active", "active-network", " active "} {
		if !componentStatusActive(s) {
			t.Errorf("%q should be active", s)
		}
	}
	for _, s := range []string{"inactive", "", "must-use"} {
		if componentStatusActive(s) {
			t.Errorf("%q should not be active", s)
		}
	}
}

func TestComponentIDAndImport(t *testing.T) {
	if got := componentID("/var/www/html", "akismet"); got != "/var/www/html/akismet" {
		t.Fatalf("componentID = %q", got)
	}
	p, slug := parseComponentImportID("/var/www/html/akismet")
	if p != "/var/www/html" || slug != "akismet" {
		t.Fatalf("parseComponentImportID = (%q,%q)", p, slug)
	}
	// Bare slug → empty path (provider docroot used).
	p, slug = parseComponentImportID("akismet")
	if p != "" || slug != "akismet" {
		t.Fatalf("bare import = (%q,%q)", p, slug)
	}
}

func TestPluginMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewPluginResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
	if resp.TypeName != "wordpress_plugin" {
		t.Fatalf("TypeName = %q, want wordpress_plugin", resp.TypeName)
	}
}
