package controller

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func advancePublicationLocalRef(t *testing.T, store *Store, policy PublicationPolicy, path, data string) string {
	t.Helper()
	root := store.identity.PrimaryRoot
	if err := os.WriteFile(filepath.Join(root, path), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, root, "add", path)
	runControllerGit(t, root, "commit", "-qm", "external fast-forward advancement")
	runControllerGit(t, root, "push", policy.Remote, "HEAD:"+policy.RemoteRef)
	return controllerGitOutput(t, root, "rev-parse", "HEAD")
}

func TestUnpublishedCandidateSourceAdvancementProjectsTypedRebind(t *testing.T) {
	t.Parallel()
	f, policy, head, candidate := acceptedAmendmentLane(t)
	tip := advancePublicationLocalRef(t, f.store, policy, "external-metadata.txt", "metadata-only external advancement\n")

	operation, err := f.store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if operation.Surface != PublicationOperationSurface || operation.Operation != PublicationOperationRebind || operation.Command == nil {
		t.Fatalf("stale candidate projection: %#v", operation)
	}
	if operation.Command.Action != PublicationOperationRebind || operation.Command.CandidateID != candidate.CandidateID || operation.Command.SourceOID != tip || operation.Command.ExpectedGeneration != head.ControllerGeneration {
		t.Fatalf("typed rebind command: %#v", operation.Command)
	}
	if err := f.store.requireCurrentCandidateSource(candidate); err == nil || !strings.Contains(err.Error(), "rebind") {
		t.Fatalf("stale candidate admitted without typed guidance: %v", err)
	}
	if _, _, err := PrepareFinalValidationWorkspace(f.store.config, "go-test"); err == nil || !strings.Contains(err.Error(), "rebind") {
		t.Fatalf("final validation workspace admitted a stale candidate source: %v", err)
	}
	if _, err := f.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil || !strings.Contains(err.Error(), "rebind") {
		t.Fatalf("stale candidate promoted without typed guidance: %v", err)
	}
	if remote := observeTestRemote(t, f.store.identity.PrimaryRoot, policy); remote != tip {
		t.Fatalf("stale-source refusals moved the remote: %s", remote)
	}

	rebound, err := f.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	next, err := f.store.LoadAcceptedCandidate(*rebound.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if next.BaseOID != tip || next.EvidenceValid || len(next.Evidence) != 0 || next.CandidateID == candidate.CandidateID {
		t.Fatalf("rebind did not rebind the advanced source and invalidate evidence: %#v", next)
	}
	if err := f.store.requireCurrentCandidateSource(next); err != nil {
		t.Fatalf("rebound candidate is still stale: %v", err)
	}
	if _, _, err := PrepareFinalValidationWorkspace(f.store.config, "go-test"); err == nil || !strings.Contains(err.Error(), "declares no go-test validation") {
		t.Fatalf("rebound candidate lost its regular validation admission: %v", err)
	}
}

func TestUnpublishedCandidateSourceDivergenceFailsClosed(t *testing.T) {
	t.Parallel()
	f, policy, head, candidate := acceptedAmendmentLane(t)
	root := f.store.identity.PrimaryRoot
	if err := os.WriteFile(filepath.Join(root, "divergent.txt"), []byte("divergence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, root, "add", "divergent.txt")
	runControllerGit(t, root, "commit", "--amend", "-qm", "divergent history rewrite")

	if _, err := f.store.ProjectParentCompletion(); err == nil || !strings.Contains(err.Error(), "not a fast-forward") {
		t.Fatalf("divergent primary admitted: %v", err)
	}
	if err := f.store.requireCurrentCandidateSource(candidate); err == nil || !strings.Contains(err.Error(), "not a fast-forward") {
		t.Fatalf("divergent primary admitted without fail-closed reason: %v", err)
	}
	if _, err := f.store.RebindUnpublishedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID}); err == nil {
		t.Fatal("divergent primary was rebound without an explained conflict")
	}
	if remote := observeTestRemote(t, root, policy); remote != head.IntegrationTip {
		t.Fatalf("divergence handling rewrote the remote: %s", remote)
	}
}

func TestAdmissionSourceAdvancementProjectsAdoptMutableBeforeAcceptance(t *testing.T) {
	t.Parallel()
	f, policy := newPublicationTestFixture(t)
	bindPublicationUpstream(t, f.store, policy)
	source := publicationTestEdit(t, f, "result.txt", "reviewed implementation\n")
	seedAwaitingCompletionRuntime(t, f.store, source, true)
	tip := pullRetiredExternalSourceChange(t, f, policy, "IMPLEMENTATION_HISTORY.md", "cross-lane parent metadata record\n")

	operation, err := f.store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if operation.Surface != PublicationOperationSurface || operation.Operation != PublicationOperationAdoptMutable || operation.Command == nil {
		t.Fatalf("advanced admission projection: %#v", operation)
	}
	if operation.Command.Action != PublicationOperationAdoptMutable || operation.Command.ExpectedGeneration != source.Head.ControllerGeneration || operation.Command.Policy == nil || operation.Command.Policy.Remote != policy.Remote {
		t.Fatalf("typed adopt-mutable command: %#v", operation.Command)
	}

	stale := CandidateAcceptanceInput{Message: "stale acceptance", Policy: policy, RequiredValidations: declaredNoValidations()}
	if _, err := f.store.AcceptExecutionCandidate(source, stale); err == nil || !strings.Contains(err.Error(), "adopt the external advancement") {
		t.Fatalf("stale source accepted without adopt guidance: %v", err)
	}
	if _, err := f.store.AcceptParentCompletion(source, stale); err == nil || !strings.Contains(err.Error(), "adopt the external advancement") {
		t.Fatalf("stale completion accepted without adopt guidance: %v", err)
	}
	if remote := observeTestRemote(t, f.store.identity.PrimaryRoot, policy); remote != tip {
		t.Fatalf("stale-source refusals moved the remote: %s", remote)
	}

	adopted, err := f.store.AdoptMutableExternalAdvancement(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := f.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: adopted.Head.ControllerGeneration, SuspensionID: adopted.Suspension.SnapshotID})
	if err != nil || resumed.Admission == nil {
		t.Fatalf("adopted advancement could not resume execution: %v", err)
	}
	if _, err := f.store.AcceptExecutionCandidate(*resumed.Admission, CandidateAcceptanceInput{Message: "reused review", Policy: policy, RequiredValidations: declaredNoValidations(), Evidence: publicationTestEvidence(t, f.store, *resumed.Admission, policy)}); err == nil || !strings.Contains(err.Error(), "another attempt") {
		t.Fatalf("resumed attempt reused the pre-pull Runtime review: %v", err)
	}
	seedAwaitingCompletionRuntime(t, f.store, *resumed.Admission, true)
	converged, err := f.store.AcceptExecutionCandidate(*resumed.Admission, CandidateAcceptanceInput{Message: "adopted advancement", Policy: policy, RequiredValidations: declaredNoValidations(), Evidence: publicationTestEvidence(t, f.store, *resumed.Admission, policy)})
	if err != nil {
		t.Fatal(err)
	}
	fresh, err := f.store.LoadAcceptedCandidate(*converged.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if fresh.BaseOID != tip || fresh.AttemptID == source.Attempt.AttemptID {
		t.Fatalf("adopted acceptance did not converge on the advanced source %s: %#v", tip, fresh)
	}
	if remote := observeTestRemote(t, f.store.identity.PrimaryRoot, policy); remote != tip {
		t.Fatalf("advancement adoption moved the remote: %s", remote)
	}
}

func TestPulledAdvancementAttributionRefusesUnprovenMutations(t *testing.T) {
	t.Parallel()
	causes := map[string]string{
		"overlapping-uncommitted": "overlaps uncommitted execution path",
		"remote-ahead":            "does not match the configured remote tip",
		"divergent":               "not a fast-forward of the recorded execution base",
		"in-flight-lease":         "requires quiescent execution",
		"stale-base":              "requires the recorded execution base",
	}
	for _, mode := range []string{"overlapping-uncommitted", "remote-ahead", "divergent", "in-flight-lease", "stale-base"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			f, policy := newPublicationTestFixture(t)
			bindPublicationUpstream(t, f.store, policy)
			source := publicationTestEdit(t, f, "result.txt", "reviewed implementation\n")
			if mode == "in-flight-lease" {
				bound, err := f.store.BindModelCall(source)
				if err != nil {
					t.Fatal(err)
				}
				source = bound
			}
			seedAwaitingCompletionRuntime(t, f.store, source, true)
			root := f.store.identity.PrimaryRoot
			switch mode {
			case "overlapping-uncommitted":
				pullRetiredExternalSourceChange(t, f, policy, "IMPLEMENTATION_HISTORY.md", "cross-lane parent metadata record\n")
				if err := os.WriteFile(filepath.Join(root, "IMPLEMENTATION_HISTORY.md"), []byte("worker-local metadata edit\n"), 0o644); err != nil {
					t.Fatal(err)
				}
			case "remote-ahead":
				pullRetiredExternalSourceChange(t, f, policy, "IMPLEMENTATION_HISTORY.md", "cross-lane parent metadata record\n")
				advancePublicationTestRemote(t, f, policy, "unpulled-external.txt", "advancement beyond the pulled primary\n")
			case "in-flight-lease", "stale-base":
				pullRetiredExternalSourceChange(t, f, policy, "IMPLEMENTATION_HISTORY.md", "cross-lane parent metadata record\n")
			default:
				if err := os.WriteFile(filepath.Join(root, "divergent.txt"), []byte("divergence\n"), 0o644); err != nil {
					t.Fatal(err)
				}
				runControllerGit(t, root, "add", "divergent.txt")
				runControllerGit(t, root, "commit", "--amend", "-qm", "divergent history rewrite")
			}
			if mode == "stale-base" {
				lease, err := f.store.loadLease(source.Head.LiveLeaseID)
				if err != nil {
					t.Fatal(err)
				}
				lease.ExpectedBaseOID = controllerGitOutput(t, root, "rev-parse", source.Head.IntegrationTip+"^")
				if err := f.store.writeLease(lease); err != nil {
					t.Fatal(err)
				}
			}
			before, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			remoteBefore := observeTestRemote(t, root, policy)

			_, admissionErr := f.store.LiveMutableAdvancementAdmission(policy)
			if admissionErr == nil || !strings.Contains(admissionErr.Error(), causes[mode]) {
				t.Fatalf("%s adopt-mutable admission did not refuse with its cause: %v", mode, admissionErr)
			}
			operation, projectionErr := f.store.ProjectParentCompletion()
			if operation.Operation == PublicationOperationAdoptMutable && operation.Command != nil {
				t.Fatalf("%s was projected as an executable adopt-mutable: %#v", mode, operation)
			}
			var refusal *ProjectedAdvancementRefusal
			if errors.As(projectionErr, &refusal) != (mode != "divergent") {
				t.Fatalf("%s projection did not keep its refusal boundary: %v", mode, projectionErr)
			}
			if mode != "divergent" && !strings.Contains(projectionErr.Error(), admissionErr.Error()) {
				t.Fatalf("%s projection lost the admission cause: %v", mode, projectionErr)
			}
			if mode != "stale-base" {
				if _, err := f.store.AdoptMutableExternalAdvancement(source, policy); err == nil {
					t.Fatal("unproven primary mutation was adopted as an external pull")
				}
			}
			after, err := f.store.LoadHead()
			if err != nil || after.Status != ControllerStatusActive || after.PendingTransitionID != "" || after.LiveAttemptID != before.LiveAttemptID || after.IntegrationTip != before.IntegrationTip {
				t.Fatalf("refused attribution changed controller authority: %v %#v", err, after)
			}
			if remote := observeTestRemote(t, root, policy); remote != remoteBefore {
				t.Fatalf("refused attribution moved the remote: %s", remote)
			}
		})
	}
}

