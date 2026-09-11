package state

import (
	"strings"
	"testing"
	"time"
)

func TestSessionRotationMarkerRejectsCreationOutcomeFromPreviousDirective(t *testing.T) {
	marker := &SessionRotationMarker{
		Version:        sessionRotationMarkerVersion,
		ParentThreadID: "01a0463c-d477-7410-9efd-cb34ff2e0b0e",
		State:          SessionRotationStatePending,
		Directive: &SessionRotationDirective{
			DirectiveID: "aaaaaaaa-bbbb-4ccc-8ddd-eeeeeeeeeeee",
			TaskID:      "12345678-aaaa-bbbb-cccc-dddddddddddd",
			Terminal:    SessionRotationTerminalAccept,
			Epoch:       "12345678-aaaa-bbbb-cccc-dddddddddddd:accept",
			Reason:      SessionRotationReasonCompaction,
			CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		},
		LastCreationOutcome: &SessionRotationCreationOutcome{
			Version:        SessionRotationCreationResultVersion,
			Outcome:        SessionRotationCreationOutcomeFailed,
			ParentThreadID: "01a0463c-d477-7410-9efd-cb34ff2e0b0e",
			DirectiveID:    "bbbbbbbb-cccc-4ddd-8eee-ffffffffffff",
			ClaimID:        "cccccccc-dddd-4eee-8fff-aaaaaaaaaaaa",
			TargetTaskID:   "dddddddd-eeee-4fff-8aaa-bbbbbbbbbbbb",
			RecordedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		},
		UpdatedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}

	err := marker.validate()
	if err == nil || !strings.Contains(err.Error(), "current directive") {
		t.Fatalf("stale creation outcome was accepted: %v", err)
	}
}
