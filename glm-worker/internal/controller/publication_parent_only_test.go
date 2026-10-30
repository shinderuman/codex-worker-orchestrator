package controller

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/execfile"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type parentOnlyTaskFixture struct {
	store  *Store
	repo   string
	policy PublicationPolicy
}

const parentOnlyEvaluationTaskPath = "IMPLEMENTATION_TASKS/parent-evaluation.md"

func parentOnlyTaskContent(mode string) string {
	task := "# parent evaluation\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\nparent evaluation products\n\n## Dependencies\n\nnone\n"
	if mode == "" {
		return task
	}
	return "# parent evaluation\n\n## External feasibility\n\nstatus: not-applicable\n\n## Execution authority\n\nmode: " + mode + "\n\n## Contract\n\nparent evaluation products\n\n## Dependencies\n\nnone\n"
}

func newParentOnlyTaskFixture(t *testing.T, mode string) (parentOnlyTaskFixture, SemanticTaskRef) {
	t.Helper()
	repo := newControllerRepo(t)
	if err := os.Remove(filepath.Join(repo, "IMPLEMENTATION_TASKS", "root.md")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, parentOnlyEvaluationTaskPath), []byte(parentOnlyTaskContent(mode)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_TASKS", "successor.md"), []byte("# successor\n\n## External feasibility\n\nstatus: not-applicable\n\n## Contract\n\nsuccessor task\n\n## Dependencies\n\nnone\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"), []byte("## ACTIVE\n\n- `"+parentOnlyEvaluationTaskPath+"`\n\n## NEXT\n\n- `IMPLEMENTATION_TASKS/successor.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, repo, "add", ".")
	runControllerGit(t, repo, "commit", "-q", "-m", "parent-only corpus")
	store, err := Open(controllerTestConfig(repo, filepath.Join(t.TempDir(), "state", "sessions")))
	if err != nil {
		t.Fatal(err)
	}
	remote := filepath.Join(t.TempDir(), "remote.git")
	runControllerGit(t, repo, "init", "--bare", remote)
	runControllerGit(t, repo, "remote", "add", "origin", remote)
	runControllerGit(t, repo, "push", "-u", "origin", "HEAD:refs/heads/main")
	policy := PublicationPolicy{Remote: "origin", RemoteRef: "refs/heads/main", LocalRef: controllerGitOutput(t, repo, "symbolic-ref", "HEAD")}
	authority, err := ResolveCommittedTaskAuthority(repo)
	if err != nil {
		t.Fatal(err)
	}
	return parentOnlyTaskFixture{store: store, repo: repo, policy: policy}, authority.Task
}

func writeParentOnlyProduct(t *testing.T, f parentOnlyTaskFixture, path, data string) {
	t.Helper()
	writeSuspensionTestFile(t, f.repo, path, data)
}

func admitParentOnlyExecutionFixture(t *testing.T, f parentOnlyTaskFixture) Admission {
	t.Helper()
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	result, err := f.store.AdmitParentOnlyExecution(ParentOnlyMaterializeInput{ExpectedGeneration: head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if result.Admission == nil || result.Admission.Attempt.ExecutionAuthority != ExecutionAuthorityParentOnly {
		t.Fatalf("parent-only materialization lost its admission: %#v", result.Admission)
	}
	return *result.Admission
}

func parentOnlyCompletionAdmission(t *testing.T, f parentOnlyTaskFixture) Admission {
	t.Helper()
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	authority, err := MutationAuthorityFromHead(head)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(f.repo, f.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	admission, err := f.store.AdmitMutation(authority, workspace, snapshot)
	if err != nil {
		t.Fatal(err)
	}
	return admission
}

func TestParentOnlyExecutionMaterializesAndCompletesThroughPublication(t *testing.T) {
	t.Parallel()
	fixture, task := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	quiescent, err := fixture.store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if quiescent.Surface != ExecutionOperationSurface || quiescent.Operation != PublicationOperationParentOnlyMaterialize || quiescent.Mode != ParentCompletionModeParentOnly || quiescent.Execution == nil {
		t.Fatalf("quiescent parent-only projection: %#v", quiescent)
	}
	writeParentOnlyProduct(t, fixture, "evaluation-result.md", "parent evaluation product\n")
	admitParentOnlyExecutionFixture(t, fixture)

	live, err := fixture.store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if live.Surface != PublicationOperationSurface || live.Operation != PublicationOperationAccept || live.Mode != ParentCompletionModeParentOnly || live.Command == nil || live.Command.Policy == nil {
		t.Fatalf("live parent-only projection: %#v", live)
	}
	if len(live.SemanticSlots) != 3 || live.SemanticSlots[0] != CompletionSlotMessage {
		t.Fatalf("parent-only complete slots: %#v", live.SemanticSlots)
	}
	if _, err := fixture.store.ProjectParentCompletionOperation(PublicationOperationParentOnlyMaterialize); err == nil {
		t.Fatal("materialize stayed admitted after it bound the live attempt")
	}

	admission := parentOnlyCompletionAdmission(t, fixture)
	accepted, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{Message: "parent accepts the evaluation products", Policy: fixture.policy, RequiredValidations: declaredNoValidations()})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	assertParentOnlyAcceptanceEvidence(t, fixture, candidate)
	if candidate.CommitOID == candidate.BaseOID {
		t.Fatal("parent-only candidate published no source")
	}
	if commits := strings.TrimSpace(string(controllerGitOutput(t, fixture.repo, "rev-list", "--count", candidate.BaseOID+".."+candidate.CommitOID))); commits != "1" {
		t.Fatalf("parent-only candidate spans %s commits", commits)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	retired, candidate := prepareCompletionTestCandidate(t, fixture.store, promoted, candidate)
	published, err := fixture.store.RecoverExecutionOperation(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if published.Head.IntegrationTip != candidate.CommitOID || published.Head.LiveLeaseID != "" || published.Head.ExecutionTaskRef != nil || published.Head.MetadataLineageRef == nil {
		t.Fatalf("parent-only publication did not retire the task authority: %#v", published.Head)
	}
	remote := controllerGitOutput(t, fixture.repo, "ls-remote", "origin", fixture.policy.RemoteRef)
	if !strings.Contains(string(remote), candidate.CommitOID) {
		t.Fatal("parent-only publication did not reach the configured remote")
	}
	finalAuthority, err := ResolveCommittedTaskAuthorityAt(fixture.repo, candidate.CommitOID)
	if err != nil {
		t.Fatal(err)
	}
	if finalAuthority.Task.TaskPath != "IMPLEMENTATION_TASKS/successor.md" || projectHasTask(finalAuthority.Snapshot, task) {
		t.Fatalf("parent-only completion did not retire its task from the plan: %#v", finalAuthority.Task)
	}
	runtime := state.AttachStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.Identity(), task))
	if runtime.Exists("task.id") {
		t.Fatal("parent-only completion started a Runtime task")
	}
	seal, err := fixture.store.LoadAttemptSeal(candidate.SealRef)
	if err != nil {
		t.Fatal(err)
	}
	if seal.Coverage != attemptSealCoverageIncomplete || len(seal.Missing) != 1 || seal.Missing[0] != attemptSealRuntimeEvidenceMissing {
		t.Fatalf("parent-only attempt seal hid the missing Runtime evidence: %#v", seal)
	}
}

func assertParentOnlyAcceptanceEvidence(t *testing.T, fixture parentOnlyTaskFixture, candidate AcceptedCandidate) {
	t.Helper()
	kinds := map[string]string{}
	for _, ref := range candidate.Evidence {
		data, err := fixture.store.LoadEvidenceObject(ref)
		if err != nil {
			t.Fatal(err)
		}
		var record CandidateEvidence
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		kinds[record.Kind] = record.Result
		if record.Kind != candidateEvidenceKindParentAccept {
			continue
		}
		artifact, err := fixture.store.LoadEvidenceObject(record.Artifact)
		if err != nil {
			t.Fatal(err)
		}
		var proof ParentOnlyAcceptanceProof
		if err := json.Unmarshal(artifact, &proof); err != nil {
			t.Fatal(err)
		}
		if proof.Mode != ExecutionAuthorityParentOnly || proof.ModelExecution != parentOnlyModelExecutionNone || proof.ReviewAuthority != parentOnlyReviewAuthorityValue || proof.AttemptID != candidate.AttemptID {
			t.Fatalf("parent acceptance proof lost its explicit parent-only authority: %#v", proof)
		}
	}
	if kinds[candidateEvidenceKindParentAccept] != candidateEvidenceAccepted || kinds[candidateEvidenceKindReview] != "" || kinds[candidateEvidenceKindValidationObligation] != candidateEvidenceDeclared {
		t.Fatalf("parent-only candidate evidence kinds: %#v", kinds)
	}
}

func TestParentOnlyExecutionRejectsUndesignatedTaskAndRuntimeStart(t *testing.T) {
	t.Parallel()
	undesignated, _ := newParentOnlyTaskFixture(t, "")
	head, err := undesignated.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := undesignated.store.AdmitParentOnlyExecution(ParentOnlyMaterializeInput{ExpectedGeneration: head.ControllerGeneration}); err == nil || !strings.Contains(err.Error(), "not designated for parent-only execution") {
		t.Fatalf("undesignated task admitted parent-only execution: %v", err)
	}

	designated, task := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	authority, err := ResolveCommittedTaskAuthority(designated.repo)
	if err != nil {
		t.Fatal(err)
	}
	if !authority.Task.Equal(task) || authority.ExecutionMode != ExecutionAuthorityParentOnly {
		t.Fatalf("committed authority lost the parent-only designation: %#v", authority)
	}
	workspace, err := ResolveWorkspaceIdentity(designated.repo, designated.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(designated.repo)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := designated.store.BootstrapExecution(authority.Task, workspace, snapshot); err == nil || !strings.Contains(err.Error(), "must not bootstrap a Runtime execution") {
		t.Fatalf("parent-only designated task bootstrapped a Runtime: %v", err)
	}
}

func TestParentOnlyCompletionReentryPreservesAuthority(t *testing.T) {
	t.Parallel()
	for _, route := range []string{"reenter", "parent-source-rebind", "remote-rebind"} {
		t.Run(route, func(t *testing.T) {
			t.Parallel()
			fixture, _ := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
			writeParentOnlyProduct(t, fixture, "evaluation-result.md", "parent evaluation product\n")
			admitParentOnlyExecutionFixture(t, fixture)
			admission := parentOnlyCompletionAdmission(t, fixture)
			required := &RequiredValidationDeclaration{Forms: []FinalValidationRequest{{Form: "go-test", WorkingDir: "glm-worker"}}}
			accepted, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{Message: "initial acceptance", Policy: fixture.policy, RequiredValidations: required})
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			input := PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}
			parentOnlyReentryRoute(t, fixture, candidate, input, route)
			operation, err := fixture.store.ProjectParentCompletion()
			if err != nil || operation.Execution == nil || operation.Execution.Action != "materialize" {
				t.Fatalf("reentry materialization projection: %#v %v", operation, err)
			}
			materialized, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: operation.Execution.ExpectedGeneration, SuspensionID: operation.Execution.SuspensionID})
			if err != nil {
				t.Fatal(err)
			}
			reentered := materialized.Admission
			if reentered == nil || reentered.Attempt.ExecutionAuthority != ExecutionAuthorityParentOnly || reentered.Attempt.PredecessorAttemptID != admission.Attempt.AttemptID {
				t.Fatalf("parent-only authority was lost: %#v", reentered)
			}
			if route == "parent-source-rebind" {
				data, err := os.ReadFile(filepath.Join(reentered.Workspace.Root, "evaluation-result.md"))
				if err != nil || string(data) != "corrected parent evaluation product\n" {
					t.Fatalf("reentry lost revised parent source: %q %v", data, err)
				}
			}
			operation, err = fixture.store.ProjectParentCompletion()
			if err != nil || operation.Operation != PublicationOperationAccept || operation.Mode != ParentCompletionModeParentOnly {
				t.Fatalf("parent-only reentry did not return to parent acceptance: %#v %v", operation, err)
			}
			if _, err := fixture.store.AcceptParentCompletion(*reentered, CandidateAcceptanceInput{Message: "drop prior validation", Policy: fixture.policy, RequiredValidations: declaredNoValidations()}); err == nil || !strings.Contains(err.Error(), "drops predecessor validation") {
				t.Fatalf("parent-only reentry dropped required validation: %v", err)
			}
			reaccepted, err := fixture.store.AcceptParentCompletion(*reentered, CandidateAcceptanceInput{Message: "reaccepted parent products", Policy: fixture.policy, RequiredValidations: required})
			if err != nil {
				t.Fatal(err)
			}
			updated, err := fixture.store.LoadAcceptedCandidate(*reaccepted.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			assertParentOnlyAcceptanceEvidence(t, fixture, updated)
			if route == "parent-source-rebind" {
				data, err := runGitBinary(fixture.repo, nil, "show", updated.CommitOID+":evaluation-result.md")
				if err != nil || string(data) != "corrected parent evaluation product\n" {
					t.Fatalf("reaccepted candidate lost revised product: %q %v", data, err)
				}
			}
			runtime, _, err := fixture.store.boundAttemptRuntime(*reentered)
			if err != nil || runtime != nil {
				t.Fatalf("parent-only reentry created a model Runtime: %v", err)
			}
		})
	}
}

func parentOnlyReentryRoute(t *testing.T, fixture parentOnlyTaskFixture, candidate AcceptedCandidate, input PublicationInput, route string) {
	t.Helper()
	var err error
	switch route {
	case "reenter":
		_, err = fixture.store.ReenterAcceptedCandidate(input)
	case "parent-source-rebind":
		writeParentOnlyProduct(t, fixture, "evaluation-result.md", "corrected parent evaluation product\n")
		snapshot, captureErr := CaptureWorkspaceSnapshot(fixture.repo)
		if captureErr != nil {
			t.Fatal(captureErr)
		}
		_, err = fixture.store.RebindParentSourceCandidate(input, snapshot.ID)
	case "remote-rebind":
		tree := controllerGitOutput(t, fixture.repo, "rev-parse", candidate.BaseOID+"^{tree}")
		advance := controllerGitOutput(t, fixture.repo, "commit-tree", tree, "-p", candidate.BaseOID, "-m", "external source advance")
		runControllerGit(t, fixture.repo, "push", "origin", advance+":"+fixture.policy.RemoteRef)
		_, err = fixture.store.RebindUnpublishedCandidate(input)
	}
	if err != nil {
		t.Fatal(err)
	}
}

func TestParentOnlyCompletionFailClosedBoundaries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		seed func(t *testing.T, f parentOnlyTaskFixture, admission Admission) Admission
		want string
	}{
		{name: "runtime bound to the attempt", seed: func(t *testing.T, f parentOnlyTaskFixture, admission Admission) Admission {
			runtime, err := state.NewStateStore(WorkflowTaskConfig(f.store.config, f.store.Identity(), admission.Attempt.SemanticTaskRef))
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Write("task.id", "parent-only-runtime-task"); err != nil {
				t.Fatal(err)
			}
			if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{AttemptID: admission.Attempt.AttemptID, TaskPath: admission.Attempt.SemanticTaskRef.TaskPath, TaskContractDigest: admission.Attempt.SemanticTaskRef.ContractDigest}); err != nil {
				t.Fatal(err)
			}
			return admission
		}, want: "requires an execution without a Runtime"},
		{name: "model call recorded for the attempt", seed: func(t *testing.T, f parentOnlyTaskFixture, admission Admission) Admission {
			next, err := f.store.RecordAdmittedMutation(admission, "model:worker", "success", admission.Snapshot)
			if err != nil {
				t.Fatal(err)
			}
			return next
		}, want: "required model runtime evidence is unavailable"},
		{name: "designation removed mid-flight", seed: func(t *testing.T, f parentOnlyTaskFixture, _ Admission) Admission {
			if err := os.WriteFile(filepath.Join(f.repo, parentOnlyEvaluationTaskPath), []byte(parentOnlyTaskContent("")), 0o644); err != nil {
				t.Fatal(err)
			}
			runControllerGit(t, f.repo, "add", ".")
			runControllerGit(t, f.repo, "commit", "-q", "-m", "drop designation")
			return handBuiltParentOnlyAdmission(t, f)
		}, want: "no longer designated for parent-only execution"},
		{name: "task contract changed mid-flight", seed: func(t *testing.T, f parentOnlyTaskFixture, _ Admission) Admission {
			if err := os.WriteFile(filepath.Join(f.repo, parentOnlyEvaluationTaskPath), []byte(parentOnlyTaskContent(ExecutionAuthorityParentOnly)+"\n## Amendments\n\n- mid-flight change\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			runControllerGit(t, f.repo, "add", ".")
			runControllerGit(t, f.repo, "commit", "-q", "-m", "amend contract")
			return handBuiltParentOnlyAdmission(t, f)
		}, want: "committed task contract changed"},
		{name: "repository HEAD moved mid-flight", seed: func(t *testing.T, f parentOnlyTaskFixture, _ Admission) Admission {
			writeParentOnlyProduct(t, f, "unrelated-committed.txt", "committed elsewhere\n")
			runControllerGit(t, f.repo, "add", ".")
			runControllerGit(t, f.repo, "commit", "-q", "-m", "move HEAD")
			return handBuiltParentOnlyAdmission(t, f)
		}, want: "committed repository HEAD moved"},
		{name: "worktree source changed after admission", seed: func(t *testing.T, f parentOnlyTaskFixture, admission Admission) Admission {
			writeParentOnlyProduct(t, f, "evaluation-result.md", "changed after admission\n")
			return admission
		}, want: "source changed after admission"},
		{name: "declaration drops recorded obligation", seed: func(t *testing.T, f parentOnlyTaskFixture, admission Admission) Admission {
			runtime, err := state.NewStateStore(WorkflowTaskConfig(f.store.config, f.store.Identity(), admission.Attempt.SemanticTaskRef))
			if err != nil {
				t.Fatal(err)
			}
			if err := runtime.Write(state.FinalValidationObligationKey, `{"Form":"go-test","WorkingDir":"glm-worker"}`); err != nil {
				t.Fatal(err)
			}
			return admission
		}, want: "drops the Runtime obligation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture, _ := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
			writeParentOnlyProduct(t, fixture, "evaluation-result.md", "parent evaluation product\n")
			admitParentOnlyExecutionFixture(t, fixture)
			admission := parentOnlyCompletionAdmission(t, fixture)
			admission = tc.seed(t, fixture, admission)
			if _, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{Message: "boundary", Policy: fixture.policy, RequiredValidations: declaredNoValidations()}); err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("boundary %s admitted completion: %v", tc.name, err)
			}
		})
	}
}

