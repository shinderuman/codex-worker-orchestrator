package app

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseCommandParentHandoff(t *testing.T) {
	command, err := ParseCommand([]string{"--handoff"})
	if err != nil {
		t.Fatal(err)
	}
	if command.Mode != ModeHandoff || command.Payload != "" {
		t.Fatalf("handoff command = %#v", command)
	}
	recovery, err := ParseCommand([]string{"--handoff", "recovery"})
	if err != nil {
		t.Fatal(err)
	}
	if recovery.Mode != ModeHandoff || recovery.Payload != "recovery" {
		t.Fatalf("recovery handoff command = %#v", recovery)
	}
	if _, err := ParseCommand([]string{"--handoff", "extra"}); err == nil {
		t.Fatal("--handoff accepted an unknown projection")
	}
	if _, err := ParseCommand([]string{"--handoff", "recovery", "extra"}); err == nil {
		t.Fatal("--handoff recovery accepted an extra argument")
	}
}

func TestParentHandoffPassRequiresAcceptThenBecomesNoAction(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{})
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:       "call-pass",
		CallType:     state.CallTypeTask,
		TaskID:       taskID,
		Phase:        "reviewer-1",
		Role:         state.ReviewerRole,
		ModelAlias:   "haiku",
		Outcome:      "success",
		PacketStatus: string(packet.StatusPass),
	})

	var output parentHandoffOutput
	executeCommandOutput(t, cfg, ModeHandoff, &output, "--handoff")
	if !output.Consistent || output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionAccept) {
		t.Fatalf("PASS handoff = %#v", output)
	}
	if len(output.AllowedActions) != 1 || output.AllowedActions[0] != string(state.ParentActionAccept) {
		t.Fatalf("PASS allowed actions = %#v", output.AllowedActions)
	}
	if output.ParentReviewOpen == nil || *output.ParentReviewOpen != string(packet.StatusPass) {
		t.Fatalf("PASS parent review = %#v", output.ParentReviewOpen)
	}
	if output.Snapshot == nil || output.Baseline == nil || output.Baseline.Status == "" || output.Baseline.WorktreePatch == "" || output.Baseline.IndexPatch == "" {
		t.Fatalf("handoff evidence = %#v", output)
	}
	if output.LastMaterial == nil || output.LastMaterial.CallID == nil || *output.LastMaterial.CallID != "call-pass" {
		t.Fatalf("last material = %#v", output.LastMaterial)
	}

	accepted, err := st.AcceptParentReview()
	if err != nil || !accepted {
		t.Fatalf("accept = %v err=%v", accepted, err)
	}
	output = buildParentHandoff(st)
	if !output.Consistent || output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionComplete) || output.ParentReviewOpen != nil {
		t.Fatalf("awaiting handoff = %#v", output)
	}
	if len(output.AllowedActions) != 2 || output.AllowedActions[0] != string(state.ParentActionComplete) || output.AllowedActions[1] != string(state.ParentActionInstall) {
		t.Fatalf("awaiting allowed actions = %#v", output.AllowedActions)
	}

	if _, err := st.CompleteParentAwaiting(nil); err != nil {
		t.Fatal(err)
	}
	output = buildParentHandoff(st)
	if !output.Consistent || output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionNone) || output.ParentReviewOpen != nil {
		t.Fatalf("completed handoff = %#v", output)
	}
}

