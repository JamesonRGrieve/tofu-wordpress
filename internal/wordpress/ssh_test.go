// SPDX-License-Identifier: AGPL-3.0-or-later

package wordpress

import "testing"

// usePassword decides key-vs-password auth. A key (file or PEM) MUST always win;
// password auth engages only when a password is set and no key material is present.
func TestUsePassword(t *testing.T) {
	cases := []struct {
		name string
		cfg  SSHConfig
		want bool
	}{
		{name: "nothing set → key/agent path", cfg: SSHConfig{Host: "h"}, want: false},
		{name: "password only → password", cfg: SSHConfig{Host: "h", Password: "pw"}, want: true},
		{name: "password + key_file → key wins", cfg: SSHConfig{Host: "h", Password: "pw", KeyFile: "/id"}, want: false},
		{name: "password + key_pem → key wins", cfg: SSHConfig{Host: "h", Password: "pw", KeyPEM: "-----BEGIN-----"}, want: false},
		{name: "password + blank key_pem → password", cfg: SSHConfig{Host: "h", Password: "pw", KeyPEM: "  \n"}, want: true},
		{name: "key only, no password → key", cfg: SSHConfig{Host: "h", KeyFile: "/id"}, want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NewSSHClient(tc.cfg).usePassword(); got != tc.want {
				t.Fatalf("usePassword() = %v, want %v", got, tc.want)
			}
		})
	}
}
