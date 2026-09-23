package parentcontinuation

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	testParentThread = "01a05f46-47aa-77d2-912c-0d6b078cb856"
	testWakeThread   = "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	testResumeAt     = "2026-09-11T02:00:00Z"
	testWakeAt       = "2026-09-11T02:02:00Z"
)

func TestRateLimitedRequestRequiresVerifiedAutomationForStop(t *testing.T) {
	cfg, st := rateLimitedAutomationFixture(t)
	projection := rateLimitedProjection()

	applyVerifiedAutomation(cfg, st, &projection, func(string, string) (autoresume.DBRow, error) {
		return autoresume.DBRow{}, autoresume.ErrRowNotFound
	})

	request := projection.ParentRequest
	if request == nil || request.StopAdmitted || request.CompletionAdmitted {
		t.Fatalf("unverified request = %#v", request)
	}
	if request.Continuation.State != repositoryproject.ContinuationBlocked || request.Automation != nil {
		t.Fatalf("unverified request continuation = %#v automation=%#v", request.Continuation, request.Automation)
	}
}

func TestRateLimitedRequestAcceptsExactVerifiedWakeDeferral(t *testing.T) {
	cfg, st := rateLimitedAutomationFixture(t)
	key, rrule := writeWakeAutomation(t, cfg)
	wakeAt, err := time.Parse(time.RFC3339, testWakeAt)
	if err != nil {
		t.Fatal(err)
	}
	projection := rateLimitedProjection()

	applyVerifiedAutomation(cfg, st, &projection, func(_ string, gotKey string) (autoresume.DBRow, error) {
		if gotKey != key {
			t.Fatalf("db key = %q want %q", gotKey, key)
		}
		return autoresume.DBRow{ID: key, Status: "ACTIVE", Rrule: rrule, NextRunAt: wakeAt.UnixMilli(), HasNextRun: true}, nil
	})

	request := projection.ParentRequest
	if request == nil || request.CompletionAdmitted || !request.StopAdmitted ||
		request.Continuation.State != repositoryproject.ContinuationDeferredByVerifiedAutomation ||
		request.Continuation.Reason != ReasonVerifiedAutomation || request.Automation == nil {
		t.Fatalf("verified request = %#v", request)
	}
	proof := request.Automation
	if proof.AutomationID != key || proof.ParentThread != testParentThread || proof.WakeThread != testWakeThread ||
		proof.ResumeAtUTC != testResumeAt || proof.WakeAtUTC != testWakeAt {
		t.Fatalf("automation proof = %#v", proof)
	}
}

func TestActiveContinuationActionabilityPreservesTerminalErrorSignal(t *testing.T) {
	st := activeContinuationFixture(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:   "fatal-active",
		CallType: state.CallTypeTask,
		TaskID:   taskID,
		Phase:    "worker-new",
		Outcome:  "error",
	})
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:   "probe-after-error",
		CallType: state.CallTypeProbe,
		TaskID:   taskID,
		Phase:    "probe",
		Outcome:  "success",
	})
	projection := activeContinuationProjection()
	plan := state.ParentActionPlan{RequiredAction: state.ParentActionNone, AllowedActions: []state.ParentAction{}}

	if !fatalActiveContinuationWithoutAction(st, plan, &projection) {
		t.Fatal("fatal active continuation was not recognized")
	}
	if got := latestParentMaterialOutcome(st); got != "error" {
		t.Fatalf("latest material outcome = %q want error", got)
	}
}

func TestActiveContinuationActionabilityDoesNotTreatSuccessAsTerminalError(t *testing.T) {
	st := activeContinuationFixture(t)
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:   "healthy-active",
		CallType: state.CallTypeTask,
		TaskID:   taskID,
		Phase:    "worker-new",
		Outcome:  "success",
	})
	projection := activeContinuationProjection()
	plan := state.ParentActionPlan{RequiredAction: state.ParentActionNone, AllowedActions: []state.ParentAction{}}

	if !fatalActiveContinuationWithoutAction(st, plan, &projection) {
		t.Fatal("active continuation precondition changed")
	}
	if got := latestParentMaterialOutcome(st); got != "success" {
		t.Fatalf("latest material outcome = %q want success", got)
	}
}

func activeContinuationProjection() Projection {
	return Projection{
		Consistent: true,
		ParentRequest: &Request{
			Continuation: repositoryproject.Continuation{State: repositoryproject.ContinuationContinueNow},
		},
	}
}

func activeContinuationFixture(t *testing.T) *state.StateStore {
	t.Helper()
	repoRoot := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:  repoRoot,
		RepoHash:  config.RepoHashFor(repoRoot),
		StateBase: t.TempDir(),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	return st
}

func rateLimitedProjection() Projection {
	return Projection{
		Consistent: true,
		ParentRequest: &Request{
			Continuation: repositoryproject.Continuation{
				State:  repositoryproject.ContinuationBlocked,
				Reason: string(state.TaskStatusRateLimited),
			},
		},
	}
}

func rateLimitedAutomationFixture(t *testing.T) (config.AppConfig, *state.StateStore) {
	t.Helper()
	repoRoot := t.TempDir()
	cfg := config.AppConfig{
		RepoRoot:       repoRoot,
		RepoHash:       config.RepoHashFor(repoRoot),
		StateBase:      t.TempDir(),
		CodexConfigDir: t.TempDir(),
	}
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(testParentThread, testParentThread, nil); err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Phase: "worker", Role: state.WorkerRole, Model: "opus"}
	checkpoint.SetStopKind(state.ResumeStopRateLimited)
	checkpoint.ResetAtRFC3339 = testResumeAt
	checkpoint.ResetAtCST = "2026-09-11 10:00:00 CST"
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	return cfg, st
}

func writeWakeAutomation(t *testing.T, cfg config.AppConfig) (string, string) {
	t.Helper()
	key := autoresume.CodexWakeAutomationKey(testWakeThread)
	rrule := "DTSTART:20260911T020200\nRRULE:FREQ=DAILY;COUNT=1"
	dir := filepath.Join(cfg.CodexConfigDir, "automations", key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("id = %q\nname = %q\nprompt = %q\nstatus = \"ACTIVE\"\nrrule = \"DTSTART:20260911T020200\\nRRULE:FREQ=DAILY;COUNT=1\"\ntarget_thread_id = %q\n", key, key, "resume parent "+testParentThread, testWakeThread)
	if err := os.WriteFile(filepath.Join(dir, "automation.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return key, rrule
}
