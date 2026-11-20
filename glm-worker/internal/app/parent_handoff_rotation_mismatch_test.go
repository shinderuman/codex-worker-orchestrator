package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffRotationPendingMismatchKeepsRecoveryDirective(t *testing.T) {
	t.Parallel()
	cfg, st, _ := seedSessionRotationAccept(t)
	promoted := "IMPLEMENTATION_TASKS/next.md"
	writeProjectStateRepoFile(t, cfg.RepoRoot, "IMPLEMENTATION_PLAN.local.md", nonGoalProjectContinuationPlan([]string{promoted}, nil, nil))
	writeProjectContinuationTask(t, cfg, promoted)
	if err := st.Write(repositoryharness.ActivationStateKey, repositoryharness.ActivationActiveValue); err != nil {
		t.Fatal(err)
	}
	saveHandoffTaskAuthority(t, st, completedNonGoalTask)
	if err := st.Write("active-task", "IMPLEMENTATION_TASKS/stale.md"); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoffWithConfig(cfg, st)
	if output.Consistent || output.Inconsistency == nil || output.ParentRequest == nil || output.SessionRotation == nil ||
		output.SessionRotation.State != state.SessionRotationProjectionPending || output.SessionRotation.Directive == nil {
		t.Fatalf("handoff = %#v", output)
	}
	attribution := output.ParentRequest.TaskAttribution
	if attribution.LifecycleTask != "IMPLEMENTATION_TASKS/stale.md" || attribution.AuthorityTask != completedNonGoalTask ||
		attribution.ActiveTask != promoted || attribution.Matches || attribution.Handover ||
		attribution.Reason != repositoryproject.ReasonActiveTaskMismatch || attribution.LegalNextAction != "" {
		t.Fatalf("task attribution = %#v", attribution)
	}

	recovery := projectParentHandoffRecovery(output)
	if recovery.Consistent || recovery.SessionRotation == nil || recovery.SessionRotation.State != state.SessionRotationProjectionPending ||
		recovery.SessionRotation.Directive == nil || recovery.SessionRotation.Directive.DirectiveID != output.SessionRotation.Directive.DirectiveID ||
		recovery.ParentRequest == nil || recovery.ParentRequest.TaskAttribution.Reason != repositoryproject.ReasonActiveTaskMismatch {
		t.Fatalf("recovery = %#v", recovery)
	}
}

func seedSessionRotationAccept(t *testing.T) (config.AppConfig, *state.StateStore, string) {
	t.Helper()
	return seedSessionRotationAcceptConfig(t, newAppConfig(t))
}

func seedSessionRotationAcceptConfig(t *testing.T, cfg config.AppConfig) (config.AppConfig, *state.StateStore, string) {
	t.Helper()
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetParentCodexIdentity(appTestParentThreadID, appTestParentSessionID, nil); err != nil {
		t.Fatal(err)
	}
	var accept acceptOutput
	var output bytes.Buffer
	if err := parentAccept(st, &output); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(output.Bytes(), &accept); err != nil {
		t.Fatal(err)
	}
	if !accept.Accepted {
		t.Fatal("open reviewをacceptできませんでした")
	}
	if _, err := completeParentWithPendingRotation(st, appTestParentThreadID); err != nil {
		t.Fatal(err)
	}
	return cfg, st, appTestParentThreadID
}

func completeParentWithPendingRotation(st *state.StateStore, parentThreadID string) (bool, error) {
	taskID, err := st.TaskID()
	if err != nil {
		return false, err
	}
	return st.CompleteParentAwaiting(func(string) (*state.SessionRotationEvaluation, error) {
		return &state.SessionRotationEvaluation{
			ParentThreadID: parentThreadID,
			TaskID:         taskID,
			Terminal:       state.SessionRotationTerminalAccept,
			Decision:       state.SessionRotationDecision{Required: true, Reason: state.SessionRotationReasonEvidenceUnavailable},
		}, nil
	})
}

func TestSessionRotationAcceptWritesPendingDirectiveProjectedByHandoff(t *testing.T) {
	t.Parallel()
	cfg, st, threadID := seedSessionRotationAccept(t)

	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil || marker == nil {
		t.Fatalf("accept後のmarker = %#v err=%v", marker, err)
	}
	if marker.State != state.SessionRotationStatePending || marker.Directive == nil {
		t.Fatalf("marker = %#v", marker)
	}
	if marker.Directive.Reason != state.SessionRotationReasonEvidenceUnavailable {
		t.Fatalf("directive reason = %q", marker.Directive.Reason)
	}
	if marker.LastEvaluation == nil || !marker.LastEvaluation.Required {
		t.Fatalf("last evaluation = %#v", marker.LastEvaluation)
	}

	var output parentHandoffOutput
	executeCommandOutput(t, cfg, ModeHandoff, &output, "--handoff")
	if !output.Consistent || output.SessionRotation == nil {
		t.Fatalf("handoff session_rotation = %#v consistent=%v", output.SessionRotation, output.Consistent)
	}
	if output.SessionRotation.State != state.SessionRotationProjectionPending || output.SessionRotation.Directive == nil {
		t.Fatalf("session_rotation projection = %#v", output.SessionRotation)
	}
	if output.SessionRotation.Directive.DirectiveID != marker.Directive.DirectiveID {
		t.Fatalf("projected directive = %#v want %s", output.SessionRotation.Directive, marker.Directive.DirectiveID)
	}
	if output.SessionRotation.Directive.Epoch != marker.Directive.TaskID+":"+marker.Directive.Terminal {
		t.Fatalf("projected directive epoch = %#v", output.SessionRotation.Directive)
	}
}

