package controller

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type evidenceExportFixture struct {
	store                              *Store
	cfg                                config.AppConfig
	repo                               string
	runtime                            *state.StateStore
	runtimeTask                        string
	seal                               AttemptSeal
	sealRef                            EvidenceObjectRef
	olderSealRef                       EvidenceObjectRef
	olderSeal                          AttemptSeal
	sealedModelWindows                 []runtimeTranscriptWindowRecord
	skipSealedEvidence                 bool
	skipLiveAttempt                    bool
	skipLiveHead                       bool
	skipRuntimeEvidence                bool
	withSealedRuntime                  bool
	withSealedResources                bool
	withOlderSeal                      bool
	sealedAssociationRuntimeTask       string
	sealedAssociationUnreadable        bool
	sealedWorkspaceFromRepo            bool
	olderAssociationRuntimeTask        string
	olderAssociationWithoutRuntimeTask bool
	candidateEvidenceRounds            int
	acceptedCandidateRevisions         int
	sharedLineageRevisions             int
}

const (
	evidenceExportLiveAttemptID     = "attempt-live-1"
	evidenceExportSealedAttemptID   = "attempt-b1"
	evidenceExportSealedRuntimeTask = "runtime-task-b1"
	evidenceExportLiveLeaseID       = "lease-live-1"
	evidenceExportGateRunID         = "0123456789abcdef0123456789abcdef"
	evidenceExportSmokeRunID        = "fedcba9876543210fedcba9876543210"

	evidenceExportParentThreadID   = "1b2c3d4e-5f60-4000-8000-0000000000aa"
	evidenceExportParentSessionID  = "1b2c3d4e-5f60-4000-8000-0000000000bb"
	evidenceExportForeignThreadID  = "1b2c3d4e-5f60-4000-8000-0000000000cc"
	evidenceExportEarlyGuardianID  = "guardian-before-attempt"
	evidenceExportActiveGuardianID = "guardian-spanning-attempt"
)

func newEvidenceExportFixture(t *testing.T, options ...func(*evidenceExportFixture)) *evidenceExportFixture {
	t.Helper()
	repo := newControllerRepo(t)
	stateBase := filepath.Join(t.TempDir(), "state", "sessions")
	cfg := controllerTestConfig(repo, stateBase)
	cfg.ClaudeConfigDir = t.TempDir()
	cfg.CodexConfigDir = t.TempDir()
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fixture := &evidenceExportFixture{store: store, cfg: cfg, repo: repo}
	for _, option := range options {
		option(fixture)
	}
	if !fixture.skipSealedEvidence {
		fixture.publishSealedEvidence(t)
	} else {
		fixture.seal = AttemptSeal{
			SemanticTaskRef: SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/B.md", ContractDigest: "live-contract"},
			RootTaskRef:     SemanticTaskRef{TaskPath: "IMPLEMENTATION_TASKS/ROOT.md", ContractDigest: "root-contract"},
			EpisodeID:       "episode-1",
		}
	}
	if !fixture.skipLiveAttempt {
		fixture.startLiveAttempt(t)
	}
	if !fixture.skipRuntimeEvidence {
		fixture.writeRuntimeEvidence(t)
	}
	return fixture
}

