package controller

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const requestAdvancementInstruction = "preserve the exact user request through external advancement"

func newRequestAdvancementFixture(t *testing.T) (findingAcceptanceFixture, PublicationPolicy) {
	t.Helper()
	repo := newControllerRepo(t)
	runControllerGit(t, repo, "rm", "IMPLEMENTATION_PLAN.local.md", "IMPLEMENTATION_TASKS/root.md")
	runControllerGit(t, repo, "commit", "-qm", "ordinary request repository")
	cfg := controllerTestConfig(repo, filepath.Join(t.TempDir(), "sessions"))
	source, err := ActivateRequest(cfg, requestAdvancementInstruction)
	if err != nil {
		t.Fatal(err)
	}
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	seedFinalValidationNoneObligation(t, store, source)
	return configurePublicationTestFixture(t, findingAcceptanceFixture{store: store, source: source})
}

func assertRequestAdvancementAuthority(t *testing.T, store *Store, head RepositoryControllerHead, tip string) {
	t.Helper()
	if head.IntegrationTip != tip || head.RootTaskRef == nil || head.ExecutionTaskRef == nil ||
		!head.RootTaskRef.Equal(*head.ExecutionTaskRef) || head.RootTaskRef.RequestID == "" {
		t.Fatalf("request advancement lost active identity: %+v", head)
	}
	project, err := store.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	if project.HeadOID != tip || project.UserRequest == nil || project.UserRequest.Content != requestAdvancementInstruction ||
		project.UserRequest.RequestID != head.ExecutionTaskRef.RequestID || project.UserRequest.RequestID != head.RootTaskRef.RequestID ||
		len(project.Tasks) != 1 || !project.Tasks[0].Equal(*head.ExecutionTaskRef) ||
		project.PlanDigest != "" || project.SemanticGraphDigest != "" || project.ScheduleDigest != "" {
		t.Fatalf("request advancement minted project authority or changed the request: %+v", project)
	}
	if err := store.ValidateRequestTask(*head.ExecutionTaskRef, requestAdvancementInstruction); err != nil {
		t.Fatal(err)
	}
	if err := store.ValidateRequestTask(*head.ExecutionTaskRef, requestAdvancementInstruction+" tampered"); err == nil {
		t.Fatal("request body mutation was admitted after advancement")
	}
	for _, path := range []string{"IMPLEMENTATION_PLAN.local.md", "IMPLEMENTATION_TASKS/root.md", ".glm-worker-repository-harness"} {
		if _, err := os.Stat(filepath.Join(store.identity.PrimaryRoot, path)); !os.IsNotExist(err) {
			t.Fatalf("request advancement created project metadata %s: %v", path, err)
		}
	}
}