func TestProjectedAdoptMutableReverifiesAdvancementAtExecution(t *testing.T) {
	t.Parallel()
	f, policy := newPublicationTestFixture(t)
	bindPublicationUpstream(t, f.store, policy)
	source := publicationTestEdit(t, f, "result.txt", "reviewed implementation\n")
	seedAwaitingCompletionRuntime(t, f.store, source, true)
	root := f.store.identity.PrimaryRoot
	pulled := pullRetiredExternalSourceChange(t, f, policy, "IMPLEMENTATION_HISTORY.md", "cross-lane parent metadata record\n")

	operation, err := f.store.ProjectParentCompletion()
	if err != nil || operation.Operation != PublicationOperationAdoptMutable || operation.Command == nil {
		t.Fatalf("executable advancement lost its typed adopt-mutable: %#v %v", operation, err)
	}
	if _, err := f.store.LiveMutableAdvancementAdmission(policy); err != nil {
		t.Fatalf("verified advancement stopped being admissible: %v", err)
	}

	raced := advancePublicationTestRemote(t, f, policy, "unpulled-external.txt", "advancement beyond the pulled primary\n")
	if _, err := f.store.planMutableAdvancement(source, policy, pulled); err == nil || !strings.Contains(err.Error(), "pulled source") {
		t.Fatalf("post-pull planning must refuse a remote change after its initial guard: %v", err)
	}
	_, admissionErr := f.store.LiveMutableAdvancementAdmission(policy)
	if admissionErr == nil {
		t.Fatal("admission accepted a raced remote")
	}
	if _, err := f.store.AdoptMutableExternalAdvancement(source, policy); err == nil || err.Error() != admissionErr.Error() {
		t.Fatalf("stale adopt-mutable execution did not refuse at the admission boundary: %v", err)
	}
	head, err := f.store.LoadHead()
	if err != nil || head.IntegrationTip != source.Head.IntegrationTip || head.PendingTransitionID != "" || head.LiveLeaseID != source.Lease.LeaseID || head.AcceptedCandidateRef != nil {
		t.Fatalf("refused race changed controller authority: %v %#v", err, head)
	}
	if remote := observeTestRemote(t, root, policy); remote != raced {
		t.Fatalf("refused race moved the remote: %s", remote)
	}

	runControllerGit(t, root, "pull", "--ff-only", policy.Remote, policy.RemoteRef)
	adopted, err := f.store.AdoptMutableExternalAdvancement(source, policy)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.Head.IntegrationTip != raced || adopted.Head.LiveLeaseID != "" || adopted.Head.AcceptedCandidateRef != nil || adopted.Head.PendingTransitionID != "" {
		t.Fatalf("resynchronized adopt-mutable did not converge on the remote tip: %#v", adopted.Head)
	}
}