func TestSessionRotationRecoveryHandoffCarriesProjection(t *testing.T) {
	t.Parallel()
	cfg, _, _ := seedSessionRotationAccept(t)

	var out bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &out, io.Discard); err != nil {
		t.Fatal(err)
	}
	var recovery parentHandoffRecoveryOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &recovery); err != nil {
		t.Fatalf("recovery handoff出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	if recovery.SessionRotation == nil || recovery.SessionRotation.State != state.SessionRotationProjectionPending {
		t.Fatalf("recovery session_rotation = %#v", recovery.SessionRotation)
	}
}

func TestSessionRotationRepeatedHandoffWritesNothingAndReturnsSameDirective(t *testing.T) {
	t.Parallel()
	cfg, st, threadID := seedSessionRotationAccept(t)

	var first parentHandoffOutput
	executeCommandOutput(t, cfg, ModeHandoff, &first, "--handoff")
	markerBefore, err := os.ReadFile(st.SessionRotationMarkerPath(threadID))
	if err != nil {
		t.Fatal(err)
	}

	var second parentHandoffOutput
	executeCommandOutput(t, cfg, ModeHandoff, &second, "--handoff")
	if second.SessionRotation == nil || second.SessionRotation.Directive == nil {
		t.Fatalf("再読のsession_rotation = %#v", second.SessionRotation)
	}
	if second.SessionRotation.Directive.DirectiveID != first.SessionRotation.Directive.DirectiveID {
		t.Fatalf("再読が別directiveを返しました: %s -> %s", first.SessionRotation.Directive.DirectiveID, second.SessionRotation.Directive.DirectiveID)
	}
	markerAfter, err := os.ReadFile(st.SessionRotationMarkerPath(threadID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(markerBefore, markerAfter) {
		t.Fatal("handoff読み出しがrotation markerを更新しました")
	}
}

func TestSessionRotationNewThreadBindRetiresOldDirective(t *testing.T) {
	cfg, st, oldThread := seedSessionRotationAcceptConfig(t, newHarnessAppConfig(t))

	newThread := prepareNextRotatedTask(t, st)
	next := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("next")},
		{structured: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request2"}, cfg, next.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	retired, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil {
		t.Fatal(err)
	}
	if retired.State != state.SessionRotationStateIssued || retired.Issued == nil || retired.Issued.BoundThreadID != newThread {
		t.Fatalf("旧directive = %#v", retired)
	}

	var output parentHandoffOutput
	executeCommandOutput(t, cfg, ModeHandoff, &output, "--handoff")
	if !output.Consistent || output.SessionRotation == nil {
		t.Fatalf("handoff session_rotation = %#v", output.SessionRotation)
	}
	if output.SessionRotation.State == state.SessionRotationProjectionPending || output.SessionRotation.Directive != nil {
		t.Fatalf("新thread bind後に旧directiveが再投影されました: %#v", output.SessionRotation)
	}
}

func prepareNextRotatedTask(t *testing.T, st *state.StateStore) string {
	t.Helper()
	identity, err := st.CurrentParentCodexIdentity()
	if err != nil {
		t.Fatal(err)
	}
	marker, err := st.LoadSessionRotationMarker(identity.ThreadID)
	if err != nil || marker == nil || marker.Directive == nil {
		t.Fatalf("rotation marker = %#v, err=%v", marker, err)
	}
	claim, err := st.ClaimSessionRotation(identity.ThreadID, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.BindSessionRotationClaim(identity.ThreadID, marker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	t.Setenv(state.ParentActionCodexThreadIDEnv, newThread)
	t.Setenv(state.ParentActionCodexSessionIDEnv, newThread)
	t.Setenv(state.SessionRotationClaimIDEnv, claim.ClaimID)
	return newThread
}

func TestSessionRotationPendingAllowsOrdinaryNewTaskAndRetiresRecommendation(t *testing.T) {
	cfg, st, threadID := seedSessionRotationAcceptConfig(t, newHarnessAppConfig(t))
	beforeTaskID := st.ReadOr("task.id", "")
	t.Setenv(state.ParentActionCodexThreadIDEnv, threadID)
	t.Setenv(state.ParentActionCodexSessionIDEnv, threadID)
	runner := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("next")},
		{structured: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "ordinary next task"}, cfg, runner.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(runner.prompts) == 0 || st.ReadOr("task.id", "") == beforeTaskID {
		t.Fatal("ordinary start did not begin a new task")
	}
	retired, err := st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if retired == nil || retired.State != "" || retired.Directive != nil {
		t.Fatalf("pending recommendation was not retired: %#v", retired)
	}
	if retired.LastEvaluation == nil || !retired.LastEvaluation.Required {
		t.Fatalf("last evaluation evidence was not preserved: %#v", retired.LastEvaluation)
	}
}

func TestSessionRotationPendingOnOtherThreadAllowsOrdinaryStartAndRetiresRecommendation(t *testing.T) {
	cfg, st, oldThread := seedSessionRotationAcceptConfig(t, newHarnessAppConfig(t))
	marker, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil || marker == nil || marker.Directive == nil {
		t.Fatalf("pending marker = %#v err=%v", marker, err)
	}
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	beforeTaskID := st.ReadOr("task.id", "")
	t.Setenv(state.ParentActionCodexThreadIDEnv, newThread)
	t.Setenv(state.ParentActionCodexSessionIDEnv, newThread)
	runner := &fakeRunner{steps: []fakeStep{
		{structured: implementedPacketApp("next")},
		{structured: passPacketApp()},
	}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "ordinary next task"}, cfg, runner.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	if len(runner.prompts) == 0 || st.ReadOr("task.id", "") == beforeTaskID {
		t.Fatal("ordinary start on another thread did not begin a new task")
	}
	retired, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil {
		t.Fatal(err)
	}
	if retired == nil || retired.State != "" || retired.Directive != nil {
		t.Fatalf("old pending recommendation was not retired: %#v", retired)
	}
	if retired.LastEvaluation == nil || !retired.LastEvaluation.Required || retired.LastEvaluation.TaskID != marker.LastEvaluation.TaskID {
		t.Fatalf("last evaluation evidence was not preserved: %#v", retired.LastEvaluation)
	}
	incomplete, err := st.IncompleteSessionRotations()
	if err != nil {
		t.Fatal(err)
	}
	for _, rotation := range incomplete {
		if rotation.ParentThreadID == oldThread {
			t.Fatalf("retired recommendation remained incomplete: %#v", rotation)
		}
	}
}

func TestSessionRotationHandoffSurvivesUnreadableStatsMirror(t *testing.T) {
	cfg, st, threadID := seedSessionRotationAccept(t)
	if err := os.WriteFile(st.CurrentTaskStatsPath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := buildParentHandoffWithConfig(cfg, st)
	if output.SessionRotation == nil || output.SessionRotation.ParentThreadID != threadID || output.SessionRotation.State != state.SessionRotationProjectionPending {
		t.Fatalf("rotation disappeared with stats mirror: %#v", output.SessionRotation)
	}
	resume, err := st.AdmitNewTaskRotationBoundary(threadID, "")
	if err != nil || resume {
		t.Fatalf("pending recommendation blocked ordinary admission with unreadable stats: resume=%v err=%v", resume, err)
	}
	marker, err := st.LoadSessionRotationMarker(threadID)
	if err != nil {
		t.Fatal(err)
	}
	if marker == nil || marker.State != state.SessionRotationStatePending || marker.Directive == nil {
		t.Fatalf("admission check mutated pending recommendation: %#v", marker)
	}
}

func TestSessionRotationStartResumesAfterTaskSwitch(t *testing.T) {
	cfg, st, oldThread := seedSessionRotationAcceptConfig(t, newHarnessAppConfig(t))
	marker, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil {
		t.Fatal(err)
	}
	claim, err := st.ClaimSessionRotation(oldThread, marker.Directive.DirectiveID)
	if err != nil {
		t.Fatal(err)
	}
	newThread := "01a0244a-4ee4-7e71-b2e1-dec3bdda2120"
	if err := st.BindSessionRotationClaim(oldThread, marker.Directive.DirectiveID, claim.ClaimID, newThread); err != nil {
		t.Fatal(err)
	}
	if taskID, err := st.StartSessionRotationTask(newThread, claim.ClaimID); err != nil || taskID != claim.TargetTaskID {
		t.Fatalf("interrupted task switch = %s, %v", taskID, err)
	}
	t.Setenv(state.ParentActionCodexThreadIDEnv, newThread)
	t.Setenv(state.ParentActionCodexSessionIDEnv, newThread)
	t.Setenv(state.SessionRotationClaimIDEnv, claim.ClaimID)
	runner := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("resumed")}, {structured: passPacketApp()}}}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "request2"}, cfg, runner.factory(), io.Discard, io.Discard); err != nil {
		t.Fatal(err)
	}
	retired, err := st.LoadSessionRotationMarker(oldThread)
	if err != nil || retired.State != state.SessionRotationStateIssued || st.ReadOr("task.id", "") != claim.TargetTaskID {
		t.Fatalf("resumed rotation = marker:%#v task:%s err:%v", retired, st.ReadOr("task.id", ""), err)
	}
	if err := Execute(Command{Mode: ModeNewTask, Payload: "duplicate"}, cfg, (&fakeRunner{}).factory(), io.Discard, io.Discard); err == nil {
		t.Fatal("acknowledged claim was accepted twice")
	}
}
