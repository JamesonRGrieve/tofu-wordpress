// SPDX-License-Identifier: AGPL-3.0-or-later
//
// WP-CLI wrapper. All WordPress state is read and written by running `wp` on the
// host. The command-building helpers are pure (unit-tested); the run methods go
// through an injected Executor so the apply logic is testable without a device
// and NEVER touches a real host in a unit test.
package wordpress

import (
	"fmt"
	"strings"
)

// Executor runs a remote shell command, optionally piping stdin, and returns
// stdout. *SSHClient satisfies it in production; a fake satisfies it in tests.
type Executor interface {
	Run(remote string, stdin []byte) ([]byte, error)
}

// WPCLI drives WP-CLI for a single WordPress install rooted at Path.
type WPCLI struct {
	Exec Executor
	Path string // docroot passed as --path
}

// NewWPCLI binds a WP-CLI wrapper to an executor and docroot.
func NewWPCLI(exec Executor, path string) *WPCLI {
	return &WPCLI{Exec: exec, Path: path}
}

// shQuote single-quotes a value for safe use in a remote POSIX shell command.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// wpCommand renders a `wp` invocation string: the wp binary, --path, --allow-root
// (WP-CLI refuses to run as root without it, which the transport user usually is),
// and the quoted sub-arguments. Pure — unit-tested.
func wpCommand(path string, args ...string) string {
	parts := []string{"wp", "--path=" + shQuote(path), "--allow-root"}
	for _, a := range args {
		parts = append(parts, shQuote(a))
	}
	return strings.Join(parts, " ")
}

func (w *WPCLI) run(args ...string) ([]byte, error) {
	out, err := w.Exec.Run(wpCommand(w.Path, args...), nil)
	if err != nil {
		return nil, fmt.Errorf("wp %s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

// Command runs an arbitrary WP-CLI invocation and returns trimmed stdout. It is
// the escape hatch resources use for commands without a dedicated method
// (`core download`, `plugin install`, etc.). Every argument is shell-quoted.
func (w *WPCLI) Command(args ...string) (string, error) {
	out, err := w.run(args...)
	if err != nil {
		return "", err
	}
	return trimOut(out), nil
}

// trimOut returns stdout trimmed of surrounding whitespace/newlines.
func trimOut(b []byte) string { return strings.TrimSpace(string(b)) }

// CoreVersion returns the installed WordPress version (`wp core version`).
func (w *WPCLI) CoreVersion() (string, error) {
	out, err := w.run("core", "version")
	if err != nil {
		return "", err
	}
	return trimOut(out), nil
}

// CoreIsInstalled reports whether `wp core is-installed` succeeds (exit 0).
func (w *WPCLI) CoreIsInstalled() bool {
	_, err := w.run("core", "is-installed")
	return err == nil
}

// ConfigGet returns a single wp-config.php constant value (`wp config get NAME`).
// Missing constants make wp exit non-zero → returned as ("", err).
func (w *WPCLI) ConfigGet(name string) (string, error) {
	out, err := w.run("config", "get", name, "--type=constant")
	if err != nil {
		return "", err
	}
	return trimOut(out), nil
}

// ConfigSet writes a wp-config.php constant (`wp config set`). raw controls
// whether the value is written unquoted (booleans/ints/PHP expressions) vs as a
// PHP string literal.
func (w *WPCLI) ConfigSet(name, value string, raw bool) error {
	args := configSetArgs(name, value, raw)
	_, err := w.run(args...)
	return err
}

// OptionGet returns a WordPress option value (`wp option get NAME`).
func (w *WPCLI) OptionGet(name string) (string, error) {
	out, err := w.run("option", "get", name)
	if err != nil {
		return "", err
	}
	return trimOut(out), nil
}

// configSetArgs renders the `wp config set` arguments. Pure — unit-tested.
// A raw value gets `--raw` (no PHP quoting) and `--type=constant`; a string
// value is written as a quoted literal constant.
func configSetArgs(name, value string, raw bool) []string {
	args := []string{"config", "set", name, value, "--type=constant"}
	if raw {
		args = append(args, "--raw")
	}
	return args
}