func (f *evidenceExportFixture) publishSealedEvidence(t *testing.T) {
	t.Helper()
	source := newControllerRepo(t)
	sealRef, seal := storeBundleAttemptSeal(t, f.store, source, evidenceExportSealedAttemptID, "IMPLEMENTATION_TASKS/B.md", "episode-1", 1)
	if f.withSealedRuntime {
		sealRef, seal = f.attachSealedRuntimeEvidence(t, seal)
	}
	if f.candidateEvidenceRounds > 0 {
		sealRef, seal = f.attachCandidateEvidence(t, seal)
	}
	seals := []EvidenceObjectRef{sealRef}
	if f.withOlderSeal {
		f.olderSealRef, f.olderSeal = storeBundleAttemptSealForTask(t, f.store, source, evidenceExportOlderAttemptID, seal.SemanticTaskRef, "episode-1", 3)
		if f.olderAssociationRuntimeTask != "" || f.olderAssociationWithoutRuntimeTask {
			f.attachOlderSealAssociation(t)
		}
		seals = append(seals, f.olderSealRef)
	}
	taskRef := storeBundleTaskRevision(t, f.store, seal.SemanticTaskRef, 1, nil, seals, nil)
	if f.acceptedCandidateRevisions > 0 {
		taskRef = f.storeLineageRevision(t, seal, seals, taskRef)
	}
	if f.sharedLineageRevisions > 0 {
		taskRef = f.storeSharedLineageChain(t, seal.SemanticTaskRef, taskRef)
	}
	episodeRef := storeBundleEpisodeRevision(t, f.store, seal, taskRef, 1, 1, nil, seals, nil)
	publishBundleAuthority(t, f.store, 1, "snapshot-1", nil, nil,
		[]EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}},
		[]EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}},
	)
	f.seal = seal
	f.sealRef = sealRef
	f.writeSealedAttemptRecord(t, seal)
	if f.withOlderSeal {
		f.writeOlderAttemptRecord(t)
	}
}

func (f *evidenceExportFixture) attachSealedRuntimeEvidence(t *testing.T, seal AttemptSeal) (EvidenceObjectRef, AttemptSeal) {
	t.Helper()
	attemptID := seal.AttemptID
	sealedTelemetry, telemetryErr := json.Marshal(state.ModelCallLog{
		Version: state.ModelCallLogVersion, CallID: "c1", CallType: state.CallTypeTask,
		TaskID: evidenceExportSealedRuntimeTask, SessionID: "session-sealed-worker",
		StartedAt: seal.StartedAt, CompletedAt: seal.StartedAt.Add(time.Minute),
		Phase: "completed", Role: state.WorkerRole, Outcome: "success",
	})
	if telemetryErr != nil {
		t.Fatal(telemetryErr)
	}
	sealedTelemetry = append(sealedTelemetry, '\n')
	sealedFiles := []struct {
		kind    string
		logical string
		data    []byte
	}{
		{"runtime-state", attemptID + ":state/worker.id", []byte("session-sealed-worker\n")},
		{"runtime-state", attemptID + ":state/parent-evidence.jsonl", evidenceTestParentEvidenceLedger(t, seal.StartedAt)},
		{"telemetry", attemptID + ":telemetry.jsonl", sealedTelemetry},
		{"task-events", attemptID + ":events.jsonl", []byte(`{"version":1,"task_id":"` + evidenceExportSealedRuntimeTask + `","call_id":"c1","session_id":"session-sealed-worker","seq":1,"timestamp":"2026-10-06T00:00:30Z","kind":"validation","validation":{"form":"go-test","gate_class":"test","result":"pass","attempt":"initial","validation_run_id":"0123456789abcdef0123456789abcdef"}}` + "\n" + `{"version":1,"task_id":"` + evidenceExportSealedRuntimeTask + `","call_id":"c2","session_id":"session-sealed-worker","seq":2,"timestamp":"2026-10-06T00:02:00Z","kind":"validation","validation":{"form":"go-test","gate_class":"test","result":"fail","attempt":"retry","validation_run_id":"0123456789abcdef0123456789abcdee"}}` + "\n")},
		{"review-rounds", attemptID + ":rounds.jsonl", []byte(`{"version":1,"task_id":"` + evidenceExportSealedRuntimeTask + `","seq":1,"review_number":1,"auto_fixes":0,"worker_phase":"completed","captured_at":"2026-10-06T00:03:00Z"}` + "\n")},
		{"model-transcript", attemptID + ":session/session-sealed-worker/0", []byte("sealed worker transcript\n")},
		{"parent-transcript", attemptID + ":parent/parent-thread-1/0", []byte("sealed parent rollout window\n")},
		{"guardian-transcript", attemptID + ":guardian/guardian-rollout-1", []byte("sealed guardian window\n")},
	}
	for _, file := range sealedFiles {
		ref, err := f.store.PutEvidenceObject(file.kind, "application/octet-stream", file.logical, true, file.data)
		if err != nil {
			t.Fatal(err)
		}
		seal.EvidenceRefs = append(seal.EvidenceRefs, ref)
	}
	association := runtimeSessionAssociation{
		AttemptID: attemptID, RuntimeTaskID: evidenceExportSealedRuntimeTask,
		Task: seal.SemanticTaskRef, SessionIDs: []string{"session-sealed-worker"},
		Parent:      &state.ParentCodexIdentity{Version: 1, ThreadID: "parent-thread-1", SessionID: "parent-session-1"},
		WindowStart: seal.StartedAt, WindowEnd: seal.SealedAt,
		ModelTranscripts: f.sealedModelWindows,
	}
	if f.sealedAssociationRuntimeTask != "" {
		association.RuntimeTaskID = f.sealedAssociationRuntimeTask
	}
	if f.withSealedResources {
		document := sealedTaskResourceDocument(seal, association.RuntimeTaskID)
		data, err := qualitygate.EncodeResourceSamples(document)
		if err != nil {
			t.Fatal(err)
		}
		logical := attemptID + ":artifacts/task-resources/" + document.Execution.CallID + "/" + qualitygate.ResourceSamplesFile
		ref, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", logical, true, data)
		if err != nil {
			t.Fatal(err)
		}
		seal.EvidenceRefs = append(seal.EvidenceRefs, ref)
	}
	associationData, err := json.Marshal(association)
	if err != nil {
		t.Fatal(err)
	}
	if f.sealedAssociationUnreadable {
		associationData = []byte(`{"attempt_id":"` + attemptID)
	}
	if f.sealedWorkspaceFromRepo {
		workspace, workspaceErr := ResolveWorkspaceIdentity(f.repo, f.store.Identity())
		if workspaceErr != nil {
			t.Fatal(workspaceErr)
		}
		seal.WorkspaceID = workspace.ID
	}
	associationRef, err := f.store.PutEvidenceObject("session-association", "application/json", attemptID+":runtime-sessions", true, associationData)
	if err != nil {
		t.Fatal(err)
	}
	seal.SessionAssociationRefs = append(seal.SessionAssociationRefs, associationRef)
	ref, stored, err := f.store.StoreAttemptSeal(seal)
	if err != nil {
		t.Fatal(err)
	}
	return ref, stored
}

