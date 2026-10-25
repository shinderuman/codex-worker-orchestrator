package controller

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const retiredAlternateSuccessorPath = "IMPLEMENTATION_TASKS/alternate-successor.md"

func pullRetiredExternalSource(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy) string {
	t.Helper()
	return pullRetiredExternalSourceChange(t, fixture, policy, "result.txt", "external code update\n")
}

func pullRetiredExternalSourceChange(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, path, data string) string {
	t.Helper()
	repo := fixture.store.identity.PrimaryRoot
	external := filepath.Join(t.TempDir(), "external")
	runControllerGit(t, repo, "clone", "--quiet", repo, external)
	runControllerGit(t, external, "config", "user.email", "test@example.com")
	runControllerGit(t, external, "config", "user.name", "external")
	writeSuspensionTestFile(t, external, path, data)
	writeSuspensionTestFile(t, external, ".github/workflows/check.yml", "name: external\non: push\n")
	runControllerGit(t, external, "add", "--all")
	runControllerGit(t, external, "commit", "-qm", "external code and CI")
	remote := controllerGitOutput(t, repo, "remote", "get-url", policy.Remote)
	runControllerGit(t, external, "push", remote, "HEAD:"+policy.RemoteRef)
	runControllerGit(t, repo, "pull", "--ff-only", policy.Remote, policy.RemoteRef)
	return controllerGitOutput(t, repo, "rev-parse", "HEAD")
}

func switchPlanActiveTask(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, from, to string) {
	t.Helper()
	repo := fixture.store.identity.PrimaryRoot
	plan, err := os.ReadFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		t.Fatal(err)
	}
	const swapPlaceholder = "IMPLEMENTATION_TASKS/__swap_placeholder__.md"
	switched := strings.ReplaceAll(strings.ReplaceAll(string(plan), from, swapPlaceholder), to, from)
	switched = strings.ReplaceAll(switched, swapPlaceholder, to)
	writeSuspensionTestFile(t, repo, "IMPLEMENTATION_PLAN.local.md", switched)
	runControllerGit(t, repo, "add", "--all")
	runControllerGit(t, repo, "commit", "-qm", "parent successor selection")
	runControllerGit(t, repo, "push", "origin", policy.LocalRef+":"+policy.RemoteRef)
}

func retiredSuccessorSwitchFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, ExecutionOperationResult) {
	t.Helper()
	fixture, policy := configurePublicationTestFixture(t, newFindingAcceptanceFixture(t, retiredAlternateSuccessorPath))
	source := publicationTestEdit(t, fixture, "result.txt", "retired result\n")
	retired, _ := publishTerminalTestSource(t, fixture, policy, source)
	runControllerGit(t, fixture.store.identity.PrimaryRoot, "add", "--all")
	return fixture, policy, retired
}

