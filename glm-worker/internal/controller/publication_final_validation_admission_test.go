package controller

import (
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func TestAcceptedCandidateValidationLookupUsesRealControllerStateBase(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		forms   []FinalValidationRequest
		request string
		lookup  CandidateValidationLookup
	}{
		{"none declared", nil, "go-test", CandidateValidationFormAbsent},
		{"only go test", []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}}, "go-test-race", CandidateValidationFormAbsent},
		{"go test declared", []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}}, "go-test", CandidateValidationDeclared},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fixture, policy := newPublicationTestFixture(t)
			source := publicationTestEdit(t, fixture, "validation-lookup.txt", "candidate source\n")
			accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
				Message: "validation obligation lookup", Policy: policy,
				Evidence:            publicationTestEvidence(t, fixture.store, source, policy),
				RequiredValidations: &RequiredValidationDeclaration{Forms: tc.forms},
			})
			if err != nil || accepted.CandidateRef == nil {
				t.Fatalf("real candidate acceptance: %v", err)
			}
			request, lookup, err := AcceptedCandidateValidationRequest(fixture.store.config, tc.request)
			if err != nil || lookup != tc.lookup {
				t.Fatalf("real Controller lookup = %#v %v %v", request, lookup, err)
			}
			if lookup == CandidateValidationDeclared && request.WorkingDir != "." {
				t.Fatalf("candidate validation declaration changed: %#v", request)
			}
			foreign := fixture.store.config
			foreign.StateBase = filepath.Join(t.TempDir(), "foreign-state")
			if _, foreignLookup, err := AcceptedCandidateValidationRequest(foreign, tc.request); err == nil && foreignLookup == CandidateValidationDeclared {
				t.Fatal("unbound controller state reused a foreign accepted candidate")
			}
		})
	}
}

func TestAcceptedCandidateValidationLookupRejectsForeignRepository(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "candidate.txt", "source\n")
	if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "foreign repo", Policy: policy, Evidence: publicationTestEvidence(t, fixture.store, source, policy),
		RequiredValidations: &RequiredValidationDeclaration{Forms: []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}}},
	}); err != nil {
		t.Fatal(err)
	}
	foreign := fixture.store.config
	foreign.RepoRoot = t.TempDir()
	foreign.RepoHash = config.RepoHashFor(foreign.RepoRoot)
	if _, lookup, err := AcceptedCandidateValidationRequest(foreign, "go-test"); err == nil && lookup == CandidateValidationDeclared {
		t.Fatal("different repository selected the accepted candidate from Controller StateBase")
	}
}