func TestRequestPulledUnpublishedCandidateRebindPreservesAuthority(t *testing.T) {
	for _, locallyPromoted := range []bool{false, true} {
		name := "prepared"
		if locallyPromoted {
			name = "locally-promoted"
		}
		t.Run(name, func(t *testing.T) {
			f, policy := newRequestAdvancementFixture(t)
			source := publicationTestEdit(t, f, "candidate.txt", "request implementation\n")
			f.source = source
			accepted, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
				RequiredValidations: declaredNoValidations(), Message: "request implementation",
				Policy: policy, Evidence: publicationTestEvidence(t, f.store, source, policy),
			})
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := f.store.LoadAcceptedCandidate(*accepted.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			if locallyPromoted {
				accepted, err = f.store.PromoteAcceptedCandidate(PublicationInput{
					ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID,
				})
				if err != nil {
					t.Fatal(err)
				}
			}
			remote := advancePublicationTestRemote(t, f, policy, "external.txt", "upstream advancement\n")
			input := PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}
			var rebound ExecutionOperationResult
			if locallyPromoted {
				rebound, err = f.store.RebindUnpublishedCandidate(input)
			} else {
				runControllerGit(t, f.store.identity.PrimaryRoot, "pull", "--ff-only", policy.Remote, policy.RemoteRef)
				rebound, err = f.store.RebindPulledUnpublishedCandidate(input, remote)
			}
			if err != nil {
				t.Fatalf("request rebind after external fast-forward: %v", err)
			}
			assertRequestAdvancementAuthority(t, f.store, rebound.Head, remote)
			current, err := f.store.LoadAcceptedCandidate(*rebound.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			if current.BaseOID != remote || current.EvidenceValid || len(current.Evidence) != 0 ||
				current.CandidateID == candidate.CandidateID || !current.TaskRef.Equal(candidate.TaskRef) {
				t.Fatalf("rebound request reused stale candidate/proof or lost request: %+v", current)
			}
			if _, err := f.store.PublishAcceptedCandidate(PublicationInput{
				ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: current.CandidateID,
			}); err == nil {
				t.Fatal("request published without new evidence after rebind")
			}
			reentered, err := f.store.ReenterAcceptedCandidate(PublicationInput{
				ExpectedGeneration: rebound.Head.ControllerGeneration, CandidateID: current.CandidateID,
			})
			if err != nil {
				t.Fatal(err)
			}
			cleaned, err := f.store.CleanupExecution(CleanupExecutionInput{
				ExpectedGeneration: reentered.Head.ControllerGeneration,
				WorkspaceID: f.source.Workspace.ID, SealRef: current.SealRef,
			})
			if err != nil {
				t.Fatal(err)
			}
			live, err := f.store.MaterializeExecution(MaterializeExecutionInput{
				ExpectedGeneration: cleaned.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID,
			})
			if err != nil {
				t.Fatal(err)
			}
			if live.Admission == nil || !live.Admission.Attempt.SemanticTaskRef.Equal(candidate.TaskRef) ||
				live.Admission.Lease.LeaseID == source.Lease.LeaseID {
				t.Fatalf("request did not recover with a fresh source lease: %+v", live.Admission)
			}
			for _, path := range []string{"candidate.txt", "external.txt"} {
				if _, err := os.Stat(filepath.Join(live.Admission.Workspace.Root, path)); err != nil {
					t.Fatalf("recovered request lost %s: %v", path, err)
				}
			}
		})
	}
}

