package app

import "testing"

func TestQualityGateFailureDetailIncludesRunIdentity(t *testing.T) {
	runID := "0123456789abcdef0123456789abcdef"
	body := buildProcessError(&QualityGateError{
		ValidationRunID: runID,
		Form:            "go-test",
		Command:         "go test ./...",
		WorkingDir:      "/repo/glm-worker",
		ExitCode:        3,
		DurationMS:      10,
		LogPath:         "/state/quality-gate-runs/" + runID + "/gate.log",
	})
	if body.Kind != errorKindQualityGateFailed {
		t.Fatalf("kind = %q", body.Kind)
	}
	if body.Detail["validation_run_id"] != runID {
		t.Fatalf("validation_run_id = %#v", body.Detail["validation_run_id"])
	}
}
