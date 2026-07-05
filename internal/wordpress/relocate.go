// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Content-directory relocation — the headline feature. Moving wp-content on disk
// is interruption-unsafe: a naive `mv` that dies mid-copy leaves the site with a
// half-populated content dir and no way back. This models the move as an ordered
// list of named STAGES built by a pure function (BuildRelocationPlan, fully
// unit-tested) and executed behind an injected Executor (ExecuteRelocation),
// with a rollback path that restores the wp-config defines and the original
// directory WITHOUT ever deleting the original until the post-move health check
// passes. The plan is idempotent: once relocated, re-planning is a no-op.
package wordpress

import (
	"fmt"
	"strconv"
	"strings"
)

// StepKind selects how a stage's output is validated.
type StepKind int

const (
	// StepCommand is an ordinary command; a non-zero exit aborts the plan.
	StepCommand StepKind = iota
	// StepVerify prints "<srcCount> <dstCount>"; the counts must be equal.
	StepVerify
	// StepHealth prints an HTTP status code; it must be 200.
	StepHealth
)

// healthOKCode is the HTTP status the post-relocation health probe must return.
const healthOKCode = "200"

// Step is one stage of a relocation.
type Step struct {
	Name string
	Kind StepKind
	Cmd  string // remote shell command
}

// RelocationPlan is the full staged move: forward Steps executed in order, and
// Rollback steps run best-effort if any forward step fails after the commit
// point. NoOp is true when the content dir already sits at the target.
type RelocationPlan struct {
	NoOp     bool
	Steps    []Step
	Rollback []Step
}

// RelocationConfig is the declared desired state for a content-dir relocation.
type RelocationConfig struct {
	Path             string // WordPress docroot (wp --path)
	ContentDir       string // current absolute wp-content path
	TargetContentDir string // desired absolute wp-content path
	UploadsDir       string // optional: absolute uploads path (wp option upload_path)
	ContentURL       string // optional: WP_CONTENT_URL to set alongside the move
	KeepSymlink      bool   // leave a symlink at the old path pointing to the new
}

// suffix appended to the original content dir when a symlink replaces it, so the
// original bytes are retained (renamed, never deleted) until health passes.
const preTofuSuffix = ".pre-tofu"

// BuildRelocationPlan builds the staged relocation plan. When the current and
// target content dirs are identical it returns a NoOp plan (zero-diff). Pure —
// no I/O — so the full stage ordering is unit-tested without a device.
func BuildRelocationPlan(cfg RelocationConfig) RelocationPlan {
	if cfg.ContentDir == cfg.TargetContentDir {
		return RelocationPlan{NoOp: true}
	}
	src := strings.TrimRight(cfg.ContentDir, "/")
	dst := strings.TrimRight(cfg.TargetContentDir, "/")

	var steps []Step
	// (a) Quiesce writes so nothing mutates content mid-copy.
	steps = append(steps, Step{Name: "maintenance-activate", Kind: StepCommand,
		Cmd: wpCommand(cfg.Path, "maintenance-mode", "activate")})
	// (b) Copy (never move) with checksum, preserving hardlinks; then verify.
	steps = append(steps, Step{Name: "mkdir-target", Kind: StepCommand,
		Cmd: "mkdir -p " + shQuote(dst)})
	steps = append(steps, Step{Name: "rsync-copy", Kind: StepCommand,
		Cmd: fmt.Sprintf("rsync -aH --checksum %s/ %s/", shQuote(src), shQuote(dst))})
	steps = append(steps, Step{Name: "verify-copy", Kind: StepVerify,
		Cmd: fmt.Sprintf(`printf '%%s %%s' "$(find %s -type f | wc -l)" "$(find %s -type f | wc -l)"`,
			shQuote(src), shQuote(dst))})
	// (c) Point wp-config at the new location — the commit point.
	steps = append(steps, Step{Name: "config-wp-content-dir", Kind: StepCommand,
		Cmd: wpCommand(cfg.Path, "config", "set", "WP_CONTENT_DIR", dst, "--type=constant")})
	if cfg.ContentURL != "" {
		steps = append(steps, Step{Name: "config-wp-content-url", Kind: StepCommand,
			Cmd: wpCommand(cfg.Path, "config", "set", "WP_CONTENT_URL", cfg.ContentURL, "--type=constant")})
	}
	if cfg.UploadsDir != "" {
		steps = append(steps, Step{Name: "option-upload-path", Kind: StepCommand,
			Cmd: wpCommand(cfg.Path, "option", "update", "upload_path", cfg.UploadsDir)})
	}
	// (d) Optionally leave a compatibility symlink at the old path. The original
	// bytes are RENAMED aside (never deleted) so a rollback can restore them.
	if cfg.KeepSymlink {
		steps = append(steps, Step{Name: "keep-symlink", Kind: StepCommand,
			Cmd: fmt.Sprintf("[ -L %s ] || { mv %s %s 2>/dev/null || true; ln -s %s %s; }",
				shQuote(src), shQuote(src), shQuote(src+preTofuSuffix), shQuote(dst), shQuote(src))})
	}
	// (e) Prove the live site still serves before we call it done.
	steps = append(steps, Step{Name: "health-check", Kind: StepHealth,
		Cmd: fmt.Sprintf(`URL="$(%s)"; curl -s -o /dev/null -w '%%{http_code}' "$URL"`,
			wpCommand(cfg.Path, "option", "get", "siteurl"))})
	// (g) Release maintenance mode on success.
	steps = append(steps, Step{Name: "maintenance-deactivate", Kind: StepCommand,
		Cmd: wpCommand(cfg.Path, "maintenance-mode", "deactivate")})

	// (f) Rollback: restore the config define and the original directory, then
	// always release maintenance mode. Best-effort, so ordered restore-then-release.
	var rollback []Step
	rollback = append(rollback, Step{Name: "rollback-config-wp-content-dir", Kind: StepCommand,
		Cmd: wpCommand(cfg.Path, "config", "set", "WP_CONTENT_DIR", src, "--type=constant")})
	if cfg.KeepSymlink {
		rollback = append(rollback, Step{Name: "rollback-restore-original", Kind: StepCommand,
			Cmd: fmt.Sprintf("[ -L %s ] && rm -f %s; [ -d %s ] && mv %s %s || true",
				shQuote(src), shQuote(src), shQuote(src+preTofuSuffix), shQuote(src+preTofuSuffix), shQuote(src))})
	}
	rollback = append(rollback, Step{Name: "rollback-maintenance-deactivate", Kind: StepCommand,
		Cmd: wpCommand(cfg.Path, "maintenance-mode", "deactivate")})

	return RelocationPlan{Steps: steps, Rollback: rollback}
}

