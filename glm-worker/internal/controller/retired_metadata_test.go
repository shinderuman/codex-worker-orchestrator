package controller

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func retiredMetadataFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, ExecutionOperationResult) {
	t.Helper()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "result.txt", "retired result\n")
	retired, _ := publishTerminalTestSource(t, fixture, policy, source)
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "add", "--all")
	return fixture, policy, retired
}

func TestRetiredMetadataAdvancementPreservesTerminalEvidenceAndStartsSuccessor(t *testing.T) {
	t.Parallel()
	fixture, policy, retired := retiredMetadataFixture(t)
	repo := fixture.store.identity.PrimaryRoot
	terminalBefore, err := fixture.store.LoadEvidenceObject(*retired.Head.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	task, canonical, err := WorkflowAuthorityTask(fixture.store.config)
	if err != nil || !canonical || !task.Equal(*retired.Head.RootTaskRef) {
		t.Fatalf("retired read authority: %v %v %v", task, canonical, err)
	}
	current := retired
	for index := 0; index < 2; index++ {
		content, err := os.ReadFile(filepath.Join(repo, current.Head.RootTaskRef.TaskPath))
		if err != nil {
			t.Fatal(err)
		}
		writeSuspensionTestFile(t, repo, current.Head.RootTaskRef.TaskPath, string(content)+"\n親metadataの追加要求\n")
		runControllerGit(t, repo, "add", current.Head.RootTaskRef.TaskPath)
		runControllerGit(t, repo, "commit", "-qm", "parent metadata amendment")
		if index == 0 {
			runControllerGit(t, repo, "push", "origin", policy.LocalRef+":"+policy.RemoteRef)
		}
		selected, canonical, err := WorkflowAuthorityTask(fixture.store.config)
		if err != nil || !canonical || selected.TaskPath != task.TaskPath || selected.Equal(*current.Head.RootTaskRef) {
			t.Fatalf("amended read authority: %v %v %v", selected, canonical, err)
		}
		if _, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: current.Head.ControllerGeneration}); err == nil {
			t.Fatal("stale project authority started execution")
		}
		current, err = fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: current.Head.ControllerGeneration, Policy: policy})
		if err != nil {
			t.Fatal(err)
		}
		if current.Head.LiveLeaseID != "" || current.Head.ExecutionTaskRef != nil || !current.Head.RootTaskRef.Equal(selected) || !evidenceRefsEqual(*retired.Head.MetadataLineageRef, *current.Head.MetadataLineageRef) {
			t.Fatal("metadata adoption changed terminal or minted execution authority")
		}
		if _, err := fixture.store.RecoverExecutionOperation(current.TransitionID); err != nil {
			t.Fatal(err)
		}
		project, err := fixture.store.retiredProjectAuthority(current.Head)
		if err != nil || project.Snapshot.HeadOID != controllerGitOutput(t, repo, "rev-parse", "HEAD") {
			t.Fatalf("adopted project authority: %v", err)
		}
		if proven, err := fixture.store.terminalAttemptProven(fixture.source.Attempt); err != nil || !proven {
			t.Fatalf("metadata advancement lost completed Task publication proof: proven=%v err=%v", proven, err)
		}
	}
	terminalAfter, err := fixture.store.LoadEvidenceObject(*current.Head.MetadataLineageRef)
	if err != nil || !reflect.DeepEqual(terminalBefore, terminalAfter) {
		t.Fatal("old terminal record changed")
	}
	if _, err := fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: retired.Head.ControllerGeneration, Policy: policy}); err == nil {
		t.Fatal("stale generation adopted advancement")
	}
	materialized, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: current.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if materialized.Admission == nil || !materialized.Admission.Attempt.SemanticTaskRef.Equal(*current.Head.RootTaskRef) {
		t.Fatal("successor execution was not bound to amended authority")
	}
}

