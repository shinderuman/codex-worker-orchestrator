package parentaction

import "testing"

func TestLookupPayloadActionOwnsCurrentTransportMapping(t *testing.T) {
	for action, wantMode := range map[string]string{
		"decision":          "--decision-stdin",
		"fix":               "--fix-stdin",
		"start-milestones":  "--execution-milestones-stdin",
		"revise-milestones": "--execution-milestones-revise-stdin",
	} {
		descriptor, ok := LookupPayloadAction(action)
		if !ok {
			t.Fatalf("%s descriptor missing", action)
		}
		if string(descriptor.Action) != action || descriptor.WorkerMode != wantMode {
			t.Fatalf("%s descriptor = %#v", action, descriptor)
		}
	}
}

func TestLookupPayloadActionRejectsUnknownAndDirectActions(t *testing.T) {
	for _, action := range []string{"", "unknown", "start", "accept", "resume", "no-go", "finalize-check"} {
		if _, ok := LookupPayloadAction(action); ok {
			t.Fatalf("non-staged action %q unexpectedly has payload descriptor", action)
		}
	}
}
