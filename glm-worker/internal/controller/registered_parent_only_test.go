package controller

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestRegisteredParentOnlyTaskStartsAndCompletesWithoutRegistrationCommit(t *testing.T) {
	for _, retired := range []bool{false, true} {
		t.Run(map[bool]string{false: "pristine", true: "retired"}[retired], func(t *testing.T) {
			fixture, _ := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
			if err := os.Remove(filepath.Join(fixture.repo, "IMPLEMENTATION_TASKS/successor.md")); err != nil {
				t.Fatal(err)
			}
			plan := "## ACTIVE\n\n## NEXT\n\n## BLOCKED\n"
			if retired {
				plan = "## ACTIVE\n\n- `" + parentOnlyEvaluationTaskPath + "`\n\n## NEXT\n\n## BLOCKED\n"
			} else if err := os.Remove(filepath.Join(fixture.repo, parentOnlyEvaluationTaskPath)); err != nil {
				t.Fatal(err)
			}
			writeSuspensionTestFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", plan)
			runControllerGit(t, fixture.repo, "add", "--all")
			runControllerGit(t, fixture.repo, "commit", "-qm", "registered parent-only fixture")
			runControllerGit(t, fixture.repo, "push", fixture.policy.Remote, "HEAD:"+fixture.policy.RemoteRef)
			if retired {
				admitParentOnlyExecutionFixture(t, fixture)
				completeRegisteredParentOnly(t, fixture)
			}
			base := controllerGitOutput(t, fixture.repo, "rev-parse", "HEAD")
			path := "IMPLEMENTATION_TASKS/registered-parent-only.md"
			writeSuspensionTestFile(t, fixture.repo, path, parentOnlyTaskContent(ExecutionAuthorityParentOnly))
			writeSuspensionTestFile(t, fixture.repo, "IMPLEMENTATION_PLAN.local.md", "## ACTIVE\n\n- `"+path+"`\n\n## NEXT\n\n## BLOCKED\n")
			writeParentOnlyProduct(t, fixture, "registered-result.md", "registered parent-only result\n")
			projected, err := fixture.store.ProjectParentCompletion()
			if err != nil || projected.Execution == nil || projected.Operation != PublicationOperationParentOnlyMaterialize {
				t.Fatalf("registered parent-only start projection: %#v %v", projected, err)
			}
			admission := admitParentOnlyExecutionFixture(t, fixture)
			if admission.Attempt.SemanticTaskRef.TaskPath != path || admission.Attempt.SourceRevision == nil {
				t.Fatal("registered parent-only attempt lost source authority")
			}
			if got := controllerGitOutput(t, fixture.repo, "rev-parse", "HEAD"); got != base {
				t.Fatal("parent-only registration created a separate commit")
			}
			completeRegisteredParentOnly(t, fixture)
			head, err := fixture.store.LoadHead()
			if err != nil || head.RootTaskRef != nil || head.LiveAttemptID != "" {
				t.Fatalf("registered parent-only Task was not retired: %#v %v", head, err)
			}
			if parent := controllerGitOutput(t, fixture.repo, "rev-parse", "HEAD^"); parent != base {
				t.Fatal("registered parent-only completion exceeded one commit")
			}
			product, err := os.ReadFile(filepath.Join(fixture.repo, "registered-result.md"))
			if err != nil || string(product) != "registered parent-only result\n" {
				t.Fatalf("registered parent-only completion lost its product: %q %v", product, err)
			}
			runtime := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.Identity(), admission.Attempt.SemanticTaskRef))
			if _, err := runtime.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("parent-only Task created a model Runtime binding: %v", err)
			}
		})
	}
}

func completeRegisteredParentOnly(t *testing.T, fixture parentOnlyTaskFixture) {
	t.Helper()
	admission := parentOnlyCompletionAdmission(t, fixture)
	accepted, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{Message: "parent-only Task result", Policy: fixture.policy, RequiredValidations: declaredNoValidations()})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if admission.Attempt.SourceRevision != nil {
		pruneAndRestoreRegisteredSource(t, fixture.store, admission.Attempt.SourceRevision.AuthorityOID, accepted.Head.ProjectSnapshotID)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
		t.Fatal(err)
	}
}
