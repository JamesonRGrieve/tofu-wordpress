// SPDX-License-Identifier: AGPL-3.0-or-later

package wordpress

import (
	"errors"
	"strings"
	"testing"
)

func TestShQuote(t *testing.T) {
	cases := []struct{ in, want string }{
		{"plain", "'plain'"},
		{"/var/www/html", "'/var/www/html'"},
		{"a'b", `'a'\''b'`},
	}
	for _, c := range cases {
		if got := shQuote(c.in); got != c.want {
			t.Errorf("shQuote(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestWPCommand(t *testing.T) {
	got := wpCommand("/var/www/html", "core", "version")
	want := "wp --path='/var/www/html' --allow-root 'core' 'version'"
	if got != want {
		t.Fatalf("wpCommand = %q, want %q", got, want)
	}
	// A value containing a space is single-quoted intact.
	got = wpCommand("/srv/wp", "option", "update", "blogname", "My Site")
	if !strings.Contains(got, "'My Site'") {
		t.Fatalf("spaced value not quoted intact: %q", got)
	}
}

func TestConfigSetArgs(t *testing.T) {
	raw := configSetArgs("WP_DEBUG", "true", true)
	wantRaw := []string{"config", "set", "WP_DEBUG", "true", "--type=constant", "--raw"}
	if strings.Join(raw, " ") != strings.Join(wantRaw, " ") {
		t.Fatalf("raw args = %v, want %v", raw, wantRaw)
	}
	str := configSetArgs("WP_CONTENT_DIR", "/mnt/wp-content", false)
	wantStr := []string{"config", "set", "WP_CONTENT_DIR", "/mnt/wp-content", "--type=constant"}
	if strings.Join(str, " ") != strings.Join(wantStr, " ") {
		t.Fatalf("string args = %v, want %v", str, wantStr)
	}
}

func TestWPCLI_CoreVersion(t *testing.T) {
	f := &fakeExec{responses: []fakeResp{{out: "6.5.2\n"}}}
	w := NewWPCLI(f, "/var/www/html")
	v, err := w.CoreVersion()
	if err != nil {
		t.Fatalf("CoreVersion err: %v", err)
	}
	if v != "6.5.2" {
		t.Fatalf("CoreVersion = %q, want 6.5.2", v)
	}
	if f.calls[0] != "wp --path='/var/www/html' --allow-root 'core' 'version'" {
		t.Fatalf("unexpected command: %q", f.calls[0])
	}
}

func TestWPCLI_CoreIsInstalled(t *testing.T) {
	ok := &fakeExec{responses: []fakeResp{{}}}
	if !NewWPCLI(ok, "/w").CoreIsInstalled() {
		t.Fatal("exit 0 must report installed")
	}
	notOK := &fakeExec{responses: []fakeResp{{err: errors.New("Error: not installed")}}}
	if NewWPCLI(notOK, "/w").CoreIsInstalled() {
		t.Fatal("non-zero exit must report not installed")
	}
}

func TestWPCLI_ConfigSetPropagatesError(t *testing.T) {
	f := &fakeExec{responses: []fakeResp{{err: errors.New("wp: boom")}}}
	if err := NewWPCLI(f, "/w").ConfigSet("WP_DEBUG", "true", true); err == nil {
		t.Fatal("ConfigSet must surface the executor error")
	}
}
