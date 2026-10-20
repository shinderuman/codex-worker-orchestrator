package controller

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func seedDeclaredFinalValidation(t *testing.T, store *Store, source Admission, form string) {
	t.Helper()
	cfg := WorkflowTaskConfig(store.config, store.identity, source.Attempt.SemanticTaskRef)
	runtime, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write(state.FinalValidationObligationKey, `{"Form":"`+form+`","WorkingDir":"."}`); err != nil {
		t.Fatal(err)
	}
}

func promoteFinalValidationSource(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, source Admission, additions []string) (ExecutionOperationResult, AcceptedCandidate) {
	t.Helper()
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "final validation source", Policy: policy,
		Evidence:                publicationTestEvidence(t, fixture.store, source, policy),
		RequiredValidationForms: additions,
	})
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
	return promoted, candidate
}

func seedFinalValidationRun(t *testing.T, store *Store, form, status string, mutate func(*qualitygate.RunRecord)) qualitygate.RunRecord {
	t.Helper()
	root, release, err := PrepareFinalValidationWorkspace(store.config, form)
	if err != nil {
		t.Fatal(err)
	}
	if release != nil {
		t.Cleanup(func() { _ = release() })
	}
	return seedFinalValidationRunAt(t, store, root, form, status, mutate)
}

func seedFinalValidationRunAt(t *testing.T, store *Store, primary, form, status string, mutate func(*qualitygate.RunRecord)) qualitygate.RunRecord {
	t.Helper()
	snapshot, err := state.CaptureGitSnapshot(primary)
	if err != nil {
		t.Fatal(err)
	}
	head, err := store.LoadHead()
	if err != nil || head.ExecutionTaskRef == nil {
		t.Fatalf("validation Task unavailable: %v", err)
	}
	cfg := WorkflowTaskConfig(store.config, store.identity, *head.ExecutionTaskRef)
	module := state.AttachStateStore(cfg)
	runID, err := qualitygate.NewRunID()
	if err != nil {
		t.Fatal(err)
	}
	absent, err := qualitygate.NoIgnoredInputs(primary)
	if err != nil {
		t.Fatal(err)
	}
	record := qualitygate.RunRecord{
		ValidationRunID:               runID,
		Form:                          form,
		Repository:                    primary,
		WorkingDir:                    primary,
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
		IgnoredInputsAbsent:           &absent,
		StartedAt:                     time.Now().UTC().Add(-time.Minute),
		Status:                        status,
	}
	if status == qualitygate.StatusPass {
		completed := time.Now().UTC()
		record.CompletedAt = &completed
		record.ExitCode = 0
		record.ExitSource = state.ValidationExitSourceTarget
	}
	logPath := filepath.Join(module.Path(qualitygate.RunDirectory), runID, qualitygate.RunLog)
	if err := os.MkdirAll(filepath.Dir(logPath), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(logPath, []byte("final validation gate output\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	record.Log = logPath
	if mutate != nil {
		mutate(&record)
	}
	data, err := qualitygate.Encode(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := module.Write(qualitygate.RunRelativePath(runID), string(data)); err != nil {
		t.Fatal(err)
	}
	return record
}

func publishFinalValidationCompletion(t *testing.T, fixture findingAcceptanceFixture, promoted ExecutionOperationResult, candidate AcceptedCandidate) (*state.StateStore, error) {
	t.Helper()
	runtime := acceptedCompletionRuntime(t, fixture.store, fixture.source)
	_, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	return runtime, err
}

func assertFinalValidationRefused(t *testing.T, fixture findingAcceptanceFixture, policy PublicationPolicy, runtime *state.StateStore, err error, form string) {
	t.Helper()
	if err == nil || (!strings.Contains(err.Error(), "final validation "+form) && !strings.Contains(err.Error(), "no validation run store is bound")) {
		t.Fatalf("publication must refuse without a current terminal PASS run for %s: %v", form, err)
	}
	remote, remoteErr := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if remoteErr != nil || remote != fixture.source.Head.IntegrationTip {
		t.Fatalf("refused publication still moved the remote: remote=%s error=%v", remote, remoteErr)
	}
	if runtime.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("refused publication completed the Runtime task")
	}
}

func TestPublicationRefusesFinalValidationFormsWithoutCurrentRun(t *testing.T) {
	t.Parallel()
	for _, form := range []string{"go-test", "go-test-race"} {
		t.Run(form, func(t *testing.T) {
			fixture, policy := newPublicationTestFixture(t)
			source := publicationTestEdit(t, fixture, "validated.txt", "final validation source\n")
			seedDeclaredFinalValidation(t, fixture.store, source, form)
			promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
			runtime, err := publishFinalValidationCompletion(t, fixture, promoted, candidate)
			assertFinalValidationRefused(t, fixture, policy, runtime, err, form)
		})
	}
}

func TestPublicationRefusesWhenOnlyAnotherFormHasCurrentRun(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "race-required source\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test-race")
	promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	root, release, err := PrepareFinalValidationWorkspace(fixture.store.config, "go-test-race")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = release() })
	seedFinalValidationRunAt(t, fixture.store, root, "go-test", qualitygate.StatusPass, nil)
	runtime, err := publishFinalValidationCompletion(t, fixture, promoted, candidate)
	assertFinalValidationRefused(t, fixture, policy, runtime, err, "go-test-race")
}

