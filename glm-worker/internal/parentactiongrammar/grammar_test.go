package parentactiongrammar

import (
	"reflect"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestProjectPreservesCurrentActionSpecs(t *testing.T) {
	cases := []struct {
		name       string
		action     string
		parameters map[string]string
		want       Spec
	}{
		{
			name:   "decision",
			action: string(state.ParentActionDecision),
			want:   Spec{Kind: "staged", PrepareCommand: []string{Binary, "prepare", "decision"}},
		},
		{
			name:   "revise milestones",
			action: string(parentaction.ActionReviseMilestones),
			want:   Spec{Kind: "staged", PrepareCommand: []string{Binary, "prepare", "revise-milestones"}},
		},
		{
			name:   "fix",
			action: string(state.ParentActionFix),
			parameters: map[string]string{
				AcceptedScopeParameter: "current-diff",
			},
			want: Spec{
				Kind:               "staged",
				PrepareCommand:     []string{Binary, "prepare", "fix", AcceptedScopeOption, "current-diff"},
				Parameters:         map[string]string{AcceptedScopeParameter: "current-diff"},
				OptionalParameters: []string{"--origin", "--cause"},
			},
		},
		{
			name:   "approve surface",
			action: string(state.ParentActionApproveSurface),
			parameters: map[string]string{
				AcceptedScopeParameter: "current-diff",
			},
			want: Spec{
				Kind:       "direct",
				Command:    []string{Binary, "approve-surface", AcceptedScopeOption, "current-diff"},
				Parameters: map[string]string{AcceptedScopeParameter: "current-diff"},
			},
		},
		{
			name:   "bind defect task",
			action: string(state.ParentActionBindDefectTask),
			parameters: map[string]string{
				TaskParameter: "IMPLEMENTATION_TASKS/follow-up.md",
			},
			want: Spec{
				Kind:       "direct",
				Command:    []string{Binary, "bind-defect-task", TaskOption, "IMPLEMENTATION_TASKS/follow-up.md"},
				Parameters: map[string]string{TaskParameter: "IMPLEMENTATION_TASKS/follow-up.md"},
			},
		},
		{
			name:   "resume",
			action: string(state.ParentActionResume),
			want:   Spec{Kind: "direct", Command: []string{Binary, "resume"}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := Project(tc.action, tc.parameters)
			if !ok {
				t.Fatal("action was not projected")
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("spec = %#v, want %#v", got, tc.want)
			}
		})
	}
}

func TestProjectImprovementDispositionUsesExecutableGrammar(t *testing.T) {
	parameters := map[string]string{
		SignalKindParameter:   state.ImprovementSignalInvalidPacket,
		SourceCallIDParameter: "44444444-4444-4444-8444-444444444444",
		"signal-count":        "2",
	}
	got, ok := Project(string(state.ParentActionImprovementDisposition), parameters)
	if !ok {
		t.Fatal("improvement disposition was not projected")
	}
	wantCommand := []string{
		Binary,
		ImprovementDispositionAction,
		SignalKindOption,
		state.ImprovementSignalInvalidPacket,
		SourceCallIDOption,
		parameters[SourceCallIDParameter],
	}
	if got.Kind != "bounded-choice" || !reflect.DeepEqual(got.Command, wantCommand) {
		t.Fatalf("improvement spec = %#v", got)
	}
	if !reflect.DeepEqual(got.RequiredParameters, []string{DispositionOption}) || !reflect.DeepEqual(got.OptionalParameters, []string{TaskOption}) {
		t.Fatalf("improvement parameters = required %#v optional %#v", got.RequiredParameters, got.OptionalParameters)
	}
	if !reflect.DeepEqual(got.Choices[DispositionOption], state.ImprovementSignalDispositionChoices()) {
		t.Fatalf("disposition choices = %#v", got.Choices)
	}
	if !reflect.DeepEqual(got.Parameters, parameters) {
		t.Fatalf("parameters = %#v, want %#v", got.Parameters, parameters)
	}
}

func TestProjectRejectsMissingMachineParameters(t *testing.T) {
	for _, tc := range []struct {
		name   string
		action string
	}{
		{name: "approve surface", action: string(state.ParentActionApproveSurface)},
		{name: "bind defect task", action: string(state.ParentActionBindDefectTask)},
		{name: "improvement disposition", action: string(state.ParentActionImprovementDisposition)},
		{name: "unsupported", action: "unknown"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := Project(tc.action, nil); ok {
				t.Fatal("action projected without required machine parameters")
			}
		})
	}
}

func TestExecutableParsersFailClosedOnMalformedShapes(t *testing.T) {
	if !ValidateApproveSurfaceArgs([]string{AcceptedScopeOption, "current-diff"}) {
		t.Fatal("valid approve-surface args rejected")
	}
	for _, args := range [][]string{
		{},
		{AcceptedScopeOption},
		{AcceptedScopeOption, "other"},
		{"--other", "current-diff"},
	} {
		if ValidateApproveSurfaceArgs(args) {
			t.Fatalf("malformed approve-surface args accepted: %#v", args)
		}
	}

	if action, task, ok := ParseDefectRegistrationArgs([]string{"bind-defect-task", TaskOption, "IMPLEMENTATION_TASKS/follow-up.md"}); !ok || action != "bind-defect-task" || task != "IMPLEMENTATION_TASKS/follow-up.md" {
		t.Fatalf("valid defect args = action %q task %q ok %t", action, task, ok)
	}
	for _, args := range [][]string{
		{"bind-defect-task", "IMPLEMENTATION_TASKS/follow-up.md"},
		{"bind-defect-task", "--other", "IMPLEMENTATION_TASKS/follow-up.md"},
		{"unknown", TaskOption, "IMPLEMENTATION_TASKS/follow-up.md"},
	} {
		if _, _, ok := ParseDefectRegistrationArgs(args); ok {
			t.Fatalf("malformed defect args accepted: %#v", args)
		}
	}
}

func TestImprovementDispositionParserMatchesProjectedShape(t *testing.T) {
	parameters := map[string]string{
		SignalKindParameter:   state.ImprovementSignalInvalidPacket,
		SourceCallIDParameter: "44444444-4444-4444-8444-444444444444",
	}
	spec, ok := Project(string(state.ParentActionImprovementDisposition), parameters)
	if !ok {
		t.Fatal("improvement disposition was not projected")
	}
	args := append([]string(nil), spec.Command[1:]...)
	args = append(args, DispositionOption, string(state.ImprovementSignalDispositionReject))
	kind, sourceCallID, disposition, taskPath, err := ParseImprovementDispositionArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if kind != state.ImprovementSignalInvalidPacket || sourceCallID != parameters[SourceCallIDParameter] || disposition != string(state.ImprovementSignalDispositionReject) || taskPath != "" {
		t.Fatalf("parsed improvement args = %q %q %q %q", kind, sourceCallID, disposition, taskPath)
	}

	malformed := append([]string(nil), args...)
	malformed[1] = "--wrong"
	if _, _, _, _, err := ParseImprovementDispositionArgs(malformed); err == nil {
		t.Fatal("malformed improvement option was accepted")
	}
}