func (f *evidenceExportFixture) attachCandidateEvidence(t *testing.T, seal AttemptSeal) (EvidenceObjectRef, AttemptSeal) {
	t.Helper()
	attach := func(role, artifactKind, artifactLogical string, artifactBytes []byte) {
		artifactRef, err := f.store.PutEvidenceObject(artifactKind, "application/octet-stream", artifactLogical, true, artifactBytes)
		if err != nil {
			t.Fatal(err)
		}
		record, err := json.Marshal(struct {
			Kind     string            `json:"kind"`
			Artifact EvidenceObjectRef `json:"artifact"`
		}{Kind: role, Artifact: artifactRef})
		if err != nil {
			t.Fatal(err)
		}
		ref, err := f.store.PutEvidenceObject(evidenceKindCandidateEvidence, "application/json", seal.AttemptID+":"+role, true, append(record, '\n'))
		if err != nil {
			t.Fatal(err)
		}
		seal.EvidenceRefs = append(seal.EvidenceRefs, ref)
	}
	for round := 0; round < f.candidateEvidenceRounds; round++ {
		attach(candidateEvidenceKindValidation, "validation",
			fmt.Sprintf("%s:validation-artifact-%d", seal.AttemptID, round),
			[]byte(fmt.Sprintf("validation run %d log\n", round)))
	}
	attach(candidateEvidenceKindReview, "review", seal.AttemptID+":review-artifact", []byte("review proof\n"))
	attach(candidateEvidenceKindInstall, "install", seal.AttemptID+":install-artifact", []byte("install proof\n"))
	ref, stored, err := f.store.StoreAttemptSeal(seal)
	if err != nil {
		t.Fatal(err)
	}
	return ref, stored
}