func TestPublicationCompletesWithCurrentFinalValidationRun(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "completed implementation\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	runtime, err := publishFinalValidationCompletion(t, fixture, promoted, candidate)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.TaskStatus() != state.TaskStatusComplete {
		t.Fatalf("completion with current final validation left status %s", runtime.TaskStatus())
	}
	head, err := fixture.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.PendingTransitionID != "" || head.AcceptedCandidateRef != nil {
		t.Fatalf("completion left pending publication authority: %#v", head)
	}
	remote, err := observePublicationRemote(fixture.store.identity.PrimaryRoot, policy)
	if err != nil || remote != head.IntegrationTip {
		t.Fatalf("completion remote differs: remote=%s tip=%s error=%v", remote, head.IntegrationTip, err)
	}
}

func TestFinalValidationReadinessRejectsNewerFailure(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "implementation\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	_, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	evidence, err := fixture.store.collectFinalValidationEvidence(candidate)
	if err != nil {
		t.Fatal(err)
	}
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusFail, nil)
	if err := fixture.store.verifyFinalValidationEvidence(candidate, evidence); err == nil {
		t.Fatal("saved readiness accepted a PASS superseded by a failure")
	}
}

func TestFinalValidationRejectsUnknownOrPresentIgnoredInputs(t *testing.T) {
	t.Parallel()
	for _, observed := range []*bool{nil, new(bool)} {
		t.Run(fmt.Sprint(observed == nil), func(t *testing.T) {
			fixture, policy := newPublicationTestFixture(t)
			source := publicationTestEdit(t, fixture, "validated.txt", "implementation\n")
			seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
			_, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
			seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, func(record *qualitygate.RunRecord) {
				record.IgnoredInputsAbsent = observed
			})
			if _, err := fixture.store.collectFinalValidationEvidence(candidate); err == nil {
				t.Fatal("ignored or unobserved inputs were accepted as the final candidate")
			}
		})
	}
}

func TestFailedCandidateFixCanPromoteAnotherFinalSource(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "before fix\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusFail, nil)
	if _, err := fixture.store.collectFinalValidationEvidence(candidate); err == nil {
		t.Fatal("failed candidate acquired final validation evidence")
	}
	reentered, err := fixture.store.ReenterAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	resumed, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: reentered.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	fixture.source = *resumed.Admission
	source = publicationTestEdit(t, fixture, "validated.txt", "after fix\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	_, fixed := promoteFinalValidationSource(t, fixture, policy, source, nil)
	if candidate.CommitOID == fixed.CommitOID {
		t.Fatal("Fix retained the failed candidate source")
	}
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, nil)
	if _, err := fixture.store.collectFinalValidationEvidence(fixed); err != nil {
		t.Fatal(err)
	}
}

