// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestMuPluginMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewMuPluginResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
	if resp.TypeName != "wordpress_muplugin" {
		t.Fatalf("TypeName = %q, want wordpress_muplugin", resp.TypeName)
	}
}

func TestMuPluginPaths(t *testing.T) {
	if got := muPluginDir("/var/www/html"); got != "/var/www/html/wp-content/mu-plugins" {
		t.Fatalf("muPluginDir = %q", got)
	}
	// Trailing slash on the docroot is normalized (no doubled slash).
	if got := muPluginDir("/var/www/html/"); got != "/var/www/html/wp-content/mu-plugins" {
		t.Fatalf("muPluginDir trailing slash = %q", got)
	}
	if got := muPluginFile("/var/www/html", "zz-matomo.php"); got != "/var/www/html/wp-content/mu-plugins/zz-matomo.php" {
		t.Fatalf("muPluginFile = %q", got)
	}
}

func TestParseMuPluginImportID(t *testing.T) {
	p, name := parseMuPluginImportID("/var/www/html#zz-matomo.php")
	if p != "/var/www/html" || name != "zz-matomo.php" {
		t.Fatalf("parseMuPluginImportID = (%q,%q)", p, name)
	}
	// Bare name → empty path (provider docroot used).
	p, name = parseMuPluginImportID("zz-ntfy.php")
	if p != "" || name != "zz-ntfy.php" {
		t.Fatalf("bare import = (%q,%q)", p, name)
	}
}
