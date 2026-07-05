// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestCoreInstallArgs(t *testing.T) {
	m := coreModel{
		URL:        types.StringValue("https://example.com"),
		Title:      types.StringValue("My Site"),
		AdminUser:  types.StringValue("admin"),
		AdminEmail: types.StringValue("admin@example.com"),
	}
	got := componentJoin(coreInstallArgs(m, "s3cret"))
	for _, want := range []string{
		"core install",
		"--url=https://example.com",
		"--title=My Site",
		"--admin_user=admin",
		"--admin_email=admin@example.com",
		"--admin_password=s3cret",
		"--skip-email",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("coreInstallArgs missing %q in %q", want, got)
		}
	}
	// Omitted optional flags are absent.
	bare := componentJoin(coreInstallArgs(coreModel{}, ""))
	if strings.Contains(bare, "--url=") || strings.Contains(bare, "--admin_password=") {
		t.Errorf("empty model should omit optional flags: %q", bare)
	}
}

func TestCoreMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewCoreResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
	if resp.TypeName != "wordpress_core" {
		t.Fatalf("TypeName = %q, want wordpress_core", resp.TypeName)
	}
}

// componentJoin is a tiny test helper joining args with spaces.
func componentJoin(args []string) string { return strings.Join(args, " ") }