func TestChildFinalValidationUsesFinalCandidateWithoutReplacingParentSource(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	fixture.source = publicationTestEdit(t, fixture, "root-only.txt", "unfinished root\n")
	episode := planSuspensionTestEpisode(t, fixture)
	suspended, err := fixture.store.SuspendExecution(fixture.source, episode.EpisodeID, episode.Revision)
	if err != nil {
		t.Fatal(err)
	}
	child, err := fixture.store.MaterializeExecution(MaterializeExecutionInput{ExpectedGeneration: suspended.Head.ControllerGeneration, EpisodeID: episode.EpisodeID, EpisodeRevision: episode.Revision})
	if err != nil {
		t.Fatal(err)
	}
	fixture.source = *child.Admission
	source := publicationTestEdit(t, fixture, "child.txt", "child result\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	primary := fixture.store.identity.PrimaryRoot
	before, err := os.ReadFile(filepath.Join(primary, "root-only.txt"))
	if err != nil {
		t.Fatal(err)
	}
	root, release, err := PrepareFinalValidationWorkspace(fixture.store.config, "go-test")
	if err != nil || release == nil || root == primary {
		t.Fatalf("child validation checkout unavailable: %s %v", root, err)
	}
	t.Cleanup(func() { _ = release() })
	if _, err := os.Stat(filepath.Join(root, candidate.TaskRef.TaskPath)); !os.IsNotExist(err) {
		t.Fatal("validation source still contains the retired child task")
	}
	seedFinalValidationRunAt(t, fixture.store, root, "go-test", qualitygate.StatusPass, nil)
	if err := release(); err != nil {
		t.Fatal(err)
	}
	evidence, err := fixture.store.collectFinalValidationEvidence(candidate)
	if err != nil {
		t.Fatal(err)
	}
	if err := fixture.store.verifyFinalValidationEvidence(candidate, evidence); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(primary, "root-only.txt"))
	if err != nil || string(before) != string(after) {
		t.Fatal("child validation replaced unfinished parent source")
	}
	if _, err := fixture.store.PublishAcceptedCandidate(PublicationInput{ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: candidate.CandidateID}); err != nil {
		t.Fatal(err)
	}
}

func TestPublicationRefusesFailedCurrentFinalValidationRun(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "failing implementation\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusFail, nil)
	runtime, err := publishFinalValidationCompletion(t, fixture, promoted, candidate)
	if err == nil || !strings.Contains(err.Error(), "is not a terminal PASS") {
		t.Fatalf("publication must refuse a failed final validation run: %v", err)
	}
	if runtime.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("failed final validation completed the Runtime task")
	}
}

func TestPublicationRefusesFinalValidationRunWithoutLogEvidence(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "logless implementation\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, func(record *qualitygate.RunRecord) {
		record.Log = filepath.Join(t.TempDir(), "missing-gate.log")
	})
	runtime, err := publishFinalValidationCompletion(t, fixture, promoted, candidate)
	if err == nil || !strings.Contains(err.Error(), "is not a terminal PASS") {
		t.Fatalf("publication must refuse final validation without log evidence: %v", err)
	}
	if runtime.TaskStatus() == state.TaskStatusComplete {
		t.Fatal("logless final validation completed the Runtime task")
	}
}

func TestPublicationRefusesStaleFinalValidationRun(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "stale implementation\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, func(record *qualitygate.RunRecord) {
		record.Head = "0000000000000000000000000000000000000001"
		record.IndexDigest = "stale-index-digest"
	})
	runtime, err := publishFinalValidationCompletion(t, fixture, promoted, candidate)
	assertFinalValidationRefused(t, fixture, policy, runtime, err, "go-test")
}

func TestPublicationRefusesDirtyWorktreeFinalValidationRun(t *testing.T) {
	t.Parallel()
	for _, parent := range []bool{false, true} {
		t.Run(fmt.Sprint(parent), func(t *testing.T) {
			fixture, policy := newPublicationTestFixture(t)
			source := publicationTestEdit(t, fixture, "validated.txt", "dirty executed source\n")
			acceptedCompletionRuntime(t, fixture.store, source)
			seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
			promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
			seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, func(record *qualitygate.RunRecord) {
				if parent {
					record.WorktreeDigest = "dirty-parent-metadata"
				} else {
					record.WorktreeDigestExcludingParent = "dirty-source"
				}
			})
			runtime, err := publishFinalValidationCompletion(t, fixture, promoted, candidate)
			assertFinalValidationRefused(t, fixture, policy, runtime, err, "go-test")
		})
	}
}