// ExecuteRelocation runs a relocation plan through the injected executor. On any
// forward-step failure (command error, file-count mismatch, or a non-200 health
// probe) it runs the rollback steps best-effort and returns the error. A NoOp
// plan runs nothing. This is the only code path that touches a device, and it is
// exercised in tests exclusively through an injected fake executor.
func ExecuteRelocation(exec Executor, plan RelocationPlan) error {
	if plan.NoOp {
		return nil
	}
	for _, s := range plan.Steps {
		out, err := exec.Run(s.Cmd, nil)
		if err == nil {
			err = checkStepOutput(s, out)
		}
		if err != nil {
			runRollback(exec, plan.Rollback)
			return fmt.Errorf("relocation step %q failed: %w", s.Name, err)
		}
	}
	return nil
}

// checkStepOutput validates a stage's stdout per its kind.
func checkStepOutput(s Step, out []byte) error {
	switch s.Kind {
	case StepVerify:
		src, dst, err := parseTwoCounts(out)
		if err != nil {
			return err
		}
		if src != dst {
			return fmt.Errorf("copy verification mismatch: source has %d files, target has %d", src, dst)
		}
		return nil
	case StepHealth:
		if code := trimOut(out); code != healthOKCode {
			return fmt.Errorf("health check returned HTTP %q, want %s", code, healthOKCode)
		}
		return nil
	default:
		return nil
	}
}

// parseTwoCounts parses the "<src> <dst>" output of a verify step.
func parseTwoCounts(out []byte) (int, int, error) {
	fields := strings.Fields(string(out))
	if len(fields) != 2 {
		return 0, 0, fmt.Errorf("verify step: expected two counts, got %q", strings.TrimSpace(string(out)))
	}
	src, err := strconv.Atoi(fields[0])
	if err != nil {
		return 0, 0, fmt.Errorf("verify step: source count %q not numeric", fields[0])
	}
	dst, err := strconv.Atoi(fields[1])
	if err != nil {
		return 0, 0, fmt.Errorf("verify step: target count %q not numeric", fields[1])
	}
	return src, dst, nil
}

// runRollback executes rollback steps best-effort, ignoring their errors — the
// goal is to leave the site serving from the original directory.
func runRollback(exec Executor, steps []Step) {
	for _, s := range steps {
		_, _ = exec.Run(s.Cmd, nil)
	}
}
