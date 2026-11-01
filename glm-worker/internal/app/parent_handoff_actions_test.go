package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffResultFormatStopIncludesResubmitReport(t *testing.T) {
	t.Parallel()
	for _, projection := range []string{"", "recovery"} {
		t.Run(projection, func(t *testing.T) {
			cfg := newAppConfig(t)
			st := startParentHandoffTask(t, cfg)
			if err := st.Write(state.ResultCorrectionStateFile, `{"version":2,"resubmissions":0,"terminal":{"reason":"repeated_violation"}}`); err != nil {
				t.Fatal(err)
			}
			if err := st.EnterStop(state.ResumeCheckpoint{
				Stage: state.ResumeStageWorker, Phase: "worker-new-result-correct",
				Role: state.WorkerRole, Model: "worker", ReadOnly: true,
				StopKind: state.ResumeStopResultFormat, ResultCorrection: true,
			}); err != nil {
				t.Fatal(err)
			}
			var stdout bytes.Buffer
			if err := Execute(Command{Mode: ModeHandoff, Payload: projection}, cfg, nil, &stdout, io.Discard); err != nil {
				t.Fatal(err)
			}
			var output struct {
				Consistent     bool                               `json:"consistent"`
				RequiredAction string                             `json:"required_action"`
				AllowedActions []string                           `json:"allowed_actions"`
				ActionSpecs    map[string]parentHandoffActionSpec `json:"action_specs"`
			}
			if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
				t.Fatal(err)
			}
			action := string(state.ParentActionResubmitReport)
			if !output.Consistent || output.RequiredAction != action || !reflect.DeepEqual(output.AllowedActions, []string{action}) {
				t.Fatalf("handoff = %#v", output)
			}
			spec := output.ActionSpecs[action]
			if spec.Kind != "direct" || !reflect.DeepEqual(spec.Command, []string{"glm-parent-action", action}) {
				t.Fatalf("resubmit spec = %#v", spec)
			}
			if _, admitted, err := st.AdmitParentAction(state.ParentActionResubmitReport); err != nil || !admitted {
				t.Fatalf("resubmit admission = %v, err = %v", admitted, err)
			}
			if _, admitted, err := st.AdmitParentAction(state.ParentActionResume); err != nil || admitted {
				t.Fatalf("resume admission = %v, err = %v", admitted, err)
			}
		})
	}
}

func TestParentHandoffExhaustedReportDoesNotAdvertiseUnexecutableResubmission(t *testing.T) {
	t.Parallel()
	for _, projection := range []string{"", "recovery"} {
		t.Run(projection, func(t *testing.T) {
			cfg := newAppConfig(t)
			st := startParentHandoffTask(t, cfg)
			if err := st.Write(state.ResultCorrectionStateFile, `{"version":2,"resubmissions":1,"terminal":{"reason":"repeated_violation"}}`); err != nil {
				t.Fatal(err)
			}
			if err := st.EnterStop(state.ResumeCheckpoint{Stage: state.ResumeStageWorker, Role: state.WorkerRole, Model: "worker", Phase: "worker-new-result-correct", ReadOnly: true, StopKind: state.ResumeStopResultFormat, ResultCorrection: true}); err != nil {
				t.Fatal(err)
			}
			var output bytes.Buffer
			if err := Execute(Command{Mode: ModeHandoff, Payload: projection}, cfg, nil, &output, io.Discard); err != nil {
				t.Fatal(err)
			}
			var result parentHandoffRecoveryOutput
			if err := json.Unmarshal(output.Bytes(), &result); err != nil {
				t.Fatal(err)
			}
			if result.Consistent || result.RequiredAction != nil || len(result.AllowedActions) != 0 || result.Inconsistency == nil {
				t.Fatalf("exhausted report still advertised an executable action: %+v", result)
			}
		})
	}
}

