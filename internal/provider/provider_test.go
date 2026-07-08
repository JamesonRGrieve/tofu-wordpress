// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestProviderMetadata(t *testing.T) {
	p := New("1.2.3")()
	resp := &provider.MetadataResponse{}
	p.Metadata(context.Background(), provider.MetadataRequest{}, resp)
	if resp.TypeName != "wordpress" {
		t.Fatalf("TypeName = %q, want wordpress", resp.TypeName)
	}
	if resp.Version != "1.2.3" {
		t.Fatalf("Version = %q, want 1.2.3", resp.Version)
	}
}

func TestProviderResources(t *testing.T) {
	p := New("dev")().(*wordpressProvider)
	got := p.Resources(context.Background())
	if len(got) != 8 {
		t.Fatalf("expected 8 resources, got %d", len(got))
	}
	// Every registered resource must produce a distinct wordpress_* type name.
	seen := map[string]bool{}
	for _, factory := range got {
		resp := &resource.MetadataResponse{}
		factory().Metadata(context.Background(),
			resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
		if !strings.HasPrefix(resp.TypeName, "wordpress_") {
			t.Errorf("resource type %q missing wordpress_ prefix", resp.TypeName)
		}
		if seen[resp.TypeName] {
			t.Errorf("duplicate resource type %q", resp.TypeName)
		}
		seen[resp.TypeName] = true
	}
}