func TestRetiredAuthorityPreservesHistoricalUserTermination(t *testing.T) {
	t.Parallel()
	fixture, _, retired := retiredMetadataFixture(t)
	project, err := fixture.store.LoadProjectSnapshot(fixture.source.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	record := UserTaskTermination{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: fixture.store.identity.LineageID, TransitionID: "historical-termination", SourceHead: fixture.source.Head, SuccessorProject: project, SuccessorTask: fixture.source.Attempt.SemanticTaskRef, Reason: "historical incomplete execution"}
	ref, err := fixture.store.putEvidenceJSON("user-task-termination", "historical-termination", record)
	if err != nil {
		t.Fatal(err)
	}
	before, err := fixture.store.LoadEvidenceObject(ref)
	if err != nil {
		t.Fatal(err)
	}
	head := retired.Head
	head.UserTerminationRef = &ref
	task, canonical, err := fixture.store.quiescentWorkflowAuthority(fixture.store.config, head, nil)
	if err != nil || !canonical || !task.Equal(*retired.Head.RootTaskRef) {
		t.Fatalf("retired successor with historical termination: %v %v %v", task, canonical, err)
	}
	after, err := fixture.store.LoadEvidenceObject(ref)
	if err != nil || !reflect.DeepEqual(before, after) || !evidenceRefsEqual(*head.UserTerminationRef, ref) {
		t.Fatal("historical incomplete execution evidence changed")
	}
	head.ProjectSnapshotID = project.SnapshotID
	if _, _, err := fixture.store.quiescentWorkflowAuthority(fixture.store.config, head, nil); err == nil {
		t.Fatal("historical termination admitted stale retired project authority")
	}
}

func TestRetiredMetadataAdvancementRejectsNonMetadataHistoryBeforeTransition(t *testing.T) {
	for _, mode := range []string{"source", "reverted-source", "symlink", "active-switch", "policy"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			fixture, policy, retired := retiredMetadataFixture(t)
			repo := fixture.store.identity.PrimaryRoot
			switch mode {
			case "source", "reverted-source":
				writeSuspensionTestFile(t, repo, "result.txt", "external source change\n")
				runControllerGit(t, repo, "add", "result.txt")
				runControllerGit(t, repo, "commit", "-qm", "external source")
				if mode == "reverted-source" {
					runControllerGit(t, repo, "revert", "--no-edit", "HEAD")
				}
			case "symlink":
				if err := os.Symlink("../result.txt", filepath.Join(repo, "IMPLEMENTATION_TASKS", "extra.md")); err != nil {
					t.Fatal(err)
				}
				runControllerGit(t, repo, "add", "IMPLEMENTATION_TASKS/extra.md")
				runControllerGit(t, repo, "commit", "-qm", "external metadata symlink")
			case "active-switch":
				plan, err := os.ReadFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"))
				if err != nil {
					t.Fatal(err)
				}
				writeSuspensionTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", strings.ReplaceAll(string(plan), retired.Head.RootTaskRef.TaskPath, "IMPLEMENTATION_TASKS/missing.md"))
				runControllerGit(t, repo, "add", "IMPLEMENTATION_PLAN.local.md")
				runControllerGit(t, repo, "commit", "-qm", "invalid active task")
			case "policy":
				policy.RemoteRef = "refs/heads/unowned"
			}
			if mode != "policy" {
				runControllerGit(t, repo, "push", "origin", policy.LocalRef+":"+policy.RemoteRef)
				if _, _, err := WorkflowAuthorityTask(fixture.store.config); err == nil {
					t.Fatal("invalid advancement supplied bootstrap authority")
				}
			}
			if _, err := fixture.store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: retired.Head.ControllerGeneration, Policy: policy}); err == nil {
				t.Fatal("invalid external advancement adopted")
			}
			head, err := fixture.store.LoadHead()
			if err != nil || !reflect.DeepEqual(head, retired.Head) {
				t.Fatal("rejected advancement changed controller authority")
			}
		})
	}
}