func TestRequestMutableAndSuspendedExternalAdvancement(t *testing.T) {
	f, policy := newRequestAdvancementFixture(t)
	source := publicationTestEdit(t, f, "unfinished.txt", "live request work\n")
	f.source = source
	remote := advancePublicationTestRemote(t, f, policy, "external.txt", "upstream one\n")
	runControllerGit(t, f.store.identity.PrimaryRoot, "pull", "--ff-only", policy.Remote, policy.RemoteRef)
	if _, err := f.store.LiveMutableAdvancementAdmission(policy); err != nil {
		t.Fatalf("request pull cannot be attributed to its original execution: %v", err)
	}
	adopted, err := f.store.AdoptPulledMutableExternalAdvancement(source, policy, remote)
	if err != nil {
		t.Fatalf("request external pull could not adopt live source: %v", err)
	}
	assertRequestAdvancementAuthority(t, f.store, adopted.Head, remote)
	if adopted.Suspension == nil || adopted.Head.LiveLeaseID != "" {
		t.Fatalf("request live source was not suspended: %+v", adopted)
	}
	if _, err := f.store.AdmitMutation(source.MutationAuthority(), source.Workspace, source.Snapshot); err == nil {
		t.Fatal("stale request lease admitted after external advancement")
	}
	nextRemote := advancePublicationTestRemote(t, f, policy, "external-next.txt", "upstream two\n")
	second, err := f.store.AdoptExternalAdvancement(ExternalAdvancementInput{
		ExpectedGeneration: adopted.Head.ControllerGeneration, Policy: policy,
	})
	if err != nil {
		t.Fatalf("suspended request did not adopt next advancement: %v", err)
	}
	assertRequestAdvancementAuthority(t, f.store, second.Head, nextRemote)
	resumed, err := f.store.MaterializeExecution(MaterializeExecutionInput{
		ExpectedGeneration: second.Head.ControllerGeneration, SuspensionID: adopted.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resumed.Admission == nil || resumed.Admission.Lease.LeaseID == source.Lease.LeaseID ||
		!resumed.Admission.Attempt.SemanticTaskRef.Equal(source.Attempt.SemanticTaskRef) {
		t.Fatalf("advanced request materialized with old or different authority: %+v", resumed.Admission)
	}
}

func TestRequestAdvancementRejectsForeignAuthorityAndHarness(t *testing.T) {
	f, policy := newRequestAdvancementFixture(t)
	head := f.source.Head
	remote := advancePublicationTestRemote(t, f, policy, "external.txt", "upstream\n")
	observed, err := observePublicationRemote(f.store.identity.PrimaryRoot, policy)
	if err != nil || observed != remote {
		t.Fatalf("request remote was not fetched for the authority test: %s, %v", observed, err)
	}
	if _, err := f.store.publicationProjectAdvancement(head, remote); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		edit func(*RepositoryControllerHead)
	}{
		{name: "request digest", edit: func(h *RepositoryControllerHead) { h.ExecutionTaskRef.ContractDigest = strings.Repeat("f", 64) }},
		{name: "request identity", edit: func(h *RepositoryControllerHead) { h.ExecutionTaskRef.RequestID += "-forged" }},
		{name: "stale integration", edit: func(h *RepositoryControllerHead) { h.IntegrationTip = remote }},
		{name: "missing root", edit: func(h *RepositoryControllerHead) { h.RootTaskRef = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			forged := head
			root := *head.RootTaskRef
			task := *head.ExecutionTaskRef
			forged.RootTaskRef = &root
			forged.ExecutionTaskRef = &task
			tc.edit(&forged)
			if _, err := f.store.publicationProjectAdvancement(forged, remote); err == nil {
				t.Fatalf("forged request advancement %s admitted", tc.name)
			}
		})
	}
	if _, err := f.store.RebindPulledUnpublishedCandidate(PublicationInput{
		ExpectedGeneration: head.ControllerGeneration, CandidateID: "stale",
	}, remote); err == nil {
		t.Fatal("stale candidate ID admitted after remote advancement")
	}
	if err := os.WriteFile(filepath.Join(f.store.identity.PrimaryRoot, ".glm-worker-repository-harness"),
		[]byte("malicious marker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := f.store.publicationProjectAdvancement(head, remote); err == nil {
		t.Fatal("request advancement accepted an added harness marker")
	}
	current, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if current.ControllerGeneration != head.ControllerGeneration ||
		current.IntegrationTip != head.IntegrationTip || current.ProjectSnapshotID != head.ProjectSnapshotID {
		t.Fatalf("rejected request authority changed controller head: %+v", current)
	}
}

func TestRequestPostPullRebindRefusesStaleAndDivergedSource(t *testing.T) {
	for _, tc := range []struct {
		name string
		move func(*testing.T, string, PublicationPolicy, string)
	}{
		{
			name: "stale source OID",
			move: func(_ *testing.T, _ string, _ PublicationPolicy, _ string) {},
		},
		{
			name: "divergent primary",
			move: func(t *testing.T, root string, _ PublicationPolicy, _ string) {
				runControllerGit(t, root, "commit", "--allow-empty", "-qm", "divergent local history")
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, policy := newRequestAdvancementFixture(t)
			source := publicationTestEdit(t, f, "candidate.txt", "request implementation\n")
			f.source = source
			accepted, err := f.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
				RequiredValidations: declaredNoValidations(), Message: "request implementation",
				Policy: policy, Evidence: publicationTestEvidence(t, f.store, source, policy),
			})
			if err != nil {
				t.Fatal(err)
			}
			candidate, err := f.store.LoadAcceptedCandidate(*accepted.CandidateRef)
			if err != nil {
				t.Fatal(err)
			}
			remote := advancePublicationTestRemote(t, f, policy, "external.txt", "external change\n")
			root := f.store.identity.PrimaryRoot
			if tc.name == "divergent primary" {
				tc.move(t, root, policy, remote)
			} else {
				runControllerGit(t, root, "pull", "--ff-only", policy.Remote, policy.RemoteRef)
			}
			sourceOID := remote
			if tc.name == "stale source OID" {
				sourceOID = accepted.Head.IntegrationTip
			}
			if _, err := f.store.RebindPulledUnpublishedCandidate(PublicationInput{
				ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID,
			}, sourceOID); err == nil {
				t.Fatalf("invalid pull binding admitted: %s", tc.name)
			}
			after, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if after.ControllerGeneration != accepted.Head.ControllerGeneration ||
				after.IntegrationTip != accepted.Head.IntegrationTip ||
				after.ProjectSnapshotID != accepted.Head.ProjectSnapshotID ||
				after.AcceptedCandidateRef == nil ||
				after.AcceptedCandidateRef.Digest != accepted.Head.AcceptedCandidateRef.Digest {
				t.Fatalf("rejected request rebind changed controller authority: %+v", after)
			}
		})
	}
}
