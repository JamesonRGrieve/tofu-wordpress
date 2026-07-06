// SPDX-License-Identifier: AGPL-3.0-or-later

package wordpress

import (
	"reflect"
	"strings"
	"testing"
)

func TestRenderPHPHardeningINI(t *testing.T) {
	got := RenderPHPHardeningINI()
	for _, want := range []string{iniManagedMarker, "disable_functions = ", "shell_exec", "allow_url_fopen = Off"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderPHPHardeningINI missing %q; got:\n%s", want, got)
		}
	}
	// disable_functions must be a single comma-joined line (no spaces around commas).
	if strings.Contains(got, ", ") {
		t.Errorf("disable_functions list should be comma-joined without spaces; got:\n%s", got)
	}
}

func TestRenderRemoteIPBlock(t *testing.T) {
	cases := []struct {
		name, proxies string
		wantEmpty     bool
		wantContains  []string
	}{
		{"blank", "", true, nil},
		{"whitespace only", "  ,  ", true, nil},
		{"single cidr", "10.0.0.0/24", false, []string{"RemoteIPHeader X-Forwarded-For", "RemoteIPTrustedProxy 10.0.0.0/24"}},
		{"multi trimmed", " 10.0.0.1 , 192.168.1.0/24 ", false, []string{
			"RemoteIPTrustedProxy 10.0.0.1", "RemoteIPTrustedProxy 192.168.1.0/24"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RenderRemoteIPBlock(c.proxies)
			if c.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty, got %q", got)
				}
				return
			}
			if !strings.HasPrefix(got, "<IfModule mod_remoteip.c>") {
				t.Errorf("block should open the mod_remoteip guard; got:\n%s", got)
			}
			for _, w := range c.wantContains {
				if !strings.Contains(got, w) {
					t.Errorf("missing %q; got:\n%s", w, got)
				}
			}
		})
	}
}

func TestRenderHSTSBlock(t *testing.T) {
	got := RenderHSTSBlock()
	for _, want := range []string{"<IfModule mod_headers.c>", "Strict-Transport-Security", "max-age=31536000", "includeSubDomains"} {
		if !strings.Contains(got, want) {
			t.Errorf("RenderHSTSBlock missing %q; got:\n%s", want, got)
		}
	}
}

func TestRenderApacheHardeningConf(t *testing.T) {
	cases := []struct {
		name       string
		proxies    string
		hsts       bool
		wantEmpty  bool
		wantRemote bool
		wantHSTS   bool
	}{
		{"neither", "", false, true, false, false},
		{"proxies only", "10.0.0.0/8", false, false, true, false},
		{"hsts only", "", true, false, false, true},
		{"both", "10.0.0.0/8", true, false, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := RenderApacheHardeningConf(c.proxies, c.hsts)
			if c.wantEmpty {
				if got != "" {
					t.Fatalf("expected empty (caller removes the file), got %q", got)
				}
				return
			}
			if !strings.Contains(got, apacheManagedMarker) {
				t.Errorf("composed conf must carry the managed marker; got:\n%s", got)
			}
			if c.wantRemote != strings.Contains(got, "mod_remoteip.c") {
				t.Errorf("remoteip presence = %v, want %v; got:\n%s", !c.wantRemote, c.wantRemote, got)
			}
			if c.wantHSTS != strings.Contains(got, "Strict-Transport-Security") {
				t.Errorf("hsts presence = %v, want %v; got:\n%s", !c.wantHSTS, c.wantHSTS, got)
			}
		})
	}
}

func TestRedisCacheDefines(t *testing.T) {
	cases := []struct {
		name string
		host string
		port int64
		want map[string]string
	}{
		{"host and port", "10.0.0.5", 6379, map[string]string{"WP_REDIS_HOST": "10.0.0.5", "WP_REDIS_PORT": "6379"}},
		{"host only", "cache.local", 0, map[string]string{"WP_REDIS_HOST": "cache.local"}},
		{"trimmed host", "  cache.local  ", 6380, map[string]string{"WP_REDIS_HOST": "cache.local", "WP_REDIS_PORT": "6380"}},
		{"blank host drops define", "", 6379, map[string]string{"WP_REDIS_PORT": "6379"}},
		{"empty", "", 0, map[string]string{}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := RedisCacheDefines(c.host, c.port); !reflect.DeepEqual(got, c.want) {
				t.Errorf("RedisCacheDefines(%q,%d) = %v, want %v", c.host, c.port, got, c.want)
			}
		})
	}
	// WP_REDIS_PORT must be raw (int), WP_REDIS_HOST a string constant.
	if !IsRawConstant("WP_REDIS_PORT") {
		t.Error("WP_REDIS_PORT should be a raw constant")
	}
	if IsRawConstant("WP_REDIS_HOST") {
		t.Error("WP_REDIS_HOST should be a string constant")
	}
}

func TestLoginSlugOptionArgs(t *testing.T) {
	got := LoginSlugOptionArgs("secret-login")
	want := []string{"option", "update", "whl_page", "secret-login"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoginSlugOptionArgs = %v, want %v", got, want)
	}
}

func TestWriteAndRemoveFileCommand(t *testing.T) {
	w := WriteFileCommand("/etc/php/conf.d/zz-wp-hardening.ini")
	if !strings.Contains(w, "mkdir -p '/etc/php/conf.d'") || !strings.Contains(w, "tee '/etc/php/conf.d/zz-wp-hardening.ini'") {
		t.Fatalf("WriteFileCommand = %q", w)
	}
	if got := RemoveFileCommand("/etc/apache2/conf-enabled/zz-wp-hardening.conf"); got != "rm -f '/etc/apache2/conf-enabled/zz-wp-hardening.conf'" {
		t.Fatalf("RemoveFileCommand = %q", got)
	}
}

func TestDirOf(t *testing.T) {
	cases := map[string]string{
		"/etc/php/conf.d/x.ini": "/etc/php/conf.d",
		"/x.conf":               "/",
		"noslash":               "/",
	}
	for in, want := range cases {
		if got := dirOf(in); got != want {
			t.Errorf("dirOf(%q) = %q, want %q", in, got, want)
		}
	}
}
