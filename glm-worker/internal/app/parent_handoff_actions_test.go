package app

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParentHandoffResultFormatStopIncludesResubmitReport(t *testing.T) {
	for _, projection := range []string{"", "recovery"} {
		t.Run(projection, func(t *testing.T) {
			cfg := newAppConfig(t)
			st := startParentHandoffTask(t, cfg)
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

func TestParentHandoffActionSpecs(t *testing.T) {
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