func TestParentHandoffAdvertisesResubmissionWhenSummaryRecoveryIsAvailable(t *testing.T) {
	t.Parallel()
	for _, projection := range []string{"", "recovery"} {
		t.Run(projection, func(t *testing.T) {
			cfg := newAppConfig(t)
			st := startParentHandoffTask(t, cfg)
			const sessionID = "app-summary-session"
			const phase = "worker-new-result-correct"
			if err := st.Write("worker.id", sessionID); err != nil {
				t.Fatal(err)
			}
			taskID, err := st.TaskID()
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := state.CaptureGitSnapshot(cfg.RepoRoot)
			if err != nil {
				t.Fatal(err)
			}
			dirty, err := state.CaptureStopDirtyFiles(cfg.RepoRoot)
			if err != nil {
				t.Fatal(err)
			}
			body, err := json.Marshal(packet.Result{
				Status:              packet.StatusImplemented,
				Risk:                packet.RiskLow,
				Summary:             strings.Repeat("x", packet.MaxFieldBytes+1),
				RequirementCoverage: "covered",
				Tests:               "pass",
				Unverified:          "none",
			})
			if err != nil {
				t.Fatal(err)
			}
			digest := sha256.Sum256(body)
			record, err := json.Marshal(map[string]any{
				"version":    2,
				"task_id":    taskID,
				"role":       "worker",
				"session_id": sessionID,
				"snapshot": state.SnapshotDigest{
					Head: snapshot.Head, IndexDigest: snapshot.IndexDigest,
					WorktreeDigest: snapshot.WorktreeDigest, WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
				},
				"attempts":      1,
				"resubmissions": 1,
				"terminal":      map[string]any{"reason": "repeated_violation"},
			})
			if err != nil {
				t.Fatal(err)
			}
			if err := st.Write(state.ResultCorrectionStateFile, string(record)); err != nil {
				t.Fatal(err)
			}
			st.RecordModelCallLog(state.ModelCallLog{
				CallID: "app-summary-source-call", CallType: state.CallTypeTask, TaskID: taskID,
				SessionID: sessionID, Role: state.WorkerRole, Phase: phase,
				Outcome: "invalid_packet", ResumeSource: "result-format-recovery", ReadOnly: true,
				Response: string(body), ResponseSHA256: hex.EncodeToString(digest[:]),
			})
			if err := st.EnterStop(state.ResumeCheckpoint{
				Stage: state.ResumeStageWorker, Phase: phase, Role: state.WorkerRole, Model: "worker",
				ReadOnly: true, StopKind: state.ResumeStopResultFormat, ResultCorrection: true,
				StopGitSnapshot: &snapshot, StopDirtyFiles: dirty,
			}); err != nil {
				t.Fatal(err)
			}
			handoff := func(t *testing.T, consistent bool) parentHandoffRecoveryOutput {
				t.Helper()
				var output bytes.Buffer
				if err := Execute(Command{Mode: ModeHandoff, Payload: projection}, cfg, nil, &output, io.Discard); err != nil {
					t.Fatal(err)
				}
				var result parentHandoffRecoveryOutput
				if err := json.Unmarshal(output.Bytes(), &result); err != nil {
					t.Fatal(err)
				}
				if result.Consistent != consistent {
					t.Fatalf("handoff consistency = %v want %v: %+v", result.Consistent, consistent, result)
				}
				return result
			}
			advertised := handoff(t, true)
			action := string(state.ParentActionResubmitReport)
			if advertised.RequiredAction == nil || *advertised.RequiredAction != action || !reflect.DeepEqual(advertised.AllowedActions, []string{action}) {
				t.Fatalf("model-free summary recovery was not advertised: %+v", advertised)
			}
			if err := st.Write("worker.id", "another-session"); err != nil {
				t.Fatal(err)
			}
			refused := handoff(t, false)
			if refused.RequiredAction != nil || refused.Inconsistency == nil || !strings.Contains(*refused.Inconsistency, "summary recovery") {
				t.Fatalf("broken recovery evidence still advertised resubmission: %+v", refused)
			}
		})
	}
}

