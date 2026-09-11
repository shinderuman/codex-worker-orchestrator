package app

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const (
	continuationParentThread = "01a05f46-47aa-77d2-912c-0d6b078cb856"
	continuationWakeThread   = "01a03a9e-10a0-7f11-801c-f04e5dbd5490"
	continuationResumeAt     = "2026-09-11T02:00:00Z"
	continuationWakeAt       = "2026-09-11T02:02:00Z"
)

func TestRateLimitedParentRequestRequiresVerifiedAutomationForStop(t *testing.T) {
	cfg, st, output := rateLimitedParentRequestFixture(t)
	if output.ParentRequest == nil || output.ParentRequest.StopAdmitted || output.ParentRequest.CompletionAdmitted {
		t.Fatalf("unverified parent request = %#v", output.ParentRequest)
	}
	applyVerifiedAutomationDeferral(cfg, st, &output, func(string, string) (autoresume.DBRow, error) {
		return autoresume.DBRow{}, autoresume.ErrRowNotFound
	})
	if output.ParentRequest.StopAdmitted || output.ParentRequest.Continuation.State != projectContinuationBlocked {
		t.Fatalf("missing automation authorized stop = %#v", output.ParentRequest)
	}
}

func TestRateLimitedParentRequestAcceptsExactVerifiedWakeDeferral(t *testing.T) {
	cfg, st, output := rateLimitedParentRequestFixture(t)
	key, rrule := writeContinuationWakeAutomation(t, cfg)
	wakeAt, err := time.Parse(time.RFC3339, continuationWakeAt)
	if err != nil {
		t.Fatal(err)
	}
	applyVerifiedAutomationDeferral(cfg, st, &output, func(_ string, gotKey string) (autoresume.DBRow, error) {
		if gotKey != key {
			t.Fatalf("db key = %q want %q", gotKey, key)
		}
		return autoresume.DBRow{ID: key, Status: "ACTIVE", Rrule: rrule, NextRunAt: wakeAt.UnixMilli(), HasNextRun: true}, nil
	})
	request := output.ParentRequest
	if request == nil || request.CompletionAdmitted || !request.StopAdmitted ||
		request.Continuation.State != projectContinuationDeferredByVerifiedAutomation ||
		request.Continuation.Reason != projectContinuationReasonVerifiedAutomation ||
		request.Continuation.Automation == nil {
		t.Fatalf("verified parent request = %#v", request)
	}
	proof := request.Continuation.Automation
	if proof.AutomationID != key || proof.ParentThread != continuationParentThread || proof.WakeThread != continuationWakeThread ||
		proof.ResumeAtUTC != continuationResumeAt || proof.WakeAtUTC != continuationWakeAt {
		t.Fatalf("automation proof = %#v", proof)
	}
}

func rateLimitedParentRequestFixture(t *testing.T) (config.AppConfig, *state.StateStore, parentHandoffOutput) {
	t.Helper()
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/current.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("active-task", active); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(continuationParentThread, continuationParentThread, nil); err != nil {
		t.Fatal(err)
	}
	checkpoint := state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Phase: "worker", Role: state.WorkerRole, Model: "opus"}
	checkpoint.SetStopKind(state.ResumeStopRateLimited)
	checkpoint.ResetAtRFC3339 = continuationResumeAt
	checkpoint.ResetAtCST = "2026-09-11 10:00:00 CST"
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}
	projection, err := BuildCurrentParentRequestCompletionProjection(cfg, st)
	if err != nil {
		t.Fatal(err)
	}
	return cfg, st, parentHandoffOutput{ParentRequest: &projection}
}

func writeContinuationWakeAutomation(t *testing.T, cfg config.AppConfig) (string, string) {
	t.Helper()
	key := autoresume.CodexWakeAutomationKey(continuationWakeThread)
	rrule := "DTSTART:20260911T020200\nRRULE:FREQ=DAILY;COUNT=1"
	dir := filepath.Join(cfg.CodexConfigDir, "automations", key)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := fmt.Sprintf("id = %q\nname = %q\nprompt = %q\nstatus = \"ACTIVE\"\nrrule = \"DTSTART:20260911T020200\\nRRULE:FREQ=DAILY;COUNT=1\"\ntarget_thread_id = %q\n", key, key, "resume parent "+continuationParentThread, continuationWakeThread)
	if err := os.WriteFile(filepath.Join(dir, "automation.toml"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return key, rrule
}
