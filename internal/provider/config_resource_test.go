// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/wordpress"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
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

// baseSiteModel is an all-null configModel with the drop-in paths set (mirroring
// their Computed defaults) so per-case tests toggle just the field under test.
func baseSiteModel() *configModel {
	return &configModel{
		LoginSlug:       types.StringNull(),
		ObjectCacheHost: types.StringNull(),
		ObjectCachePort: types.Int64Null(),
		SafeOpt:         types.BoolNull(),
		TrustedProxies:  types.StringNull(),
		EnableHSTS:      types.BoolNull(),
		PHPIniPath:      types.StringValue(defaultPHPHardeningINIPath),
		ApacheConfPath:  types.StringValue(defaultApacheHardeningPath),
	}
}

func TestApplySiteConfig(t *testing.T) {
	run := func(m *configModel) (string, error) {
		f := &funcExec{}
		wp := wordpress.NewWPCLI(f, "/var/www/html")
		err := applySiteConfig(m, wp, f)
		return f.joined(), err
	}

	t.Run("all-null is a no-op", func(t *testing.T) {
		got, err := run(baseSiteModel())
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Fatalf("null model should issue no commands; got:\n%s", got)
		}
	})

	t.Run("object cache sets defines, installs plugin, enables", func(t *testing.T) {
		m := baseSiteModel()
		m.ObjectCacheHost = types.StringValue("10.0.0.5")
		m.ObjectCachePort = types.Int64Value(6379)
		got, err := run(m)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"WP_REDIS_HOST", "10.0.0.5", "WP_REDIS_PORT", "--raw",
			"plugin' 'install' 'redis-cache' '--activate", "redis' 'enable"} {
			if !strings.Contains(got, want) {
				t.Errorf("object cache flow missing %q; got:\n%s", want, got)
			}
		}
	})

	t.Run("login slug installs wps-hide-login and sets whl_page", func(t *testing.T) {
		m := baseSiteModel()
		m.LoginSlug = types.StringValue("secret-login")
		got, err := run(m)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"wps-hide-login' '--activate", "option' 'update' 'whl_page' 'secret-login"} {
			if !strings.Contains(got, want) {
				t.Errorf("login-slug flow missing %q; got:\n%s", want, got)
			}
		}
	})

	t.Run("safe_opt true writes php ini, false removes it", func(t *testing.T) {
		m := baseSiteModel()
		m.SafeOpt = types.BoolValue(true)
		got, err := run(m)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "tee '"+defaultPHPHardeningINIPath+"'") {
			t.Errorf("safe_opt=true should write the ini drop-in; got:\n%s", got)
		}
		m.SafeOpt = types.BoolValue(false)
		got, err = run(m)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "rm -f '"+defaultPHPHardeningINIPath+"'") {
			t.Errorf("safe_opt=false should remove the ini drop-in; got:\n%s", got)
		}
	})

	t.Run("trusted_proxies and hsts write the apache conf", func(t *testing.T) {
		m := baseSiteModel()
		m.TrustedProxies = types.StringValue("10.0.0.0/8")
		m.EnableHSTS = types.BoolValue(true)
		got, err := run(m)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "tee '"+defaultApacheHardeningPath+"'") {
			t.Errorf("apache hardening should be written; got:\n%s", got)
		}
	})

	t.Run("declared-but-empty apache config removes the managed conf", func(t *testing.T) {
		m := baseSiteModel()
		m.TrustedProxies = types.StringValue("")
		m.EnableHSTS = types.BoolValue(false)
		got, err := run(m)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(got, "rm -f '"+defaultApacheHardeningPath+"'") {
			t.Errorf("empty apache config should remove the drop-in; got:\n%s", got)
		}
	})
}