func TestRetiredExternalSourcePullPreservesHistoryAndStartsSuccessor(t *testing.T) {
	t.Parallel()
	fixture, policy, retired := retiredMetadataFixture(t)
	store := fixture.store
	terminal, err := store.LoadEvidenceObject(*retired.Head.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	tip := pullRetiredExternalSource(t, fixture, policy)
	operation, err := store.ProjectParentCompletion()
	if err != nil || operation.Operation != PublicationOperationAdoptSource || operation.Command == nil || operation.Command.SourceOID != tip {
		t.Fatalf("retired external source lost its synchronization owner: %#v err=%v", operation, err)
	}
	selected, canonical, err := WorkflowAuthorityTask(store.config)
	if err != nil || !canonical || !selected.Equal(*retired.Head.RootTaskRef) {
		t.Fatalf("read-only authority after pull: %v", err)
	}
	head, err := store.LoadHead()
	if err != nil || !reflect.DeepEqual(head, retired.Head) {
		t.Fatal("read authority mutated controller")
	}
	if _, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: head.ControllerGeneration}); err == nil {
		t.Fatal("read-only authority bypassed source synchronization")
	}
	adopted, err := store.AdoptRetiredExternalSource(RetiredSourceAdvancementInput{ExpectedGeneration: head.ControllerGeneration, Policy: policy, SourceOID: tip})
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Head.IntegrationTip != tip || adopted.Head.ExecutionTaskRef != nil || adopted.Head.AcceptedCandidateRef != nil || adopted.Head.LiveLeaseID != "" {
		t.Fatal("source synchronization minted execution or candidate authority")
	}
	after, err := store.LoadEvidenceObject(*adopted.Head.MetadataLineageRef)
	if err != nil || !reflect.DeepEqual(terminal, after) {
		t.Fatal("source synchronization rewrote historical terminal evidence")
	}
	if _, err := store.RecoverExecutionOperation(adopted.TransitionID); err != nil {
		t.Fatal(err)
	}
	started, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: adopted.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if started.Admission == nil || started.Admission.Attempt.ExecutionBaseOID != tip || started.Admission.Attempt.AttemptID == fixture.source.Attempt.AttemptID {
		t.Fatal("successor did not start a fresh attempt on pulled source")
	}
	data, err := os.ReadFile(filepath.Join(started.Admission.Workspace.Root, "result.txt"))
	if err != nil || string(data) != "external code update\n" {
		t.Fatal("successor lost pulled code")
	}
}

func TestRetiredExternalSourceRejectsConflictsWithoutChangingAuthority(t *testing.T) {
	for _, mode := range []string{"dirty", "remote", "stale-source", "stale-generation", "policy", "runtime", "live", "divergence"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			fixture, policy, retired := retiredMetadataFixture(t)
			store := fixture.store
			if mode == "live" {
				started, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: retired.Head.ControllerGeneration})
				if err != nil {
					t.Fatal(err)
				}
				retired.Head = started.Head
			}
			tip := pullRetiredExternalSource(t, fixture, policy)
			input := RetiredSourceAdvancementInput{ExpectedGeneration: retired.Head.ControllerGeneration, Policy: policy, SourceOID: tip}
			head := retired.Head
			switch mode {
			case "dirty":
				writeSuspensionTestFile(t, store.identity.PrimaryRoot, "untracked.txt", "preserve\n")
			case "remote":
				runControllerGit(t, store.identity.PrimaryRoot, "push", "--force", policy.Remote, head.IntegrationTip+":"+policy.RemoteRef)
			case "stale-source":
				input.SourceOID = head.IntegrationTip
			case "stale-generation":
				input.ExpectedGeneration--
			case "policy":
				input.Policy.RequireInstall = !policy.RequireInstall
			case "runtime":
				cfg := WorkflowTaskConfig(store.config, store.identity, *head.RootTaskRef)
				cfg.RepoRoot = store.identity.PrimaryRoot
				runtime := state.AttachStateStore(cfg)
				if err := runtime.Write("task.id", "unfinished"); err != nil {
					t.Fatal(err)
				}
				if err := runtime.Write("task.status", string(state.TaskStatusAwaitingParentCompletion)); err != nil {
					t.Fatal(err)
				}
			case "divergence":
				runControllerGit(t, store.identity.PrimaryRoot, "checkout", "--orphan", "unrelated")
				runControllerGit(t, store.identity.PrimaryRoot, "commit", "-qm", "unrelated root")
				input.SourceOID = controllerGitOutput(t, store.identity.PrimaryRoot, "rev-parse", "HEAD")
			}
			before, err := store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if _, err := store.AdoptRetiredExternalSource(input); err == nil {
				t.Fatal("conflicting source adoption succeeded")
			}
			after, err := store.LoadHead()
			if err != nil || !reflect.DeepEqual(before, after) {
				t.Fatal("rejected source adoption changed authority")
			}
		})
	}
}