func TestParentHandoffRecoveryProjectionOmitsBroadEvidence(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := state.CaptureGitBaseline(cfg, st); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	st.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskLow}, state.ParentReviewProducer{})
	taskID, err := st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	st.RecordModelCallLog(state.ModelCallLog{
		CallID:             "call-recovery",
		CallType:           state.CallTypeTask,
		TaskID:             taskID,
		Phase:              "reviewer-1",
		Role:               state.ReviewerRole,
		ModelAlias:         "haiku",
		Outcome:            "invalid_packet",
		PacketStatus:       string(packet.StatusPass),
		PacketRejectReason: "structured-output",
		Error:              "packet validation failed",
	})

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("recovery output is not JSON: %v\n%s", err, stdout.String())
	}
	for _, forbidden := range []string{"baseline", "snapshot", "artifact_dir", "validations", "resume_kind", "inconsistency"} {
		if _, exists := raw[forbidden]; exists {
			t.Fatalf("recovery output leaked %q: %s", forbidden, stdout.String())
		}
	}
	if raw["projection"] != "recovery" || raw["consistent"] != true {
		t.Fatalf("recovery identity = %s", stdout.String())
	}
	material, ok := raw["last_material"].(map[string]any)
	if !ok || material["call_id"] != "call-recovery" || material["outcome"] != "invalid_packet" {
		t.Fatalf("recovery material = %#v", raw["last_material"])
	}
	if material["packet_reject_reason"] != "structured-output" || material["packet_error"] != "packet validation failed" {
		t.Fatalf("recovery diagnostics = %#v", material)
	}
	for _, forbidden := range []string{"role", "model"} {
		if _, exists := material[forbidden]; exists {
			t.Fatalf("recovery material leaked %q: %s", forbidden, stdout.String())
		}
	}

	stdout.Reset()
	if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	raw = nil
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("full handoff output is not JSON: %v\n%s", err, stdout.String())
	}
	material, ok = raw["last_material"].(map[string]any)
	if !ok {
		t.Fatalf("full handoff material = %#v", raw["last_material"])
	}
	for _, hidden := range []string{"packet_reject_reason", "packet_error"} {
		if _, exists := material[hidden]; exists {
			t.Fatalf("full handoff exposed recovery-only %q: %s", hidden, stdout.String())
		}
	}
}

func TestParentHandoffRecoveryIncludesGuardDiagnostics(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := st.SetTaskStatus(state.TaskStatusGuardRecoverable); err != nil {
		t.Fatal(err)
	}
	before := &state.GuardRefState{Name: "refs/codex/turn-diffs/old", ObjectID: strings.Repeat("a", 40)}
	after := &state.GuardRefState{Name: "refs/codex/turn-diffs/old", ObjectID: strings.Repeat("b", 40)}
	checkpoint := state.ResumeCheckpoint{
		Model:                    "glm-5.3",
		StopKind:                 state.ResumeStopGuardRecoverable,
		GuardFailure:             "git authority guard failed: after-call-mutation: refs",
		GuardRefBeforeDigest:     "before",
		GuardRefAfterDigest:      "after",
		GuardRefChanges:          []state.GuardRefChange{{Name: "refs/codex/turn-diffs/old", Before: before, After: after}},
		GuardRefChangesTruncated: true,
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("recovery output is not JSON: %v\n%s", err, stdout.String())
	}
	if raw["guard_failure"] != checkpoint.GuardFailure || raw["guard_ref_changes_truncated"] != true {
		t.Fatalf("guard diagnostics = %s", stdout.String())
	}
	changes, ok := raw["guard_ref_changes"].([]any)
	if !ok || len(changes) != 1 {
		t.Fatalf("guard ref changes = %#v", raw["guard_ref_changes"])
	}
	change, ok := changes[0].(map[string]any)
	if !ok || change["name"] != "refs/codex/turn-diffs/old" {
		t.Fatalf("guard ref change = %#v", changes[0])
	}

	stdout.Reset()
	if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	raw = nil
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("full handoff output is not JSON: %v\n%s", err, stdout.String())
	}
	for _, hidden := range []string{"guard_failure", "guard_ref_changes", "guard_ref_changes_truncated"} {
		if _, exists := raw[hidden]; exists {
			t.Fatalf("full handoff exposed recovery-only %q: %s", hidden, stdout.String())
		}
	}
}

