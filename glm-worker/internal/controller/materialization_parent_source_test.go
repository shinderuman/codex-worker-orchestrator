package controller

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMaterializationParentSourceStopsAtRebindBoundary(t *testing.T) {
	t.Parallel()
	f, policy, head, old := acceptedAmendmentLane(t)
	root := f.store.identity.PrimaryRoot
	task := filepath.Join(root, old.TaskRef.TaskPath)
	data, err := os.ReadFile(task)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(task, append(data, []byte("\n## Amendments\n\nparent source revision\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, root, "parent-result.txt", "parent implementation\n")
	snapshot, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		t.Fatal(err)
	}
	revised, err := f.store.RebindParentSourceCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: old.CandidateID}, snapshot.ID)
	if err != nil {
		t.Fatal(err)
	}
	revisedCandidate, err := f.store.LoadAcceptedCandidate(*revised.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(root, "external-metadata.txt"), []byte("metadata-only external advancement\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, root, "add", old.TaskRef.TaskPath, "parent-result.txt", "external-metadata.txt")
	runControllerGit(t, root, "commit", "-qm", "commit the revised source and advance externally")
	runControllerGit(t, root, "push", policy.Remote, "HEAD:"+policy.RemoteRef)
	tip := controllerGitOutput(t, root, "rev-parse", "HEAD")
	advanced, err := f.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: revised.Head.ControllerGeneration, CandidateID: revisedCandidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	advancedCandidate, err := f.store.LoadAcceptedCandidate(*advanced.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if advancedCandidate.BaseOID != tip || advancedCandidate.BaseOID == revisedCandidate.BaseOID || advancedCandidate.TreeOID == revisedCandidate.TreeOID {
		t.Fatalf("external advancement rebind kept the parent-source interval: %#v", advancedCandidate)
	}

	entered, err := f.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: advanced.Head.ControllerGeneration, CandidateID: advancedCandidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	live, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: entered.Head.ControllerGeneration, SuspensionID: entered.Suspension.SnapshotID})
	if err != nil {
		t.Fatalf("legitimate rebind materialization stopped on the stale parent source: %v", err)
	}
	materialized := map[string]string{
		"external-metadata.txt": "metadata-only external advancement\n",
		"parent-result.txt":     "parent implementation\n",
		"result.txt":            "task result\n",
	}
	for path, want := range materialized {
		got, err := os.ReadFile(filepath.Join(live.Admission.Workspace.Root, path))
		if err != nil {
			t.Fatalf("materialized source lacks %s: %v", path, err)
		}
		if string(got) != want {
			t.Fatalf("materialized source differs at %s: %q != %q", path, got, want)
		}
	}
}
