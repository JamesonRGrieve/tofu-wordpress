// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestContentDirMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewContentDirResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
	if resp.TypeName != "wordpress_content_dir" {
		t.Fatalf("TypeName = %q, want wordpress_content_dir", resp.TypeName)
	}
}

func TestContentDirResourceType(t *testing.T) {
	if _, ok := NewContentDirResource().(*contentDirResource); !ok {
		t.Fatalf("NewContentDirResource should build *contentDirResource, got %T", NewContentDirResource())
	}
}
