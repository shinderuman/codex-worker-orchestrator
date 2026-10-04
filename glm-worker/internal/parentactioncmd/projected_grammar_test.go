package parentactioncmd

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentaction"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/parentactiongrammar"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestProjectedParentActionGrammarIsExecutable(t *testing.T) {
	cases := []struct {
		action     string
		parameters map[string]string
	}{
		{action: string(state.ParentActionDecision)},
		{action: string(state.ParentActionFix), parameters: map[string]string{parentactiongrammar.AcceptedScopeParameter: "current-diff"}},
		{action: string(parentaction.ActionReviseMilestones)},
		{action: string(state.ParentActionApproveSurface), parameters: map[string]string{parentactiongrammar.AcceptedScopeParameter: "current-diff"}},
		{action: string(state.ParentActionAccept)},
		{action: string(state.ParentActionResume)},
	}

	for _, tc := range cases {
		t.Run(tc.action, func(t *testing.T) {
			spec, ok := parentactiongrammar.Project(tc.action, tc.parameters)
			if !ok {
				t.Fatal("action was not projected")
			}
			switch spec.Kind {
			case "staged":
				assertProjectedPrepareCommandAccepted(t, tc.action, spec.PrepareCommand)
			case "direct":
				assertProjectedDirectCommandAccepted(t, tc.action, spec.Command)
			default:
				t.Fatalf("unexpected projected kind %q", spec.Kind)
			}
		})
	}
}

func assertProjectedPrepareCommandAccepted(t *testing.T, action string, command []string) {
	t.Helper()
	if len(command) < 3 || command[0] != parentactiongrammar.Binary || command[1] != "prepare" || command[2] != action {
		t.Fatalf("prepare command = %#v", command)
	}
	if _, ok := parentaction.LookupPayloadAction(action); !ok {
		t.Fatalf("projected staged action %q has no payload owner", action)
	}
	options := command[3:]
	if action == string(parentaction.ActionFix) {
		if err := validateFixOptions(options); err != nil {
			t.Fatalf("projected fix options rejected: %v", err)
		}
		return
	}
	if len(options) != 0 {
		t.Fatalf("non-fix prepare projected options: %#v", options)
	}
}

func assertProjectedDirectCommandAccepted(t *testing.T, action string, command []string) {
	t.Helper()
	if len(command) < 2 || command[0] != parentactiongrammar.Binary || command[1] != action {
		t.Fatalf("direct command = %#v", command)
	}
	if _, ok := lookupParentActionCommand(action); !ok {
		t.Fatalf("projected direct action %q has no executable command owner", action)
	}
	if action == string(state.ParentActionApproveSurface) {
		if !parentactiongrammar.ValidateApproveSurfaceArgs(command[2:]) {
			t.Fatalf("projected approve command rejected: %#v", command)
		}
		return
	}
	if len(command) != 2 {
		t.Fatalf("no-argument action projected extra argv: %#v", command)
	}
}
