package parentactioncmd

import "testing"

func TestObservationExecuteRequiresControllerGuard(t *testing.T) {
	descriptor, ok := lookupParentActionCommand(actionObservationExecute)
	if !ok {
		t.Fatal("observation-execute descriptor is missing")
	}
	if !parentActionNeedsControllerGuard(descriptor, parentActionExecutionObservationExecute) {
		t.Fatal("observation-execute bypassed controller lease admission")
	}
}

func TestReadOnlyGitEvidenceDoesNotRequireControllerGuard(t *testing.T) {
	descriptor, ok := lookupParentActionCommand("finalize-check")
	if !ok {
		t.Fatal("finalize-check descriptor is missing")
	}
	if parentActionNeedsControllerGuard(descriptor, parentActionExecutionGitEvidence) {
		t.Fatal("read-only finalize-check unexpectedly requires mutation admission")
	}
}
