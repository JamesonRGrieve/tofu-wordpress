// SPDX-License-Identifier: AGPL-3.0-or-later

package provider

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/JamesonRGrieve/tofu-wordpress/internal/wordpress"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// funcExec is a hermetic wordpress.Executor for provider-layer tests: it records
// every command and delegates to an optional per-command func. No device.
type funcExec struct {
	calls []string
	run   func(cmd string) (string, error)
}

func (f *funcExec) Run(cmd string, _ []byte) ([]byte, error) {
	f.calls = append(f.calls, cmd)
	if f.run != nil {
		out, err := f.run(cmd)
		return []byte(out), err
	}
	return nil, nil
}

func (f *funcExec) joined() string { return strings.Join(f.calls, "\n") }

func TestComponentConverge(t *testing.T) {
	wp := func(f *funcExec) *wordpress.WPCLI { return wordpress.NewWPCLI(f, "/var/www/html") }
	model := func(state string) *componentModel {
		return &componentModel{
			Slug:   types.StringValue("woocommerce"),
			State:  types.StringValue(state),
			Source: types.StringValue("wporg"),
		}
	}

	t.Run("active plugin installs and activates", func(t *testing.T) {
		f := &funcExec{}
		if err := (&componentResource{kind: "plugin"}).converge(wp(f), model(stateActive)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(f.joined(), "install") || !strings.Contains(f.joined(), "activate") {
			t.Fatalf("active should install+activate; calls:\n%s", f.joined())
		}
	})

	t.Run("present_inactive plugin deactivates", func(t *testing.T) {
		f := &funcExec{}
		if err := (&componentResource{kind: "plugin"}).converge(wp(f), model(statePresentInactive)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(f.joined(), "deactivate") {
			t.Fatalf("present_inactive plugin should deactivate; calls:\n%s", f.joined())
		}
	})

	t.Run("present_inactive theme does not deactivate", func(t *testing.T) {
		f := &funcExec{}
		if err := (&componentResource{kind: "theme"}).converge(wp(f), model(statePresentInactive)); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(f.joined(), "deactivate") {
			t.Fatalf("theme has no deactivate verb; calls:\n%s", f.joined())
		}
	})

	t.Run("absent installed deletes", func(t *testing.T) {
		f := &funcExec{} // all commands succeed → is-installed ok → delete
		if err := (&componentResource{kind: "plugin"}).converge(wp(f), model(stateAbsent)); err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(f.joined(), "delete") {
			t.Fatalf("absent+installed should delete; calls:\n%s", f.joined())
		}
	})

	t.Run("absent not-installed is a no-op", func(t *testing.T) {
		f := &funcExec{run: func(cmd string) (string, error) {
			if strings.Contains(cmd, "is-installed") {
				return "", errors.New("not installed")
			}
			return "", nil
		}}
		if err := (&componentResource{kind: "plugin"}).converge(wp(f), model(stateAbsent)); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(f.joined(), "delete") {
			t.Fatalf("absent+not-installed must not delete; calls:\n%s", f.joined())
		}
	})
}

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

func TestComponentStatusState(t *testing.T) {
	for _, s := range []string{"active", "active-network", " active "} {
		if got := componentStatusState(s); got != stateActive {
			t.Errorf("%q → %q, want %q", s, got, stateActive)
		}
	}
	for _, s := range []string{"inactive", "", "must-use"} {
		if got := componentStatusState(s); got != statePresentInactive {
			t.Errorf("%q → %q, want %q", s, got, statePresentInactive)
		}
	}
}

func TestValidComponentState(t *testing.T) {
	for _, s := range []string{stateActive, statePresentInactive, stateAbsent} {
		if !validComponentState(s) {
			t.Errorf("%q should be valid", s)
		}
	}
	for _, s := range []string{"", "enabled", "removed", "ACTIVE"} {
		if validComponentState(s) {
			t.Errorf("%q should be invalid", s)
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
