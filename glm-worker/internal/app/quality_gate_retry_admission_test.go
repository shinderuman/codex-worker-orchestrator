package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func qualityGateFailedRetryFixture(t *testing.T) (*state.StateStore, qualitygate.RunRecord, qualitygate.RunRecord) {
	t.Helper()
	_, st := newQualityGateEnv(t)
	workdir := t.TempDir()
	log := filepath.Join(workdir, "failure.log")
	if err := os.WriteFile(log, []byte("go test failed at unchanged source\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	previous := qualitygate.RunRecord{
		ValidationRunID: strings.Repeat("a", 32),
		Form: "go-test", Repository: workdir, WorkingDir: workdir,
		Head: "HEAD-1", IndexDigest: "index-1", WorktreeDigest: "tree-1",
		WorktreeDigestExcludingParent: "tree-1", TaskID: "task-1",
		StartedAt: now.Add(-time.Minute), CompletedAt: &now,
		Status: qualitygate.StatusFail, ExitCode: 1,
		ExitSource: state.ValidationExitSourceTarget, InvocationDigest: "same-argv-env-tool-timeout",
		Log: log,
	}
	if err := writeQualityGateRun(st, previous); err != nil {
		t.Fatal(err)
	}
	next := previous
	next.ValidationRunID = strings.Repeat("b", 32)
	next.StartedAt = now
	next.CompletedAt = nil
	next.Status = qualitygate.StatusRunning
	return st, previous, next
}

func TestQualityGateIdenticalTargetFailureIsNotRepeated(t *testing.T) {
	t.Parallel()
	st, previous, next := qualityGateFailedRetryFixture(t)
	found, repeated, err := repeatedFailedQualityGateRun(st, next)
	if err != nil || !repeated || found.ValidationRunID != previous.ValidationRunID {
		t.Fatalf("identical failed gate was admitted again: %#v repeated=%t err=%v", found, repeated, err)
	}
	if _, err := os.Stat(st.Path(qualitygate.RunRelativePath(next.ValidationRunID))); !os.IsNotExist(err) {
		t.Fatalf("retry admission itself created a second gate run: %v", err)
	}
}

func TestQualityGateFailureAdmitsChangedOrUnstableEvidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		modify func(*qualitygate.RunRecord)
	}{
		{"source revision", func(r *qualitygate.RunRecord) { r.Head = "HEAD-2" }},
		{"worktree revision", func(r *qualitygate.RunRecord) { r.WorktreeDigest = "tree-2" }},
		{"tool or argv or timeout", func(r *qualitygate.RunRecord) { r.InvocationDigest = "changed" }},
		{"task authority", func(r *qualitygate.RunRecord) { r.TaskID = "task-2" }},
		{"validation form", func(r *qualitygate.RunRecord) { r.Form = "go-test-race" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			st, _, next := qualityGateFailedRetryFixture(t)
			tc.modify(&next)
			if _, repeated, err := repeatedFailedQualityGateRun(st, next); err != nil || repeated {
				t.Fatalf("changed gate evidence was skipped: repeated=%t err=%v", repeated, err)
			}
		})
	}
}

func TestQualityGateFailureRequiresStableTerminalEvidence(t *testing.T) {
	t.Parallel()
	for _, outcome := range []struct {
		name string
		change func(*qualitygate.RunRecord)
	}{
		{"wrapper failure", func(r *qualitygate.RunRecord) { r.ExitSource = state.ValidationExitSourceWrapper }},
		{"interrupted", func(r *qualitygate.RunRecord) { r.Status = qualitygate.StatusInterrupted }},
		{"missing invocation", func(r *qualitygate.RunRecord) { r.InvocationDigest = "" }},
		{"missing log", func(r *qualitygate.RunRecord) { r.Log = "" }},
		{"timeout diagnosis", func(r *qualitygate.RunRecord) { r.Log = writeUnstableQualityGateLog(t, "panic: test timed out") }},
		{"network diagnosis", func(r *qualitygate.RunRecord) { r.Log = writeUnstableQualityGateLog(t, "connection reset by peer") }},
	} {
		t.Run(outcome.name, func(t *testing.T) {
			st, previous, next := qualityGateFailedRetryFixture(t)
			outcome.change(&previous)
			if err := writeQualityGateRun(st, previous); err != nil {
				t.Fatal(err)
			}
			if _, repeated, err := repeatedFailedQualityGateRun(st, next); err != nil || repeated {
				t.Fatalf("unstable or unauthenticated failure was skipped: %t %v", repeated, err)
			}
		})
	}
}

func writeUnstableQualityGateLog(t *testing.T, message string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "gate.log")
	if err := os.WriteFile(path, []byte(message), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestQualityGateFingerprintChangesWithExecutionEnvironment(t *testing.T) {
	record := qualitygate.RunRecord{Toolchain: &qualitygate.RunToolchain{RequiredGoVersion: "1.25.4"}}
	t.Setenv("QUALITY_GATE_RETRY_INTEGRATION", "first")
	a, err := qualityGateInvocationDigest(record, []string{"test", "-timeout=45m", "./..."})
	if err != nil || a == "" {
		t.Fatalf("first invocation digest: %v", err)
	}
	b, err := qualityGateInvocationDigest(record, []string{"test", "-timeout=60m", "./..."})
	if err != nil || a == b {
		t.Fatalf("timeout change did not affect digest: %v", err)
	}
	t.Setenv("QUALITY_GATE_RETRY_INTEGRATION", "second")
	c, err := qualityGateInvocationDigest(record, []string{"test", "-timeout=45m", "./..."})
	if err != nil || a == c {
		t.Fatalf("environment change did not affect digest: %v", err)
	}
}