func handBuiltParentOnlyAdmission(t *testing.T, f parentOnlyTaskFixture) Admission {
	t.Helper()
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	attempt, err := f.store.loadAttempt(head.LiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	lease, err := f.store.loadLease(head.LiveLeaseID)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := ResolveWorkspaceIdentity(f.repo, f.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := CaptureWorkspaceSnapshot(f.repo)
	if err != nil {
		t.Fatal(err)
	}
	return Admission{Head: head, Attempt: attempt, Lease: lease, Workspace: workspace, Snapshot: snapshot}
}

func TestParentOnlyCompletionObligationCoverageAndEvidenceInjection(t *testing.T) {
	t.Parallel()
	fixture, _ := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	writeParentOnlyProduct(t, fixture, "evaluation-result.md", "parent evaluation product\n")
	admitParentOnlyExecutionFixture(t, fixture)
	admission := parentOnlyCompletionAdmission(t, fixture)
	runtime, err := state.NewStateStore(WorkflowTaskConfig(fixture.store.config, fixture.store.Identity(), admission.Attempt.SemanticTaskRef))
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write(state.FinalValidationObligationKey, `{"Form":"go-test","WorkingDir":"glm-worker"}`); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{Message: "boundary", Policy: fixture.policy, RequiredValidations: nil}); err == nil || !strings.Contains(err.Error(), "undetermined") {
		t.Fatalf("undetermined validation set admitted: %v", err)
	}
	covered := &RequiredValidationDeclaration{Forms: []FinalValidationRequest{{Form: "go-test", WorkingDir: "glm-worker"}}}
	accepted, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{Message: "covered obligation", Policy: fixture.policy, RequiredValidations: covered})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.RequiredValidations) != 1 || candidate.RequiredValidations[0].Form != "go-test" {
		t.Fatalf("covered obligation did not bind the candidate: %#v", candidate.RequiredValidations)
	}

	injected, _ := fixture.store.PutEvidenceObject("review", "text/plain", "injected:"+candidate.SnapshotID, true, []byte("injected"))
	fixture2, _ := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	writeParentOnlyProduct(t, fixture2, "evaluation-result.md", "parent evaluation product\n")
	admitParentOnlyExecutionFixture(t, fixture2)
	admission2 := parentOnlyCompletionAdmission(t, fixture2)
	if _, err := fixture2.store.AcceptParentCompletion(admission2, CandidateAcceptanceInput{Message: "injected", Policy: fixture2.policy, RequiredValidations: declaredNoValidations(), Evidence: []EvidenceObjectRef{injected}}); err == nil || !strings.Contains(err.Error(), "not caller evidence JSON") {
		t.Fatalf("caller evidence injected into parent-only completion: %v", err)
	}
}