func (f *evidenceExportFixture) storeLineageRevision(t *testing.T, seal AttemptSeal, seals []EvidenceObjectRef, previous EvidenceObjectRef) EvidenceObjectRef {
	t.Helper()
	var lineage []EvidenceObjectRef
	for revision := 0; revision < f.acceptedCandidateRevisions; revision++ {
		record, err := json.Marshal(struct {
			AttemptID string `json:"attempt_id"`
			Revision  int    `json:"revision"`
		}{AttemptID: seal.AttemptID, Revision: revision})
		if err != nil {
			t.Fatal(err)
		}
		ref, err := f.store.PutEvidenceObject("accepted-candidate", "application/json",
			fmt.Sprintf("%s:accepted-candidate-%d", seal.AttemptID, revision), true, append(record, '\n'))
		if err != nil {
			t.Fatal(err)
		}
		lineage = append(lineage, ref)
	}
	revision := testTaskIndexRevision(seal.SemanticTaskRef, 2, bundleTransitionID(2), time.Unix(5350, 0).UTC())
	revision.AttemptSeals = seals
	revision.PublicationLineageRecords = lineage
	revision.PreviousRevision = &previous
	ref, _, err := f.store.StoreTaskIndexRevision(revision)
	if err != nil {
		t.Fatal(err)
	}
	return ref
}

func (f *evidenceExportFixture) storeSharedLineageChain(t *testing.T, task SemanticTaskRef, previous EvidenceObjectRef) EvidenceObjectRef {
	t.Helper()
	record, err := json.Marshal(struct {
		AttemptID string `json:"attempt_id"`
	}{AttemptID: evidenceExportSealedAttemptID})
	if err != nil {
		t.Fatal(err)
	}
	sharedRef, err := f.store.PutEvidenceObject("accepted-candidate", "application/json",
		evidenceExportSealedAttemptID+":accepted-shared", true, append(record, '\n'))
	if err != nil {
		t.Fatal(err)
	}
	ref := previous
	for revision := 1; revision <= f.sharedLineageRevisions; revision++ {
		generation := uint64(2 + revision)
		next := testTaskIndexRevision(task, generation, bundleTransitionID(generation), time.Unix(5350+int64(revision), 0).UTC())
		next.PublicationLineageRecords = []EvidenceObjectRef{sharedRef}
		next.PreviousRevision = &ref
		stored, _, err := f.store.StoreTaskIndexRevision(next)
		if err != nil {
			t.Fatal(err)
		}
		ref = stored
	}
	return ref
}

func (f *evidenceExportFixture) writeSealedAttemptRecord(t *testing.T, seal AttemptSeal) {
	t.Helper()
	attempt := AttemptRecord{
		SchemaVersion: controllerSchemaVersion, AttemptID: seal.AttemptID,
		SemanticTaskRef: seal.SemanticTaskRef, RootTaskRef: seal.RootTaskRef,
		EpisodeID: seal.EpisodeID, EpisodeRevision: seal.EpisodeRevision,
		ExecutionBaseOID:   seal.ExecutionBaseOID,
		BaselineSnapshotID: "baseline-sealed", BaselineArchive: seal.GitObjectArchive,
		StartControllerGeneration: seal.ControllerGeneration,
		AttemptState:              AttemptStateSuspendedForBlocker,
		AttemptSealID:             seal.AttemptSealID,
		CreatedAt:                 seal.StartedAt,
	}
	if f.withOlderSeal {
		attempt.PredecessorAttemptID = evidenceExportOlderAttemptID
		attempt.ResumedFromSealID = f.olderSealRef.LogicalIdentity
	}
	if err := f.store.writeAttempt(attempt); err != nil {
		t.Fatal(err)
	}
}