func TestRetiredSourceAdvancementRaceAbortsAndReplans(t *testing.T) {
	t.Parallel()
	fixture, policy, retired := retiredMetadataFixture(t)
	store := fixture.store
	tip := pullRetiredExternalSource(t, fixture, policy)
	op, err := store.planRetiredSourceAdvancement(retired.Head, policy, tip)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.prepareExecutionOperation(&op, retired.Head); err != nil {
		t.Fatal(err)
	}
	pending, err := store.LoadHead()
	if err != nil || pending.PendingTransitionID != op.Transition.TransitionID || pending.ControllerGeneration != op.Transition.PreparedGeneration {
		t.Fatalf("prepared source advancement lost pending authority: %#v", pending)
	}
	raced := pullRetiredExternalSourceChange(t, fixture, policy, "raced-code.txt", "second external code update\n")
	if _, err := store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("raced source advancement applied a stale source")
	}
	after, err := store.LoadHead()
	if err != nil || after.PendingTransitionID != "" || after.IntegrationTip != retired.Head.IntegrationTip || after.ControllerGeneration == pending.ControllerGeneration {
		t.Fatalf("raced source advancement retained pending authority: %#v", after)
	}
	adopted, err := store.AdoptRetiredExternalSource(RetiredSourceAdvancementInput{ExpectedGeneration: after.ControllerGeneration, Policy: policy, SourceOID: raced})
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Head.IntegrationTip != raced || !evidenceRefsEqual(*retired.Head.MetadataLineageRef, *adopted.Head.MetadataLineageRef) {
		t.Fatal("replanned source advancement lost completion lineage")
	}
	started, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: adopted.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(started.Admission.Workspace.Root, "raced-code.txt"))
	if err != nil || string(data) != "second external code update\n" {
		t.Fatal("replanned successor lost raced source")
	}
}

func TestRetiredSourceAdvancementFollowUpsPreserveTerminalHistory(t *testing.T) {
	t.Parallel()
	fixture, policy, retired := retiredSuccessorSwitchFixture(t)
	store := fixture.store
	terminal, err := store.LoadEvidenceObject(*retired.Head.MetadataLineageRef)
	if err != nil {
		t.Fatal(err)
	}
	sourceTip := pullRetiredExternalSource(t, fixture, policy)
	adopted, err := store.AdoptRetiredExternalSource(RetiredSourceAdvancementInput{ExpectedGeneration: retired.Head.ControllerGeneration, Policy: policy, SourceOID: sourceTip})
	if err != nil {
		t.Fatal(err)
	}
	switchPlanActiveTask(t, fixture, policy, retired.Head.RootTaskRef.TaskPath, retiredAlternateSuccessorPath)
	selected, canonical, err := WorkflowAuthorityTask(store.config)
	if err != nil || !canonical || selected.TaskPath != retiredAlternateSuccessorPath {
		t.Fatalf("successor switch read authority: %v %v %v", selected, canonical, err)
	}
	switched, err := store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: adopted.Head.ControllerGeneration, Policy: policy})
	if err != nil {
		t.Fatal(err)
	}
	if switched.Head.RootTaskRef == nil || !switched.Head.RootTaskRef.Equal(selected) {
		t.Fatal("metadata switch lost the selected successor")
	}
	secondTip := pullRetiredExternalSourceChange(t, fixture, policy, "second-source.txt", "second external update\n")
	resynced, err := store.AdoptRetiredExternalSource(RetiredSourceAdvancementInput{ExpectedGeneration: switched.Head.ControllerGeneration, Policy: policy, SourceOID: secondTip})
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.LoadEvidenceObject(*resynced.Head.MetadataLineageRef)
	if err != nil || !reflect.DeepEqual(terminal, after) || !evidenceRefsEqual(*retired.Head.MetadataLineageRef, *resynced.Head.MetadataLineageRef) {
		t.Fatal("follow-up advancements rewrote terminal history")
	}
	started, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: resynced.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	if started.Admission == nil || !started.Admission.Attempt.SemanticTaskRef.Equal(selected) || started.Admission.Attempt.ExecutionBaseOID != secondTip {
		t.Fatal("switched successor did not start on the synchronized source")
	}
}

