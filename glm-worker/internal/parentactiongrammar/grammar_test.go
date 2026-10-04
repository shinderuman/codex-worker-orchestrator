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
			name:   "controller publication",
			action: "controller-publication",
			want:   Spec{Kind: "staged", PrepareCommand: []string{Binary, "prepare", "controller-publication"}},
		},
		{
			name:   "observation execute",
			action: string(state.ParentActionObservationExecute),
			want: Spec{
				Kind:               "staged",
				PrepareCommand:     []string{Binary, "prepare", string(state.ParentActionObservationExecute)},
				RequiredParameters: []string{"operation"},
				OptionalParameters: []string{"reference", "working-dir", "deadline-ms"},
				Choices: map[string][]string{
					"operation": {"shadow-eval", "go-test", "go-test-race"},
				},
			},
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
			name:   "accept",
			action: string(state.ParentActionAccept),
			want:   Spec{Kind: "direct", Command: []string{Binary, "accept"}},
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

func TestProjectRejectsRetiredExternalActions(t *testing.T) {
	for _, action := range []state.ParentAction{
		state.ParentActionImprovementDisposition,
		state.ParentActionBindDefectTask,
		state.ParentActionReopen,
		state.ParentActionComplete,
		state.ParentActionInstall,
		state.ParentActionNoGo,
	} {
		t.Run(string(action), func(t *testing.T) {
			if _, ok := Project(string(action), nil); ok {
				t.Fatal("retired external action was projected")
			}
		})
	}
}

func TestProjectRejectsMissingMachineParameters(t *testing.T) {
	for _, action := range []string{string(state.ParentActionApproveSurface), "unknown"} {
		t.Run(action, func(t *testing.T) {
			if _, ok := Project(action, nil); ok {
				t.Fatal("action projected without required machine parameters")
			}
		})
	}
}

func TestApproveSurfaceParserFailsClosedOnMalformedShapes(t *testing.T) {
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
}
