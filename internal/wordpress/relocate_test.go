// SPDX-License-Identifier: AGPL-3.0-or-later

package wordpress

import (
	"errors"
	"strings"
	"testing"
)

func TestBuildRelocationPlan_NoOp(t *testing.T) {
	plan := BuildRelocationPlan(RelocationConfig{
		Path:             "/var/www/html",
		ContentDir:       "/var/www/html/wp-content",
		TargetContentDir: "/var/www/html/wp-content",
	})
	if !plan.NoOp {
		t.Fatalf("identical content dirs must yield a NoOp plan, got %d steps", len(plan.Steps))
	}
	if len(plan.Steps) != 0 || len(plan.Rollback) != 0 {
		t.Fatalf("NoOp plan must have no steps: %+v", plan)
	}
}

// stepNames returns the ordered stage names for assertion.
func stepNames(steps []Step) []string {
	names := make([]string, len(steps))
	for i, s := range steps {
		names[i] = s.Name
	}
	return names
}

func TestBuildRelocationPlan_FullOrder(t *testing.T) {
	plan := BuildRelocationPlan(RelocationConfig{
		Path:             "/var/www/html",
		ContentDir:       "/var/www/html/wp-content",
		TargetContentDir: "/mnt/data/wp-content",
		UploadsDir:       "/mnt/data/wp-content/uploads",
		ContentURL:       "https://example.com/wp-content",
		KeepSymlink:      true,
	})
	if plan.NoOp {
		t.Fatal("distinct dirs must not be NoOp")
	}
	want := []string{
		"maintenance-activate",
		"mkdir-target",
		"rsync-copy",
		"verify-copy",
		"config-wp-content-dir",
		"config-wp-content-url",
		"option-upload-path",
		"keep-symlink",
		"health-check",
		"maintenance-deactivate",
	}
	got := stepNames(plan.Steps)
	if len(got) != len(want) {
		t.Fatalf("step count = %d, want %d (%v)", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("step[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Rollback must restore the config define, restore the original dir, and
	// release maintenance mode — in that order.
	wantRB := []string{"rollback-config-wp-content-dir", "rollback-restore-original", "rollback-maintenance-deactivate"}
	gotRB := stepNames(plan.Rollback)
	if strings.Join(gotRB, ",") != strings.Join(wantRB, ",") {
		t.Fatalf("rollback = %v, want %v", gotRB, wantRB)
	}
	// The verify stage must be a StepVerify; the health stage a StepHealth.
	if plan.Steps[3].Kind != StepVerify {
		t.Fatalf("verify-copy kind = %v, want StepVerify", plan.Steps[3].Kind)
	}
	if plan.Steps[8].Kind != StepHealth {
		t.Fatalf("health-check kind = %v, want StepHealth", plan.Steps[8].Kind)
	}
	// The move must be a copy, never a naive mv, and must retain the original.
	if !strings.Contains(plan.Steps[2].Cmd, "rsync -aH --checksum") {
		t.Fatalf("copy stage is not an rsync checksum copy: %q", plan.Steps[2].Cmd)
	}
	if strings.HasPrefix(strings.TrimSpace(plan.Steps[2].Cmd), "mv ") {
		t.Fatal("copy stage must never be a naive mv")
	}
}

func TestBuildRelocationPlan_MinimalOptionals(t *testing.T) {
	plan := BuildRelocationPlan(RelocationConfig{
		Path:             "/srv/wp",
		ContentDir:       "/srv/wp/wp-content",
		TargetContentDir: "/data/wp-content",
	})
	got := strings.Join(stepNames(plan.Steps), ",")
	// No content_url, no uploads_dir, no symlink → those stages are absent.
	for _, absent := range []string{"config-wp-content-url", "option-upload-path", "keep-symlink"} {
		if strings.Contains(got, absent) {
			t.Fatalf("stage %q must be absent when its option is unset: %s", absent, got)
		}
	}
	// Rollback has no restore-original stage without keep_symlink.
	if strings.Contains(strings.Join(stepNames(plan.Rollback), ","), "rollback-restore-original") {
		t.Fatal("rollback-restore-original must be absent without keep_symlink")
	}
}

func TestExecuteRelocation_NoOp(t *testing.T) {
	f := &fakeExec{}
	if err := ExecuteRelocation(f, RelocationPlan{NoOp: true}); err != nil {
		t.Fatalf("NoOp execute must succeed: %v", err)
	}
	if len(f.calls) != 0 {
		t.Fatalf("NoOp execute must run nothing, ran %v", f.calls)
	}
}

// happyPlan builds a small plan whose stage kinds mirror the real one for
// executor tests, then supplies scripted responses per stage.
func fullPlan() RelocationPlan {
	return BuildRelocationPlan(RelocationConfig{
		Path:             "/var/www/html",
		ContentDir:       "/var/www/html/wp-content",
		TargetContentDir: "/mnt/data/wp-content",
		KeepSymlink:      true,
	})
}

func TestExecuteRelocation_HappyPath(t *testing.T) {
	plan := fullPlan()
	resp := make([]fakeResp, len(plan.Steps))
	for i, s := range plan.Steps {
		switch s.Kind {
		case StepVerify:
			resp[i] = fakeResp{out: "42 42"}
		case StepHealth:
			resp[i] = fakeResp{out: "200"}
		}
	}
	f := &fakeExec{responses: resp}
	if err := ExecuteRelocation(f, plan); err != nil {
		t.Fatalf("happy path must succeed: %v", err)
	}
	if len(f.calls) != len(plan.Steps) {
		t.Fatalf("ran %d commands, want %d (no rollback expected)", len(f.calls), len(plan.Steps))
	}
	// Maintenance must be released as the final forward stage.
	if !strings.Contains(f.calls[len(f.calls)-1], "'maintenance-mode' 'deactivate'") {
		t.Fatalf("last stage was not maintenance deactivate: %q", f.calls[len(f.calls)-1])
	}
}

func TestExecuteRelocation_VerifyMismatchRollsBack(t *testing.T) {
	plan := fullPlan()
	resp := make([]fakeResp, len(plan.Steps))
	for i, s := range plan.Steps {
		if s.Kind == StepVerify {
			resp[i] = fakeResp{out: "42 41"} // file-count mismatch → abort before commit
		}
	}
	f := &fakeExec{responses: resp}
	err := ExecuteRelocation(f, plan)
	if err == nil {
		t.Fatal("verify mismatch must fail the relocation")
	}
	if !strings.Contains(err.Error(), "verify-copy") {
		t.Fatalf("error should name the failing stage: %v", err)
	}
	// Rollback ran: the last recorded call is the rollback maintenance deactivate.
	last := f.calls[len(f.calls)-1]
	if !strings.Contains(last, "'maintenance-mode' 'deactivate'") {
		t.Fatalf("rollback must end by releasing maintenance mode: %q", last)
	}
	if !containsCall(f.calls, "'config' 'set' 'WP_CONTENT_DIR'") {
		t.Fatal("rollback must restore WP_CONTENT_DIR")
	}
}

func TestExecuteRelocation_HealthFailRollsBack(t *testing.T) {
	plan := fullPlan()
	resp := make([]fakeResp, len(plan.Steps))
	for i, s := range plan.Steps {
		switch s.Kind {
		case StepVerify:
			resp[i] = fakeResp{out: "10 10"}
		case StepHealth:
			resp[i] = fakeResp{out: "503"} // site down after move → roll back
		}
	}
	f := &fakeExec{responses: resp}
	err := ExecuteRelocation(f, plan)
	if err == nil {
		t.Fatal("non-200 health must fail the relocation")
	}
	if !strings.Contains(err.Error(), "health-check") {
		t.Fatalf("error should name the health stage: %v", err)
	}
	rollbackCount := 0
	for _, c := range f.calls {
		if strings.Contains(c, "'WP_CONTENT_DIR' '/var/www/html/wp-content'") {
			rollbackCount++
		}
	}
	if rollbackCount == 0 {
		t.Fatalf("health failure must roll WP_CONTENT_DIR back to the original: %v", f.calls)
	}
}

func TestExecuteRelocation_CommandErrorRollsBack(t *testing.T) {
	plan := fullPlan()
	resp := make([]fakeResp, len(plan.Steps))
	// Fail the rsync copy stage (index 2) with a transport-style error.
	resp[2] = fakeResp{err: errors.New("rsync: connection unexpectedly closed")}
	f := &fakeExec{responses: resp}
	err := ExecuteRelocation(f, plan)
	if err == nil {
		t.Fatal("a failing command stage must fail the relocation")
	}
	if !strings.Contains(err.Error(), "rsync-copy") {
		t.Fatalf("error should name the rsync stage: %v", err)
	}
}

func containsCall(calls []string, substr string) bool {
	for _, c := range calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}