func TestRetiredMetadataSuccessorSwitchRequiresQuiescentRuntime(t *testing.T) {
	for _, mode := range []string{"unfinished-runtime", "switch"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			fixture, policy, retired := retiredSuccessorSwitchFixture(t)
			store := fixture.store
			terminal, err := store.LoadEvidenceObject(*retired.Head.MetadataLineageRef)
			if err != nil {
				t.Fatal(err)
			}
			switchPlanActiveTask(t, fixture, policy, retired.Head.RootTaskRef.TaskPath, retiredAlternateSuccessorPath)
			if mode == "unfinished-runtime" {
				cfg := WorkflowTaskConfig(store.config, store.identity, *retired.Head.RootTaskRef)
				cfg.RepoRoot = store.identity.PrimaryRoot
				runtime := state.AttachStateStore(cfg)
				if err := runtime.Write("task.id", "unfinished"); err != nil {
					t.Fatal(err)
				}
				if err := runtime.Write("task.status", string(state.TaskStatusAwaitingParentCompletion)); err != nil {
					t.Fatal(err)
				}
			}
			selected, canonical, err := WorkflowAuthorityTask(store.config)
			if err != nil || !canonical || selected.TaskPath != retiredAlternateSuccessorPath {
				t.Fatalf("successor read authority: %v %v %v", selected, canonical, err)
			}
			_, err = store.AdoptExternalAdvancement(ExternalAdvancementInput{ExpectedGeneration: retired.Head.ControllerGeneration, Policy: policy})
			if mode == "unfinished-runtime" {
				if err == nil {
					t.Fatal("metadata switch admitted an unfinished predecessor Runtime")
				}
				head, err := store.LoadHead()
				if err != nil || !reflect.DeepEqual(head, retired.Head) {
					t.Fatal("rejected metadata switch changed controller authority")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			head, err := store.LoadHead()
			if err != nil || head.RootTaskRef == nil || !head.RootTaskRef.Equal(selected) {
				t.Fatal("metadata switch did not bind the selected successor")
			}
			after, err := store.LoadEvidenceObject(*head.MetadataLineageRef)
			if err != nil || !reflect.DeepEqual(terminal, after) {
				t.Fatal("metadata switch rewrote terminal history")
			}
			started, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: head.ControllerGeneration})
			if err != nil || started.Admission == nil || !started.Admission.Attempt.SemanticTaskRef.Equal(selected) {
				t.Fatalf("switched successor was not startable: %v", err)
			}
		})
	}
}

func TestRetiredSourceSuccessorRequiresFreshInstallEvidence(t *testing.T) {
	t.Parallel()
	fixture, policy, published, retiredInstall := retiredInstallRequiredMetadataFixture(t)
	store := fixture.store
	tip := pullRetiredExternalSource(t, fixture, policy)
	adopted, err := store.AdoptRetiredExternalSource(RetiredSourceAdvancementInput{ExpectedGeneration: published.Head.ControllerGeneration, Policy: policy, SourceOID: tip})
	if err != nil {
		t.Fatal(err)
	}
	started, err := store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: adopted.Head.ControllerGeneration})
	if err != nil {
		t.Fatal(err)
	}
	successor := AcceptedCandidate{AttemptID: started.Admission.Attempt.AttemptID, SnapshotID: started.Admission.Snapshot.ID, BaseOID: tip, TreeOID: controllerGitOutput(t, store.identity.PrimaryRoot, "rev-parse", tip+"^{tree}"), Policy: policy}
	fresh := storePublicationTestEvidence(t, store, successor)
	if err := store.validateCandidateEvidence(successor, append(fresh, retiredInstall)); err == nil {
		t.Fatal("retired install PASS transferred to the synchronized source candidate")
	}
	if err := store.validateCandidateEvidence(successor, append(fresh, storeRetiredInstallEvidence(t, store, successor))); err != nil {
		t.Fatalf("fresh successor install evidence was rejected: %v", err)
	}
}

func retiredInstallRequiredMetadataFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy, ExecutionOperationResult, EvidenceObjectRef) {
	t.Helper()
	fixture, policy := newPublicationTestFixture(t)
	policy.RequireInstall = true
	source := publicationTestEdit(t, fixture, "result.txt", "retired result\n")
	base := publicationTestCandidateView(t, fixture.store, source, policy)
	install := storeRetiredInstallEvidence(t, fixture.store, base)
	refs := append(storePublicationTestEvidence(t, fixture.store, base), install)
	seedFinalValidationNoneObligation(t, fixture.store, source)
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "install required result", Policy: policy, Evidence: refs})
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
	retired, err := prepareCompletionFixture(fixture.store, TerminalTaskInput{ExpectedGeneration: promoted.Head.ControllerGeneration, ProjectSnapshotID: promoted.Head.ProjectSnapshotID, CandidateID: candidate.CandidateID, TaskRef: candidate.TaskRef})
	if err != nil {
		t.Fatal(err)
	}
	published, err := fixture.store.RecoverExecutionOperation(retired.TransitionID)
	if err != nil {
		t.Fatal(err)
	}
	return fixture, policy, published, install
}

func publicationTestCandidateView(t *testing.T, store *Store, source Admission, policy PublicationPolicy) AcceptedCandidate {
	t.Helper()
	tree, err := store.PreviewCandidateTree(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	return AcceptedCandidate{AttemptID: source.Attempt.AttemptID, SnapshotID: source.Snapshot.ID, BaseOID: source.Head.IntegrationTip, TreeOID: tree}
}

func storeRetiredInstallEvidence(t *testing.T, store *Store, c AcceptedCandidate) EvidenceObjectRef {
	t.Helper()
	artifact, err := store.PutEvidenceObject("install", "text/plain", "install:"+c.SnapshotID, true, []byte("installed runtime proof "+c.SnapshotID))
	if err != nil {
		t.Fatal(err)
	}
	ref, err := store.StoreCandidateEvidence(CandidateEvidence{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: store.identity.LineageID, AttemptID: c.AttemptID, SnapshotID: c.SnapshotID, CandidateID: c.CandidateID, BaseOID: c.BaseOID, TreeOID: c.TreeOID, Kind: "install", Result: "pass", Artifact: artifact})
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func TestRetiredMetadataSwitchRechecksRuntimeAfterPrepare(t *testing.T) {
	t.Parallel()
	fixture, policy, retired := retiredSuccessorSwitchFixture(t)
	store := fixture.store
	switchPlanActiveTask(t, fixture, policy, retired.Head.RootTaskRef.TaskPath, retiredAlternateSuccessorPath)
	authority, err := ResolveCommittedTaskAuthority(store.identity.PrimaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	tip := authority.Snapshot.HeadOID
	op, err := store.retiredAdvancementOperation(retired.Head, policy, authority, tip, tip, publicationAdopt)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.sealRetiredAdvancementPlan(retired.Head, &op, tip); err != nil {
		t.Fatal(err)
	}
	if err := store.prepareExecutionOperation(&op, retired.Head); err != nil {
		t.Fatal(err)
	}
	cfg := WorkflowTaskConfig(store.config, store.identity, *retired.Head.RootTaskRef)
	cfg.RepoRoot = store.identity.PrimaryRoot
	runtime := state.AttachStateStore(cfg)
	if err := runtime.Write("task.id", "unfinished-after-prepare"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("task.status", string(state.TaskStatusAwaitingParentCompletion)); err != nil {
		t.Fatal(err)
	}
	if _, err := store.RecoverExecutionOperation(op.Transition.TransitionID); err == nil {
		t.Fatal("metadata switch hid a Runtime that became unfinished after prepare")
	}
	after, err := store.LoadHead()
	if err != nil || after.IntegrationTip != retired.Head.IntegrationTip || !reflect.DeepEqual(after.RootTaskRef, retired.Head.RootTaskRef) || !reflect.DeepEqual(after.MetadataLineageRef, retired.Head.MetadataLineageRef) {
		t.Fatalf("rejected runtime race changed retired authority: %v", err)
	}
}