func TestParentHandoffRecoveryIncludesQualityGateDiagnostics(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	completed := packet.Result{Status: packet.StatusImplemented, Risk: packet.RiskLow, Summary: "implemented"}
	checkpoint := state.ResumeCheckpoint{
		Stage:              state.ResumeStageWorker,
		Phase:              "worker-new",
		Role:               state.WorkerRole,
		Model:              "glm-5.3",
		StopKind:           state.ResumeStopQualityGate,
		QualityGateFailure: "quality tool version mismatch: golangci-lint=2.6.0, required=2.7.0",
		CompletedResult:    &completed,
	}
	if err := st.SaveResumeCheckpoint(checkpoint); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusQualityGateRecoverable); err != nil {
		t.Fatal(err)
	}

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeHandoff, Payload: "recovery"}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("recovery output is not JSON: %v\n%s", err, stdout.String())
	}
	if raw["consistent"] != true {
		t.Fatalf("recovery consistency = %s", stdout.String())
	}
	if raw["task_status"] != string(state.TaskStatusQualityGateRecoverable) {
		t.Fatalf("recovery task status = %s", stdout.String())
	}
	if raw["required_action"] != string(state.ParentActionRepairQualityGateThenResume) {
		t.Fatalf("recovery required action = %s", stdout.String())
	}
	allowed, ok := raw["allowed_actions"].([]any)
	if !ok || len(allowed) != 1 || allowed[0] != string(state.ParentActionResume) {
		t.Fatalf("recovery allowed actions = %#v", raw["allowed_actions"])
	}
	if raw["quality_gate_failure"] != checkpoint.QualityGateFailure {
		t.Fatalf("quality gate diagnostics = %s", stdout.String())
	}

	stdout.Reset()
	if err := Execute(Command{Mode: ModeHandoff}, cfg, nil, &stdout, io.Discard); err != nil {
		t.Fatal(err)
	}
	raw = nil
	if err := json.Unmarshal(stdout.Bytes(), &raw); err != nil {
		t.Fatalf("full handoff output is not JSON: %v\n%s", err, stdout.String())
	}
	if raw["consistent"] != true || raw["task_status"] != string(state.TaskStatusQualityGateRecoverable) ||
		raw["required_action"] != string(state.ParentActionRepairQualityGateThenResume) {
		t.Fatalf("full handoff next action = %s", stdout.String())
	}
	if _, exists := raw["quality_gate_failure"]; exists {
		t.Fatalf("full handoff exposed recovery-only quality gate failure: %s", stdout.String())
	}
}

func TestParentHandoffStoppedDecisionContinuationAllowsResume(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := st.Write("last-request", "request"); err != nil {
		t.Fatal(err)
	}
	if err := st.Touch("pending-decision"); err != nil {
		t.Fatal(err)
	}
	if err := st.Write("last-decision", "decision-body"); err != nil {
		t.Fatal(err)
	}
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:          state.ResumeStageWorker,
		Phase:          "worker-decision",
		Role:           state.WorkerRole,
		Model:          "opus",
		Prompt:         "p",
		OriginalPrompt: "p",
		Request:        "request",
		Decision:       "decision-body",
		StopKind:       state.ResumeStopRateLimited,
		ResetAtCST:     "2026-09-05 03:32:23",
		ResetAtRFC3339: "2026-09-05T03:32:23+08:00",
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusRateLimited); err != nil {
		t.Fatal(err)
	}

	var output parentHandoffOutput
	executeCommandOutput(t, cfg, ModeHandoff, &output, "--handoff")
	if !output.Consistent || output.Inconsistency != nil {
		t.Fatalf("stopped decision continuation handoff = %#v", output)
	}
	if output.RequiredAction == nil || *output.RequiredAction != string(state.ParentActionResume) {
		t.Fatalf("stopped decision continuation required action = %#v", output.RequiredAction)
	}
	if len(output.AllowedActions) != 1 || output.AllowedActions[0] != string(state.ParentActionResume) {
		t.Fatalf("stopped decision continuation allowed actions = %#v", output.AllowedActions)
	}
	if output.ResumeKind == nil || *output.ResumeKind != string(state.ResumeStopRateLimited) {
		t.Fatalf("stopped decision continuation resume kind = %#v", output.ResumeKind)
	}
	if !output.PendingDecision || output.TaskStatus == nil || *output.TaskStatus != string(state.TaskStatusRateLimited) {
		t.Fatalf("stopped decision continuation task state = %#v", output)
	}
}