func (f *evidenceExportFixture) writeOlderAttemptRecord(t *testing.T) {
	t.Helper()
	attempt := AttemptRecord{
		SchemaVersion: controllerSchemaVersion, AttemptID: f.olderSeal.AttemptID,
		SemanticTaskRef: f.olderSeal.SemanticTaskRef, RootTaskRef: f.olderSeal.RootTaskRef,
		EpisodeID: f.olderSeal.EpisodeID, EpisodeRevision: f.olderSeal.EpisodeRevision,
		ExecutionBaseOID:   f.olderSeal.ExecutionBaseOID,
		BaselineSnapshotID: "baseline-older", BaselineArchive: f.olderSeal.GitObjectArchive,
		StartControllerGeneration: f.olderSeal.ControllerGeneration,
		AttemptState:              AttemptStateSuspendedForBlocker,
		AttemptSealID:             f.olderSeal.AttemptSealID,
		CreatedAt:                 f.olderSeal.StartedAt.Add(-time.Hour),
	}
	if err := f.store.writeAttempt(attempt); err != nil {
		t.Fatal(err)
	}
}

func (f *evidenceExportFixture) attachOlderSealAssociation(t *testing.T) {
	t.Helper()
	runtimeTask := f.olderAssociationRuntimeTask
	if f.olderAssociationWithoutRuntimeTask {
		runtimeTask = ""
	}
	association := runtimeSessionAssociation{
		AttemptID: evidenceExportOlderAttemptID, RuntimeTaskID: runtimeTask,
		Task: f.olderSeal.SemanticTaskRef, SessionIDs: []string{"session-older-worker"},
		WindowStart: f.olderSeal.StartedAt, WindowEnd: f.olderSeal.SealedAt,
	}
	data, err := json.Marshal(association)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := f.store.PutEvidenceObject("session-association", "application/json", evidenceExportOlderAttemptID+":runtime-sessions", true, data)
	if err != nil {
		t.Fatal(err)
	}
	f.olderSeal.SessionAssociationRefs = append(f.olderSeal.SessionAssociationRefs, ref)
	storedRef, stored, err := f.store.StoreAttemptSeal(f.olderSeal)
	if err != nil {
		t.Fatal(err)
	}
	f.olderSealRef, f.olderSeal = storedRef, stored
}

func (f *evidenceExportFixture) startLiveAttempt(t *testing.T) {
	t.Helper()
	workspace, err := ResolveWorkspaceIdentity(f.repo, f.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	next := head
	next.ControllerGeneration = head.ControllerGeneration + 1
	task := f.seal.SemanticTaskRef
	root := f.seal.RootTaskRef
	next.ExecutionTaskRef = &task
	next.RootTaskRef = &root
	next.ActiveEpisodeID = f.seal.EpisodeID
	next.ActiveEpisodeRevision = 1
	next.LiveAttemptID = evidenceExportLiveAttemptID
	next.LiveLeaseID = evidenceExportLiveLeaseID
	attempt := AttemptRecord{
		SchemaVersion: controllerSchemaVersion, AttemptID: evidenceExportLiveAttemptID,
		SemanticTaskRef: f.seal.SemanticTaskRef, RootTaskRef: f.seal.RootTaskRef,
		EpisodeID: f.seal.EpisodeID, EpisodeRevision: 1,
		ExecutionBaseOID:   controllerGitOutput(t, f.repo, "rev-parse", "HEAD"),
		BaselineSnapshotID: "baseline-live", WorkspaceSnapshotID: workspace.ID,
		StartControllerGeneration: next.ControllerGeneration, AttemptState: AttemptStateLive,
		CreatedAt: time.Unix(6000, 0).UTC(),
	}
	lease := ExecutionLease{
		SchemaVersion: controllerSchemaVersion, LeaseID: evidenceExportLiveLeaseID,
		AttemptID: evidenceExportLiveAttemptID, SemanticTaskRef: f.seal.SemanticTaskRef,
		Purpose: "blocker-execution", ControllerGeneration: next.ControllerGeneration,
		EpisodeID: f.seal.EpisodeID, EpisodeRevision: 1, WorkspaceID: workspace.ID,
		ExpectedBaseOID: attempt.ExecutionBaseOID, ExpectedWorkspaceSnapshotID: workspace.ID,
		CreatedAt: attempt.CreatedAt,
	}
	if err := f.store.writeAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	if err := f.store.writeLease(lease); err != nil {
		t.Fatal(err)
	}
	if f.skipLiveHead {
		return
	}
	if err := f.store.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		t.Fatal(err)
	}
}

