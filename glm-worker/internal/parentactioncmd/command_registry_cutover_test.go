package parentactioncmd

import (
	"io"
	"testing"
)

func TestParentActionCommandRegistryLiveAndUnknown(t *testing.T) {
	t.Setenv("CODEX_THREAD_ID", "01a0463c-d477-7410-9efd-cb34ff2e0b0e")
	t.Setenv("CODEX_SESSION_ID", "01a0244a-4ee4-7e71-b2e1-dec3bdda2120")
	cfg := newControllerActionTestConfig(t, false)
	wantRejections := map[string]string{
		"resync-source":                 "usage: glm-parent-action resync-source",
		"rotation-claim":                "usage: glm-parent-action rotation-claim <directive-id>",
		"rotation-bind":                 "usage: glm-parent-action rotation-bind <directive-id> <claim-id> <new-thread-id>",
		"rotation-fail":                 sessionRotationFailUsage,
		"wait":                          "usage: glm-parent-action wait",
		actionContinuationStopHook:      "usage: glm-parent-action <continuation-stop-hook|continuation-metadata-guard>",
		actionContinuationMetadataGuard: "usage: glm-parent-action <continuation-stop-hook|continuation-metadata-guard>",
		actionApprove:                   approveSurfaceUsage,
		actionStart:                     "usage: glm-parent-action start",
		actionAccept:                    "usage: glm-parent-action accept",
		actionResume:                    "usage: glm-parent-action resume",
		actionResubmitReport:            "usage: glm-parent-action resubmit-report",
		"evidence":                      "usage: glm-parent-action evidence <manifest.json>",
		"export-bundle":                 exportBundleUsage,
		"finalize-check":                "usage: glm-parent-action finalize-check <go-test|go-test-race>",
		actionObservationExecute:        observationExecuteUsage,
	}
	if len(wantRejections) != len(parentActionCommands) {
		t.Fatalf("live dispatch proof covers %d of %d registered actions", len(wantRejections), len(parentActionCommands))
	}
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
		want, ok := wantRejections[action]
		if !ok {
			t.Fatalf("registered action %q lacks a live dispatch probe", action)
		}
		t.Run(action, func(t *testing.T) {
			args := []string{action, "--cutover-invalid", "extra"}
			err := execute(cfg, args, io.Discard, io.Discard)
			if err == nil || err.Error() != want || err.Error() == usage {
				t.Fatalf("normal dispatch for %q: %v, want handler result %q", action, err, want)
			}
			if action == actionStart {
				want = explicitSingleStartUsage
			}
			err = executeParentActionCommand(cfg, descriptor, args, io.Discard, io.Discard, true)
			if err == nil || err.Error() != want || err.Error() == usage {
				t.Fatalf("terminal dispatch for %q: %v, want handler result %q", action, err, want)
			}
		})
	}
	for _, unknown := range []string{"", "retired-parent-action", "old-parent-action", "resync-source-alias"} {
		if descriptor, ok := lookupParentActionCommand(unknown); ok {
			t.Fatalf("unknown action %q was admitted as %+v", unknown, descriptor)
		}
	}
}
