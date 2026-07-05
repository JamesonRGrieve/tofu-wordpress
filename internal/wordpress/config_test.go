// SPDX-License-Identifier: AGPL-3.0-or-later

package wordpress

import "testing"

func TestIsRawConstant(t *testing.T) {
	raw := []string{"WP_DEBUG", "DISABLE_WP_CRON", "DISALLOW_FILE_EDIT", "FORCE_SSL_ADMIN", "WP_POST_REVISIONS", "WP_REDIS_PORT"}
	for _, c := range raw {
		if !IsRawConstant(c) {
			t.Errorf("%s should be a raw (bool/int) constant", c)
		}
	}
	str := []string{"WP_MEMORY_LIMIT", "WP_CONTENT_DIR", "WP_REDIS_HOST", "DB_NAME"}
	for _, c := range str {
		if IsRawConstant(c) {
			t.Errorf("%s should be a string constant, not raw", c)
		}
	}
}

func TestNormalizeConstantValue(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"WP_DEBUG", "true", "1"},
		{"WP_DEBUG", "false", ""},
		{"DISABLE_WP_CRON", "TRUE", "1"},
		{"WP_POST_REVISIONS", "5", "5"},
		// String constants pass through untouched (including a literal "true").
		{"WP_MEMORY_LIMIT", "256M", "256M"},
		{"WP_CONTENT_DIR", "/mnt/wp-content", "/mnt/wp-content"},
	}
	for _, c := range cases {
		if got := NormalizeConstantValue(c.name, c.in); got != c.want {
			t.Errorf("NormalizeConstantValue(%s, %q) = %q, want %q", c.name, c.in, got, c.want)
		}
	}
}

func TestReconcileConstant(t *testing.T) {
	// Semantically equal → keep the declared form (no spurious diff).
	if got := ReconcileConstant("WP_DEBUG", "false", ""); got != "false" {
		t.Errorf("declared false vs read-back empty should keep %q, got %q", "false", got)
	}
	if got := ReconcileConstant("WP_DEBUG", "true", "1"); got != "true" {
		t.Errorf("declared true vs read-back 1 should keep %q, got %q", "true", got)
	}
	if got := ReconcileConstant("WP_MEMORY_LIMIT", "256M", "256M"); got != "256M" {
		t.Errorf("equal string should keep declared, got %q", got)
	}
	// Genuine drift → surface the device value.
	if got := ReconcileConstant("WP_MEMORY_LIMIT", "256M", "512M"); got != "512M" {
		t.Errorf("drift should surface the live value 512M, got %q", got)
	}
	if got := ReconcileConstant("WP_DEBUG", "false", "1"); got != "1" {
		t.Errorf("drift (false→on) should surface live 1, got %q", got)
	}
}