func TestAdmissionSourceDivergenceDefersConflictToAcceptanceGuard(t *testing.T) {
	t.Parallel()
	f, policy := newPublicationTestFixture(t)
	bindPublicationUpstream(t, f.store, policy)
	source := publicationTestEdit(t, f, "result.txt", "reviewed implementation\n")
	seedAwaitingCompletionRuntime(t, f.store, source, true)
	root := f.store.identity.PrimaryRoot
	if err := os.WriteFile(filepath.Join(root, "divergent.txt"), []byte("divergence\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, root, "add", "divergent.txt")
	runControllerGit(t, root, "commit", "--amend", "-qm", "divergent history rewrite")

	operation, projectionErr := f.store.ProjectParentCompletion()
	if projectionErr == nil && operation.Operation == PublicationOperationAdoptMutable {
		t.Fatalf("conflicting primary was projected as adoptable advancement: %#v", operation)
	}
	if projectionErr != nil && strings.Contains(projectionErr.Error(), "not a fast-forward of the candidate base") {
		t.Fatalf("admission projection classified the divergence instead of deferring it: %v", projectionErr)
	}
	if _, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{Message: "divergent acceptance", Policy: policy, RequiredValidations: declaredNoValidations()}); err == nil || !strings.Contains(err.Error(), "not a fast-forward") {
		t.Fatalf("divergent primary admitted without fail-closed reason: %v", err)
	}
	if remote := observeTestRemote(t, root, policy); remote != source.Head.IntegrationTip {
		t.Fatalf("divergence handling moved the remote: %s", remote)
	}
}

func TestCandidateOwnPromotionKeepsSourceContinuity(t *testing.T) {
	t.Parallel()
	f, policy, head, candidate := acceptedAmendmentLane(t)
	promoted, err := f.store.PromoteAcceptedCandidate(PublicationInput{ExpectedGeneration: head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	current, err := f.store.LoadAcceptedCandidate(*promoted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if current.State != candidatePromoted || current.CommitOID != candidate.CommitOID {
		t.Fatalf("own promotion lost the candidate identity: %#v", current)
	}
	if err := f.store.requireCurrentCandidateSource(current); err != nil {
		t.Fatalf("own promotion treated as stale source: %v", err)
	}
	operation, err := f.store.ProjectParentCompletion()
	if err != nil {
		t.Fatal(err)
	}
	if operation.Operation == PublicationOperationRebind {
		t.Fatalf("own promotion projected a rebind: %#v", operation)
	}
	if operation.Operation != PublicationOperationPublish || operation.Command == nil || operation.Command.Action != PublicationOperationPublish {
		t.Fatalf("own promotion lost its publish projection: %#v", operation)
	}
	if remote := observeTestRemote(t, f.store.identity.PrimaryRoot, policy); remote != head.IntegrationTip {
		t.Fatalf("promotion advanced the remote: %s", remote)
	}
}

func TestPulledAdvancementRejectsMissingLosslessBaseline(t *testing.T) {
	t.Parallel()
	for _, missing := range []string{"index", "worktree"} {
		t.Run(missing, func(t *testing.T) {
			t.Parallel()
			f, policy := newPublicationTestFixture(t)
			bindPublicationUpstream(t, f.store, policy)
			source := publicationTestEdit(t, f, "result.txt", "reviewed implementation\n")
			seedAwaitingCompletionRuntime(t, f.store, source, true)
			switch missing {
			case "index":
				source.Attempt.BaselineTrees.IndexTree = ""
			case "worktree":
				source.Attempt.BaselineTrees.WorktreeTree = ""
			}
			if err := f.store.writeAttempt(source.Attempt); err != nil {
				t.Fatal(err)
			}
			root := f.store.identity.PrimaryRoot
			tip := pullRetiredExternalSourceChange(t, f, policy, "IMPLEMENTATION_HISTORY.md", "external parent metadata\n")
			beforeHead, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			beforeAttempt, err := f.store.loadAttempt(source.Attempt.AttemptID)
			if err != nil {
				t.Fatal(err)
			}
			beforeLease, err := f.store.loadLease(source.Lease.LeaseID)
			if err != nil {
				t.Fatal(err)
			}
			beforeSource, err := CaptureWorkspaceSnapshot(root)
			if err != nil {
				t.Fatal(err)
			}

			_, admissionErr := f.store.LiveMutableAdvancementAdmission(policy)
			if admissionErr == nil || !strings.Contains(admissionErr.Error(), "lossless execution baseline") {
				t.Fatalf("missing %s baseline admitted: %v", missing, admissionErr)
			}
			projected, projectionErr := f.store.ProjectParentCompletion()
			var refusal *ProjectedAdvancementRefusal
			if !errors.As(projectionErr, &refusal) || !strings.Contains(projectionErr.Error(), admissionErr.Error()) {
				t.Fatalf("missing %s baseline lost typed projection refusal: %#v %v", missing, projected, projectionErr)
			}
			if projected.Operation == PublicationOperationAdoptMutable || projected.Command != nil {
				t.Fatalf("missing %s baseline projected executable action: %#v", missing, projected)
			}
			if _, err := f.store.AdoptMutableExternalAdvancement(source, policy); err == nil || !strings.Contains(err.Error(), "lossless execution baseline") {
				t.Fatalf("missing %s baseline directly adopted: %v", missing, err)
			}

			afterHead, err := f.store.LoadHead()
			if err != nil || !reflect.DeepEqual(afterHead, beforeHead) {
				t.Fatalf("refused %s baseline changed head: %v %#v", missing, err, afterHead)
			}
			afterAttempt, err := f.store.loadAttempt(source.Attempt.AttemptID)
			if err != nil || !reflect.DeepEqual(afterAttempt, beforeAttempt) {
				t.Fatalf("refused %s baseline changed attempt: %v %#v", missing, err, afterAttempt)
			}
			afterLease, err := f.store.loadLease(source.Lease.LeaseID)
			if err != nil || !reflect.DeepEqual(afterLease, beforeLease) {
				t.Fatalf("refused %s baseline changed lease: %v %#v", missing, err, afterLease)
			}
			afterSource, err := CaptureWorkspaceSnapshot(root)
			if err != nil || !reflect.DeepEqual(afterSource, beforeSource) {
				t.Fatalf("refused %s baseline changed source: %v %#v", missing, err, afterSource)
			}
			if remote := observeTestRemote(t, root, policy); remote != tip {
				t.Fatalf("refused %s baseline moved remote from %s to %s", missing, tip, remote)
			}
		})
	}
}
