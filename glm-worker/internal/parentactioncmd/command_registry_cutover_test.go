package parentactioncmd

import "testing"

func TestParentActionCommandRegistryLiveAndUnknown(t *testing.T) {
	for action, expected := range parentActionCommands {
		if action != expected.Action {
			t.Fatalf("registry key %q does not match descriptor action %q", action, expected.Action)
		}
		descriptor, ok := lookupParentActionCommand(action)
		if !ok || descriptor.Action != action {
			t.Fatalf("canonical action %q is not resolved: %+v, %v", action, descriptor, ok)
		}
		if descriptor.Execute == parentActionExecutionUnsupported ||
			descriptor.TerminalExecute == parentActionExecutionUnsupported {
			t.Fatalf("canonical action %q has no executable/terminal kind: %+v", action, descriptor)
		}
	}
	for _, unknown := range []string{"", "retired-parent-action", "old-parent-action", "resync-source-alias"} {
		if descriptor, ok := lookupParentActionCommand(unknown); ok {
			t.Fatalf("unknown action %q was admitted as %+v", unknown, descriptor)
		}
	}
}