func TestParentHandoffFailsClosedOnLifecycleContradiction(t *testing.T) {
	cfg := newAppConfig(t)
	st := startParentHandoffTask(t, cfg)
	if err := st.SetTaskStatus(state.TaskStatusWaitingDecision); err != nil {
		t.Fatal(err)
	}

	output := buildParentHandoff(st)
	if output.Consistent || output.Inconsistency == nil || output.RequiredAction != nil || len(output.AllowedActions) != 0 {
		t.Fatalf("contradictory handoff = %#v", output)
	}
	if !strings.Contains(*output.Inconsistency, "lifecycle inconsistency") {
		t.Fatalf("inconsistency = %q", *output.Inconsistency)
	}
}

func TestParentHandoffValidationReferencesMatchCurrentSnapshot(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	matching := qualityGateRunRecord{
		ValidationRunID: strings.Repeat("a", 32),
		Form:            "go-test",
		Repository:      cfg.RepoRoot,
		WorkingDir:      cfg.RepoRoot,
		Head:            snapshot.Head,
		IndexDigest:     snapshot.IndexDigest,
		WorktreeDigest:  snapshot.WorktreeDigest,
		StartedAt:       now,
		Status:          qualityGateStatusPass,
		Log:             "/evidence/current/gate.log",
	}
	stale := matching
	stale.ValidationRunID = strings.Repeat("b", 32)
	stale.StartedAt = now.Add(time.Minute)
	stale.WorktreeDigest = "stale-worktree"
	stale.WorkingDir = "/evidence/stale/working-dir"
	stale.Log = "/evidence/stale/gate.log"
	race := matching
	race.ValidationRunID = strings.Repeat("c", 32)
	race.Form = "go-test-race"
	race.StartedAt = now.Add(2 * time.Minute)
	race.Status = qualityGateStatusRunning
	race.Log = "/evidence/race/gate.log"
	for _, record := range []qualityGateRunRecord{matching, stale, race} {
		if err := writeQualityGateRun(st, record); err != nil {
			t.Fatal(err)
		}
	}

	output := buildParentHandoff(st)
	if !output.Consistent || len(output.Validations) != 2 {
		t.Fatalf("validations = %#v", output.Validations)
	}
	if output.Validations[0].Form != "go-test" || output.Validations[0].ValidationRunID != matching.ValidationRunID || output.Validations[0].WorkingDir != matching.WorkingDir || output.Validations[0].Log != matching.Log {
		t.Fatalf("go-test validation = %#v", output.Validations[0])
	}
	if output.Validations[1].Form != "go-test-race" || output.Validations[1].ValidationRunID != race.ValidationRunID || output.Validations[1].WorkingDir != race.WorkingDir {
		t.Fatalf("race validation = %#v", output.Validations[1])
	}
	for _, validation := range output.Validations {
		if validation.ValidationRunID == stale.ValidationRunID || validation.WorkingDir == stale.WorkingDir {
			t.Fatalf("stale snapshot validation leaked into handoff: %#v", validation)
		}
	}
}

