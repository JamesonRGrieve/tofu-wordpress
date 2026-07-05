// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestThemeResourceKind(t *testing.T) {
	cr, ok := NewThemeResource().(*componentResource)
	if !ok {
		t.Fatalf("NewThemeResource should build a *componentResource, got %T", NewThemeResource())
	}
	if cr.kind != "theme" {
		t.Fatalf("theme resource kind = %q, want theme", cr.kind)
	}
}

func TestThemeMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewThemeResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
	if resp.TypeName != "wordpress_theme" {
		t.Fatalf("TypeName = %q, want wordpress_theme", resp.TypeName)
	}
}