func (f *evidenceExportFixture) runtimeRepoHash() string {
	return digestStrings(f.store.Identity().LineageID, f.seal.SemanticTaskRef.TaskPath)
}

func (f *evidenceExportFixture) bindSealedAttemptRuntime(t *testing.T, transcript string) {
	t.Helper()
	runtimeCfg := f.cfg
	runtimeCfg.RepoHash = f.runtimeRepoHash()
	runtime, err := state.NewStateStore(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("task.id", evidenceExportSealedRuntimeTask); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          evidenceExportSealedAttemptID,
		TaskPath:           f.seal.SemanticTaskRef.TaskPath,
		TaskContractDigest: f.seal.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("worker.id", "session-sealed-worker"); err != nil {
		t.Fatal(err)
	}
	projects := filepath.Join(f.cfg.ClaudeConfigDir, "projects", "fixture")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, "session-sealed-worker.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *evidenceExportFixture) writeRuntimeEvidence(t *testing.T) {
	t.Helper()
	runtimeCfg := f.cfg
	runtimeCfg.RepoHash = f.runtimeRepoHash()
	runtime, err := state.NewStateStore(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	runtimeTask := "runtime-task-uuid-1"
	if err := runtime.Write("task.id", runtimeTask); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          evidenceExportLiveAttemptID,
		TaskPath:           f.seal.SemanticTaskRef.TaskPath,
		TaskContractDigest: f.seal.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("worker.id", "session-worker"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("reviewer.id", "session-reviewer"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("failure-path-reviewer.id", "../escape"); err != nil {
		t.Fatal(err)
	}
	telemetry := `{"version":3,"call_id":"c1","call_type":"model","task_id":"` + runtimeTask + `","session_id":"session-worker","started_at":"2026-10-06T00:00:00Z","completed_at":"2026-10-06T00:01:00Z","phase":"completed","role":"worker","model_alias":"opus","effort":"high","outcome":"success","prompt_bytes":1,"prompt_sha256":"aa","system_prompt_bytes":0}` + "\n" + `{"version":3,"call_id":"c2","partial`
	if err := os.MkdirAll(filepath.Dir(runtime.ModelCallLogPath(runtimeTask)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtime.ModelCallLogPath(runtimeTask), []byte(telemetry), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(runtime.TaskLiveStatusPath(runtimeTask)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtime.TaskLiveStatusPath(runtimeTask), []byte(`{"status":"active"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	events := `{"version":1,"task_id":"` + runtimeTask + `","call_id":"c1","session_id":"session-worker","role":"worker","phase":"completed","seq":1,"timestamp":"2026-10-06T00:01:00Z","kind":"call"}` + "\n" +
		`{"version":1,"task_id":"` + runtimeTask + `","call_id":"c9","session_id":"session-worker","role":"worker","phase":"completed","seq":2,"timestamp":"2026-10-06T00:02:00Z","kind":"validation","validation":{"source":"quality-gate","form":"go-test","validation_run_id":"` + evidenceExportGateRunID + `","gate_class":"test","suite":"go-test","phase":"quality-gate","attempt":"initial","result":"pass","exit_code":0,"duration_ms":1000,"evidence":"quality-gate-runs/` + evidenceExportGateRunID + `/gate.log"}}` + "\n" +
		`{"version":1,"task_id":"` + runtimeTask + `","call_id":"c10","session_id":"session-worker","role":"worker","phase":"completed","seq":3,"timestamp":"2026-10-06T00:03:00Z","kind":"user","blocks":[{"type":"tool","name":"Bash","operation_category":"test","validation":[{"form":"go-test","gate_class":"test","attempt":"retry","result":"unknown","snapshot_id":"snapshot-block-1"}],"bytes":12}]}` + "\n" +
		`{"version":1,"task_id":"` + runtimeTask + `","call_id":"c11","session_id":"session-worker","role":"worker","phase":"completed","seq":4,"timestamp":"2026-10-06T00:04:00Z","kind":"user","blocks":[{"type":"tool","name":"Bash","operation_category":"test","validation":[{"form":"go-test","gate_class":"test","attempt":"retry","result":"unknown","snapshot_id":"snapshot-block-2"}],"bytes":12},{"type":"tool","name":"Bash","operation_category":"test","validation":[{"form":"go-test","gate_class":"test","attempt":"retry","result":"unknown","snapshot_id":"snapshot-block-2"}],"bytes":12}]}` + "\n"
	if err := os.WriteFile(runtime.TaskEventLogPath(runtimeTask), []byte(events), 0o600); err != nil {
		t.Fatal(err)
	}
	f.writeRuntimeValidationRuns(t, runtime)
	if err := os.MkdirAll(runtime.ArtifactDir(runtimeTask), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime.ArtifactDir(runtimeTask), "report.txt"), []byte("artifact bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(runtime.TaskAuthorityContentPath(runtimeTask)), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtime.TaskAuthorityContentPath(runtimeTask), []byte("# Task: fixture task authority\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(runtime.TaskAuthorityPathPath(runtimeTask), []byte(f.seal.SemanticTaskRef.TaskPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repo, evidenceExportInstructionPlanPath), []byte("# Plan\n\n## ACTIVE\n\n- `IMPLEMENTATION_TASKS/B.md`\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repo, evidenceExportInstructionRulesPath), []byte("# Rules\n\nfixture instruction rules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repo, evidenceExportInstructionHistoryPath), []byte("# History\n\nfixture implementation history\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	projects := filepath.Join(f.cfg.ClaudeConfigDir, "projects", "fixture")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	transcript := `{"timestamp":"1970-01-01T00:00:00Z","line":"before-window"}` + "\n" + `{"timestamp":"1970-01-01T02:00:01Z","line":"in-window"}` + "\n"
	if err := os.WriteFile(filepath.Join(projects, "session-worker.jsonl"), []byte(transcript), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repo, "untracked-note.txt"), []byte("untracked bytes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(f.repo, "source.txt"), []byte("base-staged\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	runControllerGit(t, f.repo, "add", "source.txt")
	f.runtime = runtime
	f.runtimeTask = runtimeTask
}

func (f *evidenceExportFixture) writeRuntimeValidationRuns(t *testing.T, runtime *state.StateStore) {
	t.Helper()
	started := time.Unix(7000, 0).UTC()
	completed := time.Unix(7100, 0).UTC()
	record := qualitygate.RunRecord{
		ValidationRunID: evidenceExportGateRunID, Form: "go-test",
		Repository: f.repo, WorkingDir: filepath.Join(f.repo, "glm-worker"),
		Head: "head-oid", IndexDigest: "index-digest", WorktreeDigest: "worktree-digest",
		StartedAt: started, CompletedAt: &completed, Status: qualitygate.StatusPass,
		ExitCode: 0, ExitSource: state.ValidationExitSourceTarget, DurationMS: 1000,
	}
	writeEvidenceGateRun(t, runtime, record)
	smokeRecord := qualitygate.RunRecord{
		ValidationRunID: evidenceExportSmokeRunID, Form: "install-smoke",
		Repository: f.repo, WorkingDir: f.repo,
		StartedAt: started.Add(time.Minute), CompletedAt: &completed, Status: qualitygate.StatusPass,
		ExitCode: 0, ExitSource: state.ValidationExitSourceTarget, DurationMS: 2000,
	}
	smokeData, err := json.Marshal(smokeRecord)
	if err != nil {
		t.Fatal(err)
	}
	smokeDir := runtime.Path(filepath.Join(evidenceExportValidationSmokeDirectory, evidenceExportSmokeRunID))
	if err := os.MkdirAll(smokeDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(smokeDir, qualitygate.RunFile), smokeData, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(smokeDir, evidenceExportValidationSmokeLog), []byte("install smoke tail\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func evidenceTestParentEvidenceLedger(t *testing.T, base time.Time) []byte {
	t.Helper()
	record := func(surface, outcome, digest string, at time.Time) []byte {
		data, err := json.Marshal(state.ParentEvidenceRecord{
			Version: 1, Time: at, OwnerCallID: "owner-call-1", Surface: surface,
			Origin: state.ParentEvidenceOriginEvidence, Digest: digest, Bytes: 8,
			Outcome: outcome,
		})
		if err != nil {
			t.Fatal(err)
		}
		return append(data, '\n')
	}
	var ledger []byte
	ledger = append(ledger, record(state.ParentEvidenceSurfaceStatus, state.ParentEvidenceOutcomeProjected, "digest-status", base)...)
	ledger = append(ledger, record(state.ParentEvidenceSurfaceStatus, state.ParentEvidenceOutcomeProjected, "digest-status", base.Add(time.Second))...)
	ledger = append(ledger, record(state.ParentEvidenceSurfaceHandoff, state.ParentEvidenceOutcomeProjected, "digest-handoff", base.Add(2*time.Second))...)
	ledger = append(ledger, record(state.ParentEvidenceSurfaceStatus, state.ParentEvidenceOutcomeDuplicate, "digest-status", base.Add(3*time.Second))...)
	ledger = append(ledger, record(state.ParentEvidenceSurfaceStatus, state.ParentEvidenceOutcomeDuplicate, "digest-status", base.Add(4*time.Second))...)
	ledger = append(ledger, record(state.ParentEvidenceSurfaceStatus, state.ParentEvidenceOutcomeError, "digest-error", base.Add(5*time.Second))...)
	return ledger
}

func writeEvidenceGateRun(t *testing.T, store *state.StateStore, record qualitygate.RunRecord) {
	t.Helper()
	runDir := store.Path(filepath.Join(qualitygate.RunDirectory, record.ValidationRunID))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, qualitygate.RunFile), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, qualitygate.RunLog), []byte("gate log for "+record.ValidationRunID+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func (f *evidenceExportFixture) writeModuleValidationRun(t *testing.T, record qualitygate.RunRecord) {
	t.Helper()
	f.writeModuleValidationRunAt(t, f.repo, record)
}

func exportEntryByPath(t *testing.T, manifest EvidenceExportManifest, path string) EvidenceExportEntry {
	t.Helper()
	for _, entry := range manifest.Entries {
		if entry.Path == path {
			return entry
		}
	}
	t.Fatalf("export manifest has no entry %s", path)
	return EvidenceExportEntry{}
}

func exportHasEntry(manifest EvidenceExportManifest, path string) bool {
	for _, entry := range manifest.Entries {
		if entry.Path == path {
			return true
		}
	}
	return false
}

func exportFileByPath(t *testing.T, export EvidenceExport, path string) []byte {
	t.Helper()
	for _, file := range export.Files {
		if file.Path == path {
			return file.Data
		}
	}
	t.Fatalf("export archive has no file %s", path)
	return nil
}


func evidenceObjectTreeDigest(t *testing.T, store *Store) string {
	t.Helper()
	root := filepath.Join(store.dir, "evidence", "objects")
	var builder strings.Builder
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil || entry.IsDir() {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		builder.WriteString(rel + "\x00" + digestBytes(data) + "\x00")
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return builder.String()
}