func TestParentHandoffRoutingEvidenceMatchesImplementationSnapshot(t *testing.T) {
	const routingEvidenceTaskID = "12345678-1111-2222-3333-444444444444"
	cases := []struct {
		name        string
		mutate      func(record *qualityGateRunRecord)
		wantMatches bool
		wantMatch   string
	}{
		{
			name:        "exact snapshot match stays routing evidence",
			mutate:      func(_ *qualityGateRunRecord) {},
			wantMatches: true,
			wantMatch:   routingSnapshotMatchExact,
		},
		{
			name: "parent metadata drift stays routing evidence",
			mutate: func(record *qualityGateRunRecord) {
				record.WorktreeDigest = "parent-metadata-drift"
			},
			wantMatches: true,
			wantMatch:   routingSnapshotMatchParentMetadataOnly,
		},
		{
			name: "legacy record without parent-excluded digest matches exact only",
			mutate: func(record *qualityGateRunRecord) {
				record.WorktreeDigestExcludingParent = ""
			},
			wantMatches: true,
			wantMatch:   routingSnapshotMatchExact,
		},
		{
			name: "implementation drift drops routing evidence",
			mutate: func(record *qualityGateRunRecord) {
				record.WorktreeDigest = "parent-metadata-drift"
				record.WorktreeDigestExcludingParent = "implementation-drift"
			},
			wantMatches: false,
		},
		{
			name: "head change drops routing evidence",
			mutate: func(record *qualityGateRunRecord) {
				record.Head = "other-head"
			},
			wantMatches: false,
		},
		{
			name: "index change drops routing evidence",
			mutate: func(record *qualityGateRunRecord) {
				record.IndexDigest = "other-index"
			},
			wantMatches: false,
		},
		{
			name: "task change drops routing evidence",
			mutate: func(record *qualityGateRunRecord) {
				record.TaskID = "12345678-9999-8888-7777-666666666666"
			},
			wantMatches: false,
		},
		{
			name: "failed validation is not routing evidence",
			mutate: func(record *qualityGateRunRecord) {
				record.Status = qualityGateStatusFail
			},
			wantMatches: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := newAppConfig(t)
			st, err := state.NewStateStore(cfg)
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Write("task.id", routingEvidenceTaskID); err != nil {
				t.Fatal(err)
			}
			snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
			if err != nil {
				t.Fatal(err)
			}
			record := qualityGateRunRecord{
				ValidationRunID:               strings.Repeat("a", 32),
				Form:                          "go-test",
				Repository:                    cfg.RepoRoot,
				WorkingDir:                    filepath.Join(cfg.RepoRoot, "module"),
				Head:                          snapshot.Head,
				IndexDigest:                   snapshot.IndexDigest,
				WorktreeDigest:                snapshot.WorktreeDigest,
				WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
				TaskID:                        routingEvidenceTaskID,
				StartedAt:                     time.Now().UTC(),
				Status:                        qualityGateStatusPass,
			}
			tc.mutate(&record)
			if err := writeQualityGateRun(st, record); err != nil {
				t.Fatal(err)
			}

			output := buildParentHandoff(st)
			if !output.Consistent {
				t.Fatalf("handoff = %#v", output)
			}
			if !tc.wantMatches {
				if len(output.RoutingEvidence) != 0 {
					t.Fatalf("routing evidence = %#v", output.RoutingEvidence)
				}
				return
			}
			if len(output.RoutingEvidence) != 1 {
				t.Fatalf("routing evidence = %#v", output.RoutingEvidence)
			}
			evidence := output.RoutingEvidence[0]
			if evidence.ValidationRunID != record.ValidationRunID || evidence.Form != record.Form ||
				evidence.WorkingDir != record.WorkingDir || evidence.SnapshotMatch != tc.wantMatch {
				t.Fatalf("routing evidence = %#v", evidence)
			}
		})
	}
}

func TestQualityGateRunRecordCarriesRoutingIdentity(t *testing.T) {
	_, st := newQualityGateEnv(t)
	taskID := "12345678-aaaa-bbbb-cccc-dddddddddddd"
	if err := st.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	identity, err := prepareQualityGateStart("go-test", st)
	if err != nil {
		t.Fatal(err)
	}
	if identity.TaskID != taskID {
		t.Fatalf("identity = %#v", identity)
	}
	record, err := newQualityGateRunRecord(identity)
	if err != nil {
		t.Fatal(err)
	}
	if record.TaskID != taskID {
		t.Fatalf("record task id = %q", record.TaskID)
	}
	if record.WorktreeDigestExcludingParent == "" || record.WorktreeDigestExcludingParent != identity.Snapshot.WorktreeDigestExcludingParent {
		t.Fatalf("record parent-excluded digest = %q", record.WorktreeDigestExcludingParent)
	}
}

func startParentHandoffTask(t *testing.T, cfg config.AppConfig) *state.StateStore {
	t.Helper()
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	return st
}

