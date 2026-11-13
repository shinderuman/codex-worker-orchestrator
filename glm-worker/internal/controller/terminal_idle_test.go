package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLastTaskCompletionPublishesIdleProject(t *testing.T) {
	for _, blocked := range []bool{false, true} {
		t.Run(map[bool]string{false: "exhausted", true: "blocked"}[blocked], func(t *testing.T) {
			fixture := newFindingAcceptanceFixture(t)
			repo := fixture.source.Workspace.Root
			plan := "## ACTIVE\n\n- `IMPLEMENTATION_TASKS/root.md`\n\n## NEXT\n\n## BLOCKED / USER_PERMISSION_WAIT\n\n"
			if blocked {
				plan += "- `" + fixture.child.TaskPath + "`\n"
			} else if err := os.Remove(filepath.Join(repo, fixture.child.TaskPath)); err != nil {
				t.Fatal(err)
			}
			writeSuspensionTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", plan)
			runControllerGit(t, repo, "add", "--all")
			runControllerGit(t, repo, "commit", "-qm", "last task fixture")
			store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
			if err != nil {
				t.Fatal(err)
			}
			authority, err := ResolveCommittedTaskAuthority(repo)
			if err != nil {
				t.Fatal(err)
			}
			workspace, err := ResolveWorkspaceIdentity(repo, store.Identity())
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := CaptureWorkspaceSnapshot(repo)
			if err != nil {
				t.Fatal(err)
			}
			source, err := store.BootstrapExecution(authority.Task, workspace, snapshot)
			if err != nil {
				t.Fatal(err)
			}
			fixture.store, fixture.source = store, source
			fixture, policy := configurePublicationTestFixture(t, fixture)
			fixture.source = publicationTestEdit(t, fixture, "result.txt", "verified last Task\n")
			acceptedCompletionRuntime(t, store, fixture.source)
			published, candidate := publishTerminalTestSource(t, fixture, policy, fixture.source)
			if published.Head.RootTaskRef != nil || published.Head.ExecutionTaskRef != nil || published.Head.LiveAttemptID != "" || published.Head.AcceptedCandidateRef != nil {
				t.Fatalf("completed project retained execution authority: %#v", published.Head)
			}
			if _, err := os.Stat(filepath.Join(repo, candidate.TaskRef.TaskPath)); !os.IsNotExist(err) {
				t.Fatalf("completed Task was not deleted: %v", err)
			}
			authority, err = ResolveCommittedTaskAuthority(repo)
			if err != nil || !authority.Task.Empty() || len(authority.Snapshot.Blocked) != map[bool]int{false: 0, true: 1}[blocked] {
				t.Fatalf("idle authority: %#v %v", authority, err)
			}
			operation, err := store.ProjectParentCompletion()
			if err != nil || operation.Operation != PublicationOperationObserved || operation.Surface != "" {
				t.Fatalf("idle completion: %#v %v", operation, err)
			}
			if got := observeTestRemote(t, repo, policy); got != published.Head.IntegrationTip {
				t.Fatal("idle completion was not published")
			}
			bound, err := WorkflowConfig(store.config)
			if err != nil || bound.RepoHash != WorkflowTaskConfig(store.config, store.Identity(), candidate.TaskRef).RepoHash {
				t.Fatalf("completed Task runtime lookup: %v", err)
			}
			task, canonical, err := WorkflowAuthorityTask(store.config)
			if err != nil || !canonical || !task.Equal(candidate.TaskRef) {
				t.Fatalf("completed Task read authority: %#v %v", task, err)
			}
			testIdleMetadataAndNewTask(t, store, policy, published, candidate.TaskRef)
		})
	}
}

func testIdleMetadataAndNewTask(t *testing.T, store *Store, policy PublicationPolicy, current ExecutionOperationResult, completed SemanticTaskRef) {
	t.Helper()
	repo := store.Identity().PrimaryRoot
	for _, amendment := range []string{"first", "second"} {
		writeSuspensionTestFile(t, repo, "IMPLEMENTATION_HISTORY.md", "# fixture decision\n\n"+amendment+"\n")
		runControllerGit(t, repo, "add", "IMPLEMENTATION_HISTORY.md")
		runControllerGit(t, repo, "commit", "-qm", "parent metadata amendment")
		if amendment == "first" {
			runControllerGit(t, repo, "push", "origin", policy.LocalRef+":"+policy.RemoteRef)
		}
		var err error
		current, err = store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: current.Head.ControllerGeneration, Policy: policy})
		if err != nil {
			t.Fatal(err)
		}
		if current.Head.RootTaskRef != nil || current.Head.LiveAttemptID != "" {
			t.Fatal("metadata amendment fabricated an ACTIVE Task")
		}
		task, canonical, err := WorkflowAuthorityTask(store.config)
		if err != nil || !canonical || !task.Equal(completed) {
			t.Fatalf("idle metadata read authority: %#v %v", task, err)
		}
		operation, err := store.ProjectParentCompletion()
		if err != nil || operation.Operation != PublicationOperationObserved {
			t.Fatalf("idle metadata completion: %#v %v", operation, err)
		}
	}
	path := "IMPLEMENTATION_TASKS/restarted.md"
	writeSuspensionTestFile(t, repo, path, "# restarted\n\n## External feasibility\n\nstatus: not-applicable\n\n## Dependencies\n\nnone\n")
	plan, err := os.ReadFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	plan = []byte(strings.Replace(string(plan), "## ACTIVE\n", "## ACTIVE\n\n- `"+path+"`\n", 1))
	writeSuspensionTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", string(plan))
	runControllerGit(t, repo, "add", "--all")
	runControllerGit(t, repo, "commit", "-qm", "select next Task")
	current, err = store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: current.Head.ControllerGeneration, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if current.Head.RootTaskRef == nil || current.Head.RootTaskRef.TaskPath != path {
		t.Fatal("new Task was not selected")
	}
	execution, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: current.Head.ControllerGeneration})
	if err != nil || execution.Admission == nil || execution.Admission.Attempt.SemanticTaskRef.TaskPath != path {
		t.Fatalf("new Task did not start: %v", err)
	}
}