func TestParentOnlyAcceptanceRejectsAlteredSourceProof(t *testing.T) {
	t.Parallel()
	fixture, _ := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	writeParentOnlyProduct(t, fixture, "evaluation-result.md", "parent evaluation product\n")
	admitParentOnlyExecutionFixture(t, fixture)
	admission := parentOnlyCompletionAdmission(t, fixture)
	accepted, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{Message: "source proof", Policy: fixture.policy, RequiredValidations: declaredNoValidations()})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	var record CandidateEvidence
	var proof ParentOnlyAcceptanceProof
	for _, ref := range candidate.Evidence {
		data, err := fixture.store.LoadEvidenceObject(ref)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &record); err != nil {
			t.Fatal(err)
		}
		if record.Kind != candidateEvidenceKindParentAccept {
			continue
		}
		data, err = fixture.store.LoadEvidenceObject(record.Artifact)
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(data, &proof); err != nil {
			t.Fatal(err)
		}
		break
	}
	if record.Kind != candidateEvidenceKindParentAccept {
		t.Fatal("parent acceptance evidence is missing")
	}
	for name, alter := range map[string]func(*ParentOnlyAcceptanceProof){
		"snapshot identity": func(p *ParentOnlyAcceptanceProof) { p.SourceSnapshot.ID = "other-source" },
		"source contents": func(p *ParentOnlyAcceptanceProof) {
			p.SourceSnapshot.WorktreeDigest = "other-contents"
			p.SourceSnapshot.ID = workspaceSnapshotID(p.SourceSnapshot)
		},
		"missing acceptance": func(p *ParentOnlyAcceptanceProof) { p.AcceptedAt = time.Time{} },
	} {
		t.Run(name, func(t *testing.T) {
			altered := proof
			alter(&altered)
			artifact, err := json.Marshal(altered)
			if err != nil {
				t.Fatal(err)
			}
			record.Artifact = EvidenceObjectRef{}
			ref, err := fixture.store.StoreCandidateEvidenceJSON(record, artifact)
			if err != nil {
				t.Fatal(err)
			}
			data, err := fixture.store.LoadEvidenceObject(ref)
			if err != nil {
				t.Fatal(err)
			}
			var changed CandidateEvidence
			if err := json.Unmarshal(data, &changed); err != nil {
				t.Fatal(err)
			}
			if err := fixture.store.validateParentAcceptanceArtifact(candidate, changed.Artifact); err == nil {
				t.Fatal("altered parent acceptance proof was admitted")
			}
		})
	}
}

