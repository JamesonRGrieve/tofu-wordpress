// SPDX-License-Identifier: AGPL-3.0-or-later

package wordpress

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in            string
		maj, min, pat int
		wantErr       bool
	}{
		{"6.5.2", 6, 5, 2, false},
		{"6.5", 6, 5, 0, false},
		{"6", 6, 0, 0, false},
		{"v6.4.3", 6, 4, 3, false},
		{" 6.5.2 ", 6, 5, 2, false},
		{"", 0, 0, 0, true},
		{"6.x", 0, 0, 0, true},
	}
	for _, c := range cases {
		maj, min, pat, err := ParseVersion(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseVersion(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVersion(%q) unexpected error: %v", c.in, err)
			continue
		}
		if maj != c.maj || min != c.min || pat != c.pat {
			t.Errorf("ParseVersion(%q) = %d.%d.%d, want %d.%d.%d", c.in, maj, min, pat, c.maj, c.min, c.pat)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"6.5.2", "6.5.2", 0},
		{"6.5.1", "6.5.2", -1},
		{"6.6.0", "6.5.9", 1},
		{"6.5", "6.5.0", 0},
		{"7.0", "6.9.9", 1},
	}
	for _, c := range cases {
		got, err := CompareVersions(c.a, c.b)
		if err != nil {
			t.Errorf("CompareVersions(%q,%q) error: %v", c.a, c.b, err)
			continue
		}
		if got != c.want {
			t.Errorf("CompareVersions(%q,%q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
	if _, err := CompareVersions("bad", "6.5"); err == nil {
		t.Error("CompareVersions with unparseable input should error")
	}
}
