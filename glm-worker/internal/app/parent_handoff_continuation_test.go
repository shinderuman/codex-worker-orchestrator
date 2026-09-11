package app

import (
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffCarriesPostLocalContinuation(t *testing.T) {
	cfg := newAppConfig(t)
	next := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{next}, nil, nil))
	writeProjectContinuationTask(t, cfg, next)
	st := startParentHandoffTask(t, cfg)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if output.Version != 2 || !output.Consistent || output.ParentRequest == nil {
		t.Fatalf("handoff = %#v", output)
	}
	continuation := output.ParentRequest.Continuation
	if output.ParentRequest.CompletionAdmitted || output.ParentRequest.StopAdmitted ||
		continuation.State != projectContinuationContinueNow || continuation.Task != next ||
		continuation.RequiredAction != projectContinuationActionStart {
		t.Fatalf("parent request = %#v", output.ParentRequest)
	}

	recovery := projectParentHandoffRecovery(output)
	if recovery.ParentRequest == nil || recovery.ParentRequest.Continuation.Task != next ||
		recovery.ParentRequest.Continuation.RequiredAction != projectContinuationActionStart {
		t.Fatalf("recovery parent request = %#v", recovery.ParentRequest)
	}
}

func TestParentHandoffCarriesBlockedOnlyStop(t *testing.T) {
	cfg := newAppConfig(t)
	blocked := "IMPLEMENTATION_TASKS/blocked.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", nil, nil, []string{blocked}))
	writeProjectContinuationTask(t, cfg, blocked)
	st := startParentHandoffTask(t, cfg)
	if err := st.SetTaskStatus(state.TaskStatusAwaitingParentCompletion); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if !output.Consistent || output.ParentRequest == nil || output.ParentRequest.CompletionAdmitted || !output.ParentRequest.StopAdmitted ||
		output.ParentRequest.Continuation.State != projectContinuationBlocked || output.ParentRequest.Continuation.Task != blocked {
		t.Fatalf("handoff parent request = %#v consistent=%v", output.ParentRequest, output.Consistent)
	}
}

func TestParentHandoffCarriesRateLimitStopFromCanonicalLifecycle(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/active.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("active", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)
	st := startParentHandoffTask(t, cfg)
	if err := st.Write("active-task", active); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{Model: "opus", StopKind: state.ResumeStopRateLimited}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if !output.Consistent || output.ParentRequest == nil || output.ParentRequest.CompletionAdmitted || output.ParentRequest.StopAdmitted {
		t.Fatalf("handoff = %#v", output)
	}
	continuation := output.ParentRequest.Continuation
	if continuation.State != projectContinuationBlocked || continuation.Reason != string(state.TaskStatusRateLimited) || continuation.RequiredAction == "" {
		t.Fatalf("rate-limit continuation = %#v", continuation)
	}
}

func TestParentHandoffFailsClosedOnInvalidProjectTerminal(t *testing.T) {
	cfg := newAppConfig(t)
	active := "IMPLEMENTATION_TASKS/active.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", projectContinuationPlan("completed", []string{active}, nil, nil))
	writeProjectContinuationTask(t, cfg, active)
	st := startParentHandoffTask(t, cfg)

	output := buildParentHandoff(st)
	if output.Consistent || output.Inconsistency == nil || !strings.Contains(*output.Inconsistency, "project continuation projection") {
		t.Fatalf("handoff = %#v", output)
	}
}
