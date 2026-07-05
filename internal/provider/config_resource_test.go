// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"reflect"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestSortedKeys(t *testing.T) {
	in := map[string]string{"WP_DEBUG": "false", "DISABLE_WP_CRON": "true", "WP_MEMORY_LIMIT": "256M"}
	got := sortedKeys(in)
	want := []string{"DISABLE_WP_CRON", "WP_DEBUG", "WP_MEMORY_LIMIT"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("sortedKeys = %v, want %v", got, want)
	}
	if got := sortedKeys(map[string]string{}); len(got) != 0 {
		t.Fatalf("empty map should yield no keys, got %v", got)
	}
}

func TestConfigMetadata(t *testing.T) {
	resp := &resource.MetadataResponse{}
	NewConfigResource().Metadata(context.Background(),
		resource.MetadataRequest{ProviderTypeName: "wordpress"}, resp)
	if resp.TypeName != "wordpress_config" {
		t.Fatalf("TypeName = %q, want wordpress_config", resp.TypeName)
	}
}
