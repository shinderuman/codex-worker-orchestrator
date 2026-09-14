package parentactioncmd

import (
	"os"
	"testing"
)

func TestCompleteDoesNotRequireParentOutcomeTelemetry(t *testing.T) {
	fixture := newCompleteFixture(t)
	taskID, err := fixture.st.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fixture.st.ModelCallLogPath(taskID)); err != nil {
		t.Fatal(err)
	}
	fixture.commitParentMetadataSync(t)
	runFinalizationGit(t, fixture.repo, "push", "-q", "origin", "main")

	output := runCompleteCommand(t, fixture)
	if output.Status != completeStatusComplete || !output.Completed {
		t.Fatalf("completion without telemetry = %#v", output)
	}
}