func TestPublicationRefusesWrongModuleFinalValidationRun(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "validated.txt", "wrong module run\n")
	acceptedCompletionRuntime(t, fixture.store, source)
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	promoted, candidate := promoteFinalValidationSource(t, fixture, policy, source, nil)
	seedFinalValidationRun(t, fixture.store, "go-test", qualitygate.StatusPass, func(record *qualitygate.RunRecord) {
		record.WorkingDir = t.TempDir()
	})
	runtime, err := publishFinalValidationCompletion(t, fixture, promoted, candidate)
	assertFinalValidationRefused(t, fixture, policy, runtime, err, "go-test")
}

func TestCandidateAcceptanceInheritsDeclaredValidationObligation(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "forms.txt", "accepted forms\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "inherit only", Policy: policy,
		Evidence: publicationTestEvidence(t, fixture.store, source, policy),
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidate.RequiredValidations, []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}}) {
		t.Fatalf("declared obligation was not inherited without caller input: %#v", candidate.RequiredValidations)
	}
}

func TestCandidateAcceptanceExtendsDeclaredValidationObligation(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "extended.txt", "extended forms\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "add form", Policy: policy,
		Evidence:                publicationTestEvidence(t, fixture.store, source, policy),
		RequiredValidationForms: []string{"go-test-race", "go-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(candidate.RequiredValidations, []FinalValidationRequest{{Form: "go-test", WorkingDir: "."}, {Form: "go-test-race", WorkingDir: "."}}) {
		t.Fatalf("caller additions must extend, not replace, the declared obligation: %#v", candidate.RequiredValidations)
	}
}

func TestCandidateAcceptanceFailsClosedWithoutObligationRecord(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "missing-record.txt", "no obligation record\n")
	cfg := WorkflowTaskConfig(fixture.store.config, fixture.store.identity, source.Attempt.SemanticTaskRef)
	runtime := state.AttachStateStore(cfg)
	if err := runtime.Remove(state.FinalValidationObligationKey); err != nil {
		t.Fatal(err)
	}
	if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "missing record", Policy: policy,
		Evidence: publicationTestEvidence(t, fixture.store, source, policy),
	}); err == nil || !strings.Contains(err.Error(), "final validation obligation record is missing") {
		t.Fatalf("missing obligation record must fail closed at acceptance: %v", err)
	}
}

func TestCandidateAcceptanceRejectsUndeclaredValidationForms(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "undeclared.txt", "no declared obligation\n")
	if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "invented obligation", Policy: policy,
		Evidence:                publicationTestEvidence(t, fixture.store, source, policy),
		RequiredValidationForms: []string{"go-test"},
	}); err == nil || !strings.Contains(err.Error(), "no declared final validation obligation to extend") {
		t.Fatalf("validation forms without a worker declaration must be rejected: %v", err)
	}
	accepted, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "unobligated", Policy: policy,
		Evidence: publicationTestEvidence(t, fixture.store, source, policy),
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := fixture.store.LoadAcceptedCandidate(*accepted.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	if len(candidate.RequiredValidations) != 0 {
		t.Fatalf("undeclared unextended candidate must stay unobligated: %#v", candidate.RequiredValidations)
	}
}

func TestCandidateAcceptanceRejectsUnknownValidationForm(t *testing.T) {
	t.Parallel()
	fixture, policy := newPublicationTestFixture(t)
	source := publicationTestEdit(t, fixture, "unknown.txt", "unknown form\n")
	seedDeclaredFinalValidation(t, fixture.store, source, "go-test")
	if _, err := fixture.store.AcceptExecutionCandidate(source, CandidateAcceptanceInput{
		Message: "unknown form", Policy: policy,
		Evidence:                publicationTestEvidence(t, fixture.store, source, policy),
		RequiredValidationForms: []string{"lint"},
	}); err == nil || !strings.Contains(err.Error(), "not a known quality gate form") {
		t.Fatalf("unknown validation form accepted: %v", err)
	}
}
