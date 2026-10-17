package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func retiredLaneFixture(t *testing.T) (findingAcceptanceFixture, ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	fixture, policy := configurePublicationTestFixture(t, newFindingAcceptanceFixture(t, "IMPLEMENTATION_TASKS/third.md"))
	source := publicationTestEdit(t, fixture, "first.txt", "first result\n")
	retired, _ := publishTerminalTestSource(t, fixture, policy, source)
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "add", "--all")
	lane, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	fixture.source = *lane.Admission
	writeSuspensionTestFile(t, fixture.source.Workspace.Root, "staged.txt", "staged data\n")
	runControllerGit(t, fixture.source.Workspace.Root, "add", "staged.txt")
	source = publicationTestEdit(t, fixture, "staged.txt", "unstaged data\n")
	retired, candidate := publishTerminalTestSource(t, fixture, policy, source)
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "add", "--all")
	return fixture, retired, candidate
}

func TestRetiredLaneNormalStartRetainsDataAndReusesExactPath(t *testing.T) {
	t.Parallel()
	fixture, retired, candidate := retiredLaneFixture(t)
	old := fixture.source.Workspace
	parent := retired.Head.RootTaskRef.TaskPath
	content, err := os.ReadFile(filepath.Join(fixture.store.identity.PrimaryRoot, parent))
	if err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, fixture.store.identity.PrimaryRoot, parent, string(content)+"\n次Taskの追加要求\n")
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "add", parent)
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "commit", "--only", "-qm", "amend next task before lane cleanup", "--", parent)
	retired, err = fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: retired.Head.ControllerGeneration, Policy: *retired.Head.PublicationPolicy})
	if err != nil {
		t.Fatal(err)
	}
	started, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if started.Admission == nil || started.Admission.Workspace.Root != old.Root || started.Admission.Workspace.ID == old.ID {
		t.Fatal("ordinary start did not replace exact retired lane nonce")
	}
	if !started.Admission.Attempt.SemanticTaskRef.Equal(*retired.Head.RootTaskRef) {
		t.Fatal("successor Task binding changed")
	}
	if _, err := fixture.store.ProveCleanupDurability(candidate.SealRef); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.AdmitMutation(fixture.source.MutationAuthority(), started.Admission.Workspace, started.Admission.Snapshot); err == nil {
		t.Fatal("old authority reused new lane")
	}
	if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: started.Head.ControllerGeneration}); err == nil {
		t.Fatal("live execution was cleaned")
	}
}

func TestRetiredLaneRejectsSourceDriftAndUnarchivedIgnoredData(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"source", "ignored", "foreign-nonce"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			fixture, retired, _ := retiredLaneFixture(t)
			root := fixture.source.Workspace.Root
			switch mode {
			case "source":
				writeSuspensionTestFile(t, root, "staged.txt", "unsealed data\n")
			case "ignored":
				writeSuspensionTestFile(t, root, ".gitignore", "ignored.txt\n")
				writeSuspensionTestFile(t, root, "ignored.txt", "must survive\n")
			case "foreign-nonce":
				foreign := fixture.source.Workspace
				foreign.ID = "foreign-workspace"
				if err := writeJSONAtomic(filepath.Join(foreign.GitDir, executionWorkspaceIdentityFile), foreign); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration}); err == nil {
				t.Fatal("unproven occupied lane was removed")
			}
			if _, err := os.Stat(root); err != nil {
				t.Fatal("rejected lane data was deleted")
			}
			head, err := fixture.store.LoadHead()
			if err != nil || head.ControllerGeneration != retired.Head.ControllerGeneration || head.PendingTransitionID != "" {
				t.Fatal("preflight changed controller authority")
			}
		})
	}
}

func TestCleanupRetentionRestoresStagesAndStopsOnParentDataRace(t *testing.T) {
	t.Parallel()
	fixture, retired, candidate := retiredLaneFixture(t)
	root := fixture.source.Workspace.Root
	parent := fixture.source.Attempt.SemanticTaskRef.TaskPath
	original, err := os.ReadFile(filepath.Join(root, parent))
	if err != nil {
		t.Fatal(err)
	}
	retained := string(original) + "\n親の保存対象\n"
	writeSuspensionTestFile(t, root, parent, retained)
	op, head, err := fixture.store.planExecutionCleanup(CleanupExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration, WorkspaceID: fixture.source.Workspace.ID, SealRef: candidate.SealRef})
	if err != nil {
		t.Fatal(err)
	}
	if op.CleanupTrees == nil || op.CleanupArchive == nil || op.CleanupTrees.IndexTree == op.CleanupTrees.WorktreeTree {
		t.Fatal("stage distinction was not retained")
	}
	data, err := fixture.store.LoadEvidenceObject(*op.CleanupArchive)
	if err != nil {
		t.Fatal(err)
	}
	var archive gitObjectArchiveEnvelope
	if err := json.Unmarshal(data, &archive); err != nil {
		t.Fatal(err)
	}
	restore := t.TempDir()
	runControllerGit(t, restore, "init", "-q")
	if _, err := runGitBinary(restore, archive.Pack, "unpack-objects", "-r"); err != nil {
		t.Fatal(err)
	}
	for _, check := range []struct{ tree, path, value string }{{op.CleanupTrees.IndexTree, "staged.txt", "staged data"}, {op.CleanupTrees.WorktreeTree, "staged.txt", "unstaged data"}, {op.CleanupTrees.WorktreeTree, parent, strings.TrimSpace(retained)}} {
		if got := controllerGitOutput(t, restore, "show", check.tree+":"+check.path); got != check.value {
			t.Fatal("portable archive failed to restore exact data")
		}
	}
	if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	writeSuspensionTestFile(t, root, parent, retained+"changed\n")
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("post-archive parent data race was deleted")
	}
	pending, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: pending.ControllerGeneration}); err == nil {
		t.Fatal("pending transition was released")
	}
	writeSuspensionTestFile(t, root, parent, retained)
	cleaned, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: cleaned.Head.ControllerGeneration}); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("old cleanup claimed recreated lane")
	}
}