func TestParentOnlyMaterializationRecoversSameAttempt(t *testing.T) {
	t.Parallel()
	fixture, _ := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	op, head, err := fixture.store.planParentOnlyExecution(ParentOnlyMaterializeInput{ExpectedGeneration: head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.prepareExecutionOperation(&op, head); err != nil {
		t.Fatal(err)
	}
	blocked := filepath.Join(fixture.store.dir, "leases", op.Lease.LeaseID+".json")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("injected lease write failure was ignored")
	}
	if err := os.Remove(blocked); err != nil {
		t.Fatal(err)
	}
	recovered, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Head.LiveAttemptID != op.Attempt.AttemptID || recovered.Head.LiveLeaseID != op.Lease.LeaseID {
		t.Fatal("materialization recovery replaced its attempt or lease")
	}
	if _, err := fixture.store.RecoverExecutionOperation(op.Transition.TransitionID); err != nil {
		t.Fatalf("materialization response replay: %v", err)
	}
}

func TestParentOnlyCompletionRecoversPublishedWithoutAnotherPush(t *testing.T) {
	t.Parallel()
	fixture, _ := newParentOnlyTaskFixture(t, ExecutionAuthorityParentOnly)
	writeParentOnlyProduct(t, fixture, "evaluation-result.md", "parent evaluation product\n")
	admitParentOnlyExecutionFixture(t, fixture)
	admission := parentOnlyCompletionAdmission(t, fixture)
	accepted, err := fixture.store.AcceptParentCompletion(admission, CandidateAcceptanceInput{Message: "recover publication", Policy: fixture.policy, RequiredValidations: declaredNoValidations()})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	promoted, err := fixture.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	remote := controllerGitOutput(t, fixture.repo, "remote", "get-url", fixture.policy.Remote)
	pushes := filepath.Join(t.TempDir(), "pushes")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\"'\"'") + "'" }
	hook := "#!/bin/sh\nset -eu\nprintf x >> " + quote(pushes) + "\nchmod 0500 " + quote(fixture.store.dir) + "\n"
	if err := execfile.Write(filepath.Join(remote, "hooks", "post-receive"), []byte(hook), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(fixture.store.dir, 0o700) })
	if _, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("injected post-publication failure was ignored")
	}
	head, err := fixture.store.LoadHead()
	if err != nil || head.PendingTransitionID == "" {
		t.Fatalf("published operation lost its recovery identity: %v", err)
	}
	if err := os.Chmod(fixture.store.dir, 0o700); err != nil {
		t.Fatal(err)
	}
	completed, err := fixture.store.RecoverExecutionOperation(head.PendingTransitionID)
	if err != nil {
		t.Fatal(err)
	}
	if completed.Head.MetadataLineageRef == nil || completed.Head.PendingTransitionID != "" {
		t.Fatal("publication recovery did not retire the parent-only task")
	}
	if _, err := fixture.store.RecoverExecutionOperation(head.PendingTransitionID); err != nil {
		t.Fatalf("publication response replay: %v", err)
	}
	count, err := os.ReadFile(pushes)
	if err != nil || string(count) != "x" {
		t.Fatalf("publication recovery repeated a successful push: %q %v", count, err)
	}
}