func seedSessionRotationAccept(t *testing.T) (config.AppConfig, *state.StateStore, string) {
	t.Helper()
	cfg := newAppConfig(t)
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
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.ParentReviewOpen = &state.ParentReviewOpenState{PacketStatus: "PASS", Risk: "LOW"}
	})
	if err := st.SetParentCodexIdentity(codexTestParentThreadID, codexTestParentSessionID, nil); err != nil {
		t.Fatal(err)
	}
	var accept acceptOutput
	executeCommandOutput(t, cfg, ModeAccept, &accept, "accept")
	if !accept.Accepted {
		t.Fatal("open reviewをacceptできませんでした")
	}
	if _, err := st.CompleteParentAwaiting(func(acceptedRisk string) (*state.SessionRotationEvaluation, error) {
		return EvaluateSessionRotationTerminal(cfg, st, state.SessionRotationTerminalAccept, acceptedRisk)
	}); err != nil {
		t.Fatal(err)
	}
	return cfg, st, codexTestParentThreadID
}

func TestSessionRotationAcceptWritesPendingDirectiveProjectedByHandoff(t *testing.T) {
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

func TestSessionRotationCompletionFailsClosedWithoutParentIdentity(t *testing.T) {
	cfg := newAppConfig(t)
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
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.ParentReviewOpen = &state.ParentReviewOpenState{PacketStatus: "PASS", Risk: "LOW"}
	})

	if _, err := st.AcceptParentReview(); err != nil {
		t.Fatal(err)
	}
	_, err = st.CompleteParentAwaiting(func(acceptedRisk string) (*state.SessionRotationEvaluation, error) {
		return EvaluateSessionRotationTerminal(cfg, st, state.SessionRotationTerminalAccept, acceptedRisk)
	})
	if err == nil || !strings.Contains(err.Error(), "session rotation") {
		t.Fatalf("identity欠損のcompletionがfail closedしませんでした: %v", err)
	}
	if got := st.TaskStatus(); got != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("fail closed後のstatus = %q", got)
	}
	stats, err := st.CurrentTaskStats()
	if err != nil {
		t.Fatal(err)
	}
	if stats.ParentOutcomes[state.ParentOutcomeAccepted] != 1 {
		t.Fatalf("fail closed後のstats = %#v", stats)
	}
}

func TestSessionRotationNewThreadBindRetiresOldDirective(t *testing.T) {
	cfg, st, oldThread := seedSessionRotationAccept(t)

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

func TestSessionRotationPendingRejectsNewTaskBeforeMutation(t *testing.T) {
	cfg, st, threadID := seedSessionRotationAccept(t)
	beforeTaskID := st.ReadOr("task.id", "")
	t.Setenv(state.ParentActionCodexThreadIDEnv, threadID)
	t.Setenv(state.ParentActionCodexSessionIDEnv, threadID)
	runner := &fakeRunner{}
	err := Execute(Command{Mode: ModeNewTask, Payload: "must not run"}, cfg, runner.factory(), io.Discard, io.Discard)
	if err == nil || !strings.Contains(err.Error(), "session rotation") {
		t.Fatalf("pending rotation admitted: %v", err)
	}
	if len(runner.prompts) != 0 || st.ReadOr("task.id", "") != beforeTaskID {
		t.Fatal("rejected start changed task or invoked model")
	}
}

func TestSessionRotationHandoffSurvivesUnreadableStatsMirror(t *testing.T) {
	_, st, threadID := seedSessionRotationAccept(t)
	if err := os.WriteFile(st.CurrentTaskStatsPath(), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	output := buildParentHandoff(st)
	if output.SessionRotation == nil || output.SessionRotation.ParentThreadID != threadID || output.SessionRotation.State != state.SessionRotationProjectionPending {
		t.Fatalf("rotation disappeared with stats mirror: %#v", output.SessionRotation)
	}
	if err := st.ValidateNewTaskRotation(threadID, ""); err == nil {
		t.Fatal("unreadable stats bypassed rotation")
	}
}

func TestSessionRotationStartResumesAfterTaskSwitch(t *testing.T) {
	cfg, st, oldThread := seedSessionRotationAccept(t)
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