func TestParentHandoffActionSpecs(t *testing.T) {
	t.Parallel()
	specs := parentActionSpecs([]string{
		string(state.ParentActionDecision),
		string(state.ParentActionFix),
		string(state.ParentActionResume),
		string(state.ParentActionApproveSurface),
	}, map[string]string{"accepted-scope": "current-diff"})

	if got := specs[string(state.ParentActionDecision)]; got.Kind != "staged" || !reflect.DeepEqual(got.PrepareCommand, []string{"glm-parent-action", "prepare", "decision"}) {
		t.Fatalf("decision spec = %#v", got)
	}
	fix := specs[string(state.ParentActionFix)]
	if fix.Kind != "staged" || !reflect.DeepEqual(fix.PrepareCommand, []string{"glm-parent-action", "prepare", "fix", "--accepted-scope", "current-diff"}) {
		t.Fatalf("fix spec = %#v", fix)
	}
	if !reflect.DeepEqual(fix.Parameters, map[string]string{"accepted-scope": "current-diff"}) || !reflect.DeepEqual(fix.OptionalParameters, []string{"--origin", "--cause"}) {
		t.Fatalf("fix parameters = %#v optional=%#v", fix.Parameters, fix.OptionalParameters)
	}
	if got := specs[string(state.ParentActionResume)]; got.Kind != "direct" || !reflect.DeepEqual(got.Command, []string{"glm-parent-action", "resume"}) {
		t.Fatalf("resume spec = %#v", got)
	}
	if got := specs[string(state.ParentActionApproveSurface)]; got.Kind != "direct" || !reflect.DeepEqual(got.Command, []string{"glm-parent-action", "approve-surface", "--accepted-scope", "current-diff"}) {
		t.Fatalf("approve spec = %#v", got)
	}
}

func TestParentHandoffFixWithoutRequiredScopeDeclaresAllOptionalMetadata(t *testing.T) {
	t.Parallel()
	specs := parentActionSpecs([]string{string(state.ParentActionFix)}, nil)
	fix := specs[string(state.ParentActionFix)]
	if !reflect.DeepEqual(fix.PrepareCommand, []string{"glm-parent-action", "prepare", "fix"}) {
		t.Fatalf("fix prepare command = %#v", fix.PrepareCommand)
	}
	if len(fix.Parameters) != 0 || !reflect.DeepEqual(fix.OptionalParameters, []string{"--origin", "--cause", "--accepted-scope"}) {
		t.Fatalf("fix parameters = %#v optional=%#v", fix.Parameters, fix.OptionalParameters)
	}
}

func TestParentHandoffJSONIncludesActionSpecs(t *testing.T) {
	t.Parallel()
	required := string(state.ParentActionDecision)
	output := parentHandoffOutput{
		Version:        parentHandoffVersion,
		Consistent:     true,
		RequiredAction: &required,
		AllowedActions: []string{string(state.ParentActionDecision)},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ActionSpecs map[string]parentHandoffActionSpec `json:"action_specs"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	spec := decoded.ActionSpecs[string(state.ParentActionDecision)]
	if spec.Kind != "staged" || !reflect.DeepEqual(spec.PrepareCommand, []string{"glm-parent-action", "prepare", "decision"}) {
		t.Fatalf("handoff action spec = %#v", spec)
	}
}

func TestParentHandoffRecoveryJSONIncludesActionSpecs(t *testing.T) {
	t.Parallel()
	output := parentHandoffRecoveryOutput{
		Version:        parentHandoffVersion,
		Consistent:     true,
		AllowedActions: []string{string(state.ParentActionResume)},
	}
	raw, err := json.Marshal(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded struct {
		ActionSpecs map[string]parentHandoffActionSpec `json:"action_specs"`
	}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatal(err)
	}
	if got := decoded.ActionSpecs[string(state.ParentActionResume)].Command; !reflect.DeepEqual(got, []string{"glm-parent-action", "resume"}) {
		t.Fatalf("recovery resume command = %#v", got)
	}
}
