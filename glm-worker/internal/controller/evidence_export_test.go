package controller

import (
	"bytes"
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

func TestExportEvidenceLiveAttemptCollectsRawSections(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	objectsBefore := evidenceObjectTreeDigest(t, fixture.store)
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	var decoded EvidenceExportManifest
	if err := json.Unmarshal(exportFileByPath(t, export, "manifest.json"), &decoded); err != nil {
		t.Fatal(err)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	telemetry := exportEntryByPath(t, export.Manifest, "live/task/telemetry.jsonl")
	transcript := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/session-worker/0")
	missing := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/session-reviewer")
	unsafe := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/../escape")
	artifact := exportEntryByPath(t, export.Manifest, "live/task/artifacts/report.txt")
	staged := exportFileByPath(t, export, "live/git/diff-staged.patch")
	untracked := exportEntryByPath(t, export.Manifest, "live/git/untracked/untracked-note.txt")
	plan := exportEntryByPath(t, export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath)
	gate := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+evidenceExportGateRunID+"/run.json")
	smoke := exportEntryByPath(t, export.Manifest, "live/validation/install-smoke/"+evidenceExportSmokeRunID+"/smoke.log")
	smokeMeta := exportEntryByPath(t, export.Manifest, "live/validation/install-smoke/"+evidenceExportSmokeRunID+"/run.json")
	for _, check := range []struct {
		name   string
		valid  bool
		actual any
	}{
		{"manifest-round-trip/identity", decoded.Format == evidenceExportFormat && decoded.SchemaVersion == evidenceSchemaVersion, decoded},
		{"manifest-round-trip/analysis-index", decoded.AnalysisIndex.Path == "analysis-index.json" && decoded.AnalysisIndex.SHA256 == digestBytes(exportFileByPath(t, export, "analysis-index.json")), decoded.AnalysisIndex},
		{"live-result/coverage", result.RuntimeStatus == evidenceExportRuntimeCollected && result.Coverage == evidenceExportCoveragePartial, result},
		{"live-result/attempt-absent", result.AttemptSectionStatus == evidenceExportAttemptAbsent && export.Manifest.Attempt.AbsenceReason != "", []any{result.AttemptSectionStatus, export.Manifest.Attempt}},
		{"live-result/target", result.Target.Live && result.Target.AttemptID == evidenceExportLiveAttemptID && result.Target.Kind == evidenceExportTargetRunning, result.Target},
		{"live-result/selection", result.Target.RuntimeTaskID == fixture.runtimeTask && result.Target.SelectionBasis == evidenceExportBasisLiveAttempt, result.Target},
		{"manifest-digest", result.ManifestDigest == digestBytes(exportFileByPath(t, export, "manifest.json")), result.ManifestDigest},
		{"telemetry/window", telemetry.TrailingFragment && telemetry.Records == 1 && telemetry.InProgress, telemetry},
		{"telemetry/digest", telemetry.SHA256 == digestBytes(exportFileByPath(t, export, "live/task/telemetry.jsonl")), telemetry},
		{"transcript/digest", transcript.SHA256 == digestBytes([]byte(`{"timestamp":"1970-01-01T02:00:01Z","line":"in-window"}`+"\n")), transcript},
		{"transcript/window", transcript.Window != nil && transcript.Window.RecordsBefore == 1 && transcript.Window.Basis == evidenceWindowBasisTimestamp, transcript.Window},
		{"transcript/missing", missing.Missing, missing},
		{"transcript/unsafe", unsafe.Unreadable != "" && !unsafe.Missing, unsafe},
		{"artifact/digest", artifact.SHA256 == digestBytes([]byte("artifact bytes\n")), artifact},
		{"git/staged", bytes.Contains(staged, []byte("source.txt")), string(staged)},
		{"git/untracked", untracked.SHA256 == digestBytes([]byte("untracked bytes\n")), untracked},
		{"instructions/plan", plan.Basis == evidenceExportBasisWorkspaceObservation && plan.SHA256 != "", plan},
		{"instructions/rules", exportHasEntry(export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionRulesPath), export.Manifest.Entries},
		{"instructions/history", exportHasEntry(export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionHistoryPath), export.Manifest.Entries},
		{"git/audit", export.Manifest.Git != nil && export.Manifest.Git.ExecutionBaseOID != "" && export.Manifest.Git.Snapshot != nil, export.Manifest.Git},
		{"analysis/sessions", analysis.Sessions.Status == evidenceAnalysisStatusCollected && len(analysis.Sessions.Sessions) > 0, analysis.Sessions},
		{"analysis/window", !analysis.Window.Start.IsZero() && !analysis.Window.End.IsZero(), analysis.Window},
		{"analysis/instructions", analysis.Instructions.Status == evidenceAnalysisStatusCollected && analysis.Instructions.Task != nil && analysis.Instructions.Task.Entry == "live/task/authority.md" && analysis.Instructions.Plan != nil && analysis.Instructions.Plan.Entry == "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath && analysis.Instructions.Rules != nil && analysis.Instructions.Rules.SHA256 != "" && analysis.Instructions.History != nil && analysis.Instructions.History.SHA256 != "", analysis.Instructions},
		{"validation/gate-metadata", gate.Basis == evidenceExportBasisRuntimeValidation && !gate.Missing && gate.Unreadable == "", gate},
		{"validation/gate-log", string(exportFileByPath(t, export, "live/validation/quality-gate/"+evidenceExportGateRunID+"/gate.log")) == "gate log for "+evidenceExportGateRunID+"\n", gate},
		{"validation/smoke-log", !smoke.Missing && smoke.Unreadable == "", smoke},
		{"validation/smoke-metadata", smokeMeta.Basis == evidenceExportBasisRuntimeValidation && !smokeMeta.Missing, smokeMeta},
		{"validation/canonical-gate", analysisValidationRunPresent(analysis.CanonicalValidationRuns, evidenceExportGateRunID, evidenceExportBasisRuntimeValidation), analysis.CanonicalValidationRuns},
		{"validation/canonical-smoke", analysisValidationRunPresent(analysis.CanonicalValidationRuns, evidenceExportSmokeRunID, evidenceExportBasisRuntimeValidation), analysis.CanonicalValidationRuns},
	} {
		t.Run(check.name, func(t *testing.T) {
			if !check.valid {
				t.Fatalf("%s = %#v", check.name, check.actual)
			}
		})
	}
	for _, file := range export.Files {
		if strings.Contains(file.Path, "..") || strings.Contains(file.Path, `\`) || strings.HasPrefix(file.Path, "/") {
			t.Fatalf("export archive entry escapes its prefix: %s", file.Path)
		}
	}
	for _, entry := range export.Manifest.Entries {
		if entry.Path == "live/git/object-archive.json" {
			t.Fatal("live export embedded a git object archive payload")
		}
	}
	gateRows := 0
	for _, run := range analysis.CanonicalValidationRuns {
		if run.Basis == evidenceAnalysisBasisBlockObservation || run.Basis == evidenceAnalysisBasisEvents ||
			(run.RunID == evidenceExportGateRunID && run.Basis == evidenceExportBasisRuntimeValidation && run.Binding != evidenceValidationBindingTaskStore) ||
			(run.Log != "" && !exportHasEntry(export.Manifest, run.Log)) {
			t.Fatalf("canonical validation run has invalid classification, binding or archive log: %#v", run)
		}
		if run.RunID == evidenceExportGateRunID {
			gateRows++
		}
	}
	eventObserved, mergedDuplicates := false, 0
	for _, run := range analysis.ObservedValidationActions {
		if run.Basis == evidenceAnalysisBasisEvents {
			eventObserved = run.RunID == evidenceExportGateRunID && run.Log == "" && run.Result == "pass"
			continue
		}
		if run.Basis != evidenceAnalysisBasisBlockObservation || run.Attempt != state.ValidationAttemptRetry || run.Result != "unknown" {
			t.Fatalf("observed validation action = %#v", run)
		}
		if run.SnapshotID == "snapshot-block-2" {
			mergedDuplicates = run.Occurrences
		}
	}
	for _, check := range []struct {
		name   string
		valid  bool
		actual any
	}{
		{"validation/canonical-uniqueness", gateRows == 1, analysis.CanonicalValidationRuns},
		{"validation/event-relation", eventObserved, analysis.ObservedValidationActions},
		{"validation/duplicate-provenance", mergedDuplicates == 2 && len(analysis.ObservedValidationActions) == 3, analysis.ObservedValidationActions},
		{"validation/retries", len(analysis.Retries.ValidationReruns) > 0, analysis.Retries},
		{"validation/in-progress", len(analysis.EvidenceStates.InProgress) > 0, analysis.EvidenceStates},
		{"export/read-only", evidenceObjectTreeDigest(t, fixture.store) == objectsBefore, fixture.store.dir},
	} {
		t.Run(check.name, func(t *testing.T) {
			if !check.valid {
				t.Fatalf("%s = %#v", check.name, check.actual)
			}
		})
	}
	t.Run("task-id-live-binding", func(t *testing.T) {
		_, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: fixture.runtimeTask})
		if err != nil {
			t.Fatal(err)
		}
		if !result.Target.Live || result.Target.SelectionBasis != evidenceExportBasisLiveBinding {
			t.Fatalf("live task id target = %#v", result.Target)
		}
	})
}

func analysisValidationRunPresent(runs []EvidenceAnalysisValidationRun, runID, basis string) bool {
	for _, run := range runs {
		if run.RunID == runID && run.Basis == basis && run.Log != "" {
			return true
		}
	}
	return false
}

func TestExportEvidenceBindsRepositoryScopedValidationRuns(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	inWindow := time.Unix(6100, 0).UTC()
	inWindowEnd := time.Unix(6200, 0).UTC()
	beforeAttempt := time.Unix(100, 0).UTC()
	beforeAttemptEnd := time.Unix(200, 0).UTC()
	foreignRepo := t.TempDir()
	boundRun := qualitygate.RunRecord{
		ValidationRunID: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Form: "go-test",
		Repository: repoRoot, WorkingDir: filepath.Join(repoRoot, "glm-worker"),
		StartedAt: inWindow, CompletedAt: &inWindowEnd, Status: qualitygate.StatusPass,
	}
	foreignRun := boundRun
	foreignRun.ValidationRunID = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	foreignRun.Repository = foreignRepo
	foreignRun.WorkingDir = filepath.Join(foreignRepo, "glm-worker")
	staleRun := boundRun
	staleRun.ValidationRunID = "cccccccccccccccccccccccccccccccc"
	staleRun.StartedAt = beforeAttempt
	staleRun.CompletedAt = &beforeAttemptEnd
	otherTaskRun := boundRun
	otherTaskRun.ValidationRunID = "dddddddddddddddddddddddddddddddd"
	otherTaskRun.TaskID = "another-runtime-task"
	for _, run := range []qualitygate.RunRecord{boundRun, foreignRun, staleRun, otherTaskRun} {
		fixture.writeModuleValidationRun(t, run)
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entries := map[string]EvidenceExportEntry{}
	for _, entry := range export.Manifest.Entries {
		entries[entry.Path] = entry
	}
	boundEntry, ok := entries["live/validation/quality-gate/"+boundRun.ValidationRunID+"/run.json"]
	if !ok || boundEntry.Basis != evidenceExportBasisRepositoryWorkspace || boundEntry.Missing {
		t.Fatalf("repository-bound gate run entry = %#v ok=%v", boundEntry, ok)
	}
	if !exportHasEntry(export.Manifest, "live/validation/quality-gate/"+boundRun.ValidationRunID+"/gate.log") {
		t.Fatal("repository-bound gate log entry is missing")
	}
	for _, excluded := range []string{foreignRun.ValidationRunID, staleRun.ValidationRunID, otherTaskRun.ValidationRunID} {
		if exportHasEntry(export.Manifest, "live/validation/quality-gate/"+excluded+"/run.json") {
			t.Fatalf("export absorbed unbound gate run %s", excluded)
		}
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	if !analysisValidationRunPresent(analysis.CanonicalValidationRuns, boundRun.ValidationRunID, evidenceExportBasisRepositoryWorkspace) {
		t.Fatalf("analysis miss the repository-bound run: %#v", analysis.CanonicalValidationRuns)
	}
	for _, run := range analysis.CanonicalValidationRuns {
		if run.RunID == boundRun.ValidationRunID && run.Binding != evidenceValidationBindingWorkspaceWindow {
			t.Fatalf("repository-bound run binding = %#v", run)
		}
	}
}

func sealedRuntimeExportFixture(t *testing.T) (EvidenceExport, EvidenceExportResult) {
	t.Helper()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	return export, result
}

func TestExportEvidenceSealedAttemptProjectsCanonicalSections(t *testing.T) {
	t.Parallel()
	export, result := sealedRuntimeExportFixture(t)
	t.Run("runtime-evidence", func(t *testing.T) {
		for _, check := range []struct {
			name   string
			valid  bool
			actual any
		}{
			{"target", result.Target.Kind == evidenceExportTargetTaskID && result.Target.AttemptID == evidenceExportSealedAttemptID, result.Target},
			{"binding", result.Target.RuntimeTaskID == evidenceExportSealedRuntimeTask && result.Target.SelectionBasis == evidenceExportBasisSessionAssociation, result.Target},
			{"coverage", result.RuntimeStatus == evidenceExportRuntimeAbsent && result.Coverage == evidenceExportCoverageAttemptOnly, result},
			{"attempt-section", result.AttemptSectionStatus == evidenceExportAttemptPresent && export.Manifest.Attempt.RuntimeTaskID == evidenceExportSealedRuntimeTask && export.Manifest.Attempt.RuntimeEvidence, []any{result.AttemptSectionStatus, export.Manifest.Attempt}},
			{"worker-state", string(exportFileByPath(t, export, "attempt/state/worker.id")) == "session-sealed-worker\n", exportFileByPath(t, export, "attempt/state/worker.id")},
		} {
			t.Run(check.name, func(t *testing.T) {
				if !check.valid {
					t.Fatalf("%s = %#v", check.name, check.actual)
				}
			})
		}
		for _, path := range []string{"attempt/seal.json", "attempt/session-association.json", "attempt/telemetry.jsonl", "attempt/events.jsonl", "attempt/rounds.jsonl", "attempt/state/worker.id", "attempt/transcripts/model/session-sealed-worker/0", "attempt/transcripts/parent/parent-thread-1/0", "attempt/transcripts/guardian/guardian-rollout-1"} {
			entry := exportEntryByPath(t, export.Manifest, path)
			if entry.Missing || entry.Unreadable != "" || entry.CanonicalDigest == "" || entry.Basis == "" || entry.SHA256 != digestBytes(exportFileByPath(t, export, path)) {
				t.Fatalf("sealed entry %s lacks readable bytes, canonical association or matching digest: %#v", path, entry)
			}
		}
	})
	t.Run("parent-evidence-aggregate", func(t *testing.T) {
		ledger := exportEntryByPath(t, export.Manifest, "attempt/state/parent-evidence.aggregate.json")
		var aggregate evidenceParentEvidenceAggregate
		if err := json.Unmarshal(exportFileByPath(t, export, "attempt/state/parent-evidence.aggregate.json"), &aggregate); err != nil {
			t.Fatal(err)
		}
		if ledger.CanonicalDigest == "" || ledger.OmittedPayload == "" || aggregate.Records != 6 || aggregate.GroupCount != 4 || aggregate.RepetitionsOmitted != 2 || aggregate.SourceSHA256 != ledger.CanonicalDigest {
			t.Fatalf("sealed parent evidence aggregate: entry=%#v aggregate=%#v", ledger, aggregate)
		}
		for _, file := range export.Files {
			if file.Path == "attempt/state/parent-evidence.jsonl" {
				t.Fatal("sealed export embedded the raw parent evidence ledger")
			}
		}
	})
	t.Run("git-audit-metadata-only", func(t *testing.T) {
		if export.Manifest.Git == nil || len(export.Manifest.Git.Archives) != 1 {
			t.Fatalf("sealed git audit = %#v", export.Manifest.Git)
		}
		data := exportFileByPath(t, export, "attempt/git/archives/0.json")
		entry := exportEntryByPath(t, export.Manifest, "attempt/git/archives/0.json")
		var metadata evidenceExportGitArchiveMetadata
		if err := json.Unmarshal(data, &metadata); err != nil {
			t.Fatal(err)
		}
		if bytes.Contains(data, []byte(`"pack":`)) || entry.SHA256 != digestBytes(data) || entry.Bytes != int64(len(data)) || metadata.PackDigest == "" || metadata.CanonicalDigest != export.Manifest.Git.Archives[0].Digest || metadata.OmittedPayload == "" {
			t.Fatalf("sealed git archive must expose matching metadata without pack bytes: entry=%#v metadata=%#v bytes=%d", entry, metadata, len(data))
		}
		for _, file := range export.Files {
			if file.Path == "sealed/task-bundle.json" {
				t.Fatal("sealed export kept the nested task bundle projection")
			}
		}
	})
	t.Run("analysis-index", func(t *testing.T) {
		var analysis EvidenceAnalysisIndex
		if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
			t.Fatal(err)
		}
		for _, run := range analysis.ObservedValidationActions {
			if run.Basis != evidenceAnalysisBasisEvents || run.Log != "" {
				t.Fatalf("sealed validation event observation = %#v", run)
			}
		}
		for _, check := range []struct {
			name   string
			valid  bool
			actual any
		}{
			{"validation", len(analysis.CanonicalValidationRuns) == 0 && len(analysis.ObservedValidationActions) == 2 && len(analysis.Retries.ValidationReruns) == 1, analysis},
			{"sessions", analysis.Sessions.Status == evidenceAnalysisStatusCollected && len(analysis.Sessions.Sessions) == 1, analysis.Sessions},
			{"parent", analysis.Parent != nil && analysis.Parent.Status == evidenceAnalysisStatusCollected && analysis.Parent.ThreadID == "parent-thread-1", analysis.Parent},
			{"guardian", len(analysis.Guardian) == 1, analysis.Guardian},
			{"model-calls", analysis.ModelCalls.Status == evidenceAnalysisStatusCollected && analysis.ModelCalls.Total == 1, analysis.ModelCalls},
			{"review-rounds", analysis.ReviewRounds.Status == evidenceAnalysisStatusCollected && analysis.ReviewRounds.Count == 1, analysis.ReviewRounds},
		} {
			t.Run(check.name, func(t *testing.T) {
				if !check.valid {
					t.Fatalf("%s = %#v", check.name, check.actual)
				}
			})
		}
	})
}

func TestExportEvidenceSealedAttemptDisclosesUnreadableArchiveWithoutPathCollision(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	attempt, err := fixture.store.loadAttempt(evidenceExportSealedAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	attempt.BaselineArchive = EvidenceObjectRef{
		Digest: digestBytes([]byte("unreadable-baseline-archive")), Kind: "git-object-archive",
		MediaType: "application/octet-stream", Length: 8, LogicalIdentity: evidenceExportSealedAttemptID + ":baseline",
	}
	if err := fixture.store.writeAttempt(attempt); err != nil {
		t.Fatal(err)
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if len(export.Manifest.Git.Archives) != 1 || export.Manifest.Git.Archives[0].Digest != fixture.seal.GitObjectArchive.Digest {
		t.Fatalf("sealed git audit archives = %#v", export.Manifest.Git.Archives)
	}
	unreadable := exportEntryByPath(t, export.Manifest, "attempt/git/archives/0.json")
	if unreadable.Unreadable == "" || unreadable.SHA256 != "" {
		t.Fatalf("unreadable archive marker = %#v", unreadable)
	}
	for _, file := range export.Files {
		if file.Path == "attempt/git/archives/0.json" {
			t.Fatal("unreadable archive marker archived bytes")
		}
	}
	archived := exportEntryByPath(t, export.Manifest, "attempt/git/archives/1.json")
	if archived.Unreadable != "" || archived.SHA256 != digestBytes(exportFileByPath(t, export, "attempt/git/archives/1.json")) ||
		archived.Bytes != int64(len(exportFileByPath(t, export, "attempt/git/archives/1.json"))) {
		t.Fatalf("readable archive entry = %#v", archived)
	}
	seen := map[string]bool{}
	for _, entry := range export.Manifest.Entries {
		if seen[entry.Path] {
			t.Fatalf("export manifest has duplicate entry path %s", entry.Path)
		}
		seen[entry.Path] = true
	}
}

func TestExportEvidenceSealedAttemptCandidateEntriesHaveUniquePaths(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
		f.candidateEvidenceRounds = 2
		f.acceptedCandidateRevisions = 2
	})
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	validationRecords := map[string]string{}
	validationArtifacts := map[string]string{}
	acceptedCandidates := 0
	seenPaths := map[string]bool{}
	for _, entry := range export.Manifest.Entries {
		if seenPaths[entry.Path] {
			t.Fatalf("manifest entry path is not unique: %s", entry.Path)
		}
		seenPaths[entry.Path] = true
		if entry.Missing || entry.Unreadable != "" || entry.Unattributed != "" {
			continue
		}
		data := exportFileByPath(t, export, entry.Path)
		if entry.SHA256 != digestBytes(data) || entry.Bytes != int64(len(data)) {
			t.Fatalf("entry %s digest/bytes disagree with archived bytes", entry.Path)
		}
		switch {
		case entry.CanonicalKind == evidenceKindCandidateEvidence && entry.LogicalIdentity == evidenceExportSealedAttemptID+":"+candidateEvidenceKindValidation:
			validationRecords[entry.CanonicalDigest] = entry.Path
		case entry.LogicalIdentity == evidenceExportSealedAttemptID+":validation-artifact-0" || entry.LogicalIdentity == evidenceExportSealedAttemptID+":validation-artifact-1":
			validationArtifacts[string(data)] = entry.Path
		case entry.CanonicalKind == "accepted-candidate":
			acceptedCandidates++
		}
	}
	if len(validationRecords) != 2 {
		t.Fatalf("validation candidate revisions = %d (%v)", len(validationRecords), validationRecords)
	}
	if len(validationArtifacts) != 2 {
		t.Fatalf("distinct validation artifacts = %d (%v)", len(validationArtifacts), validationArtifacts)
	}
	if acceptedCandidates != 2 {
		t.Fatalf("accepted candidate revisions = %d", acceptedCandidates)
	}
	for _, file := range export.Files {
		if file.Path != "manifest.json" && !seenPaths[file.Path] && file.Path != "analysis-index.json" {
			t.Fatalf("archive member without a manifest entry: %s", file.Path)
		}
	}
}

func TestExportEvidenceSealedAttemptRepeatsSharedLineageRefIdempotently(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
		f.sharedLineageRevisions = 2
	})
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	acceptedCandidates := 0
	seenPaths := map[string]bool{}
	for _, entry := range export.Manifest.Entries {
		if seenPaths[entry.Path] {
			t.Fatalf("manifest entry path is not unique: %s", entry.Path)
		}
		seenPaths[entry.Path] = true
		if entry.CanonicalKind == "accepted-candidate" {
			acceptedCandidates++
		}
	}
	if acceptedCandidates != 1 {
		t.Fatalf("shared lineage ref materialized %d times", acceptedCandidates)
	}
	if result := export.Manifest.Attempt; result.Status != evidenceExportAttemptPresent {
		t.Fatalf("shared lineage chain lost the sealed attempt section: %#v", result)
	}
}

func TestExportEvidenceBoundSectionRecordsMissingTranscriptUnderBoundPrefix(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	fixture.bindSealedAttemptRuntime(t, rolloutTimestampedLine(5010, "claude-attempt-own"))
	if err := os.Remove(filepath.Join(fixture.cfg.ClaudeConfigDir, "projects", "fixture", "session-sealed-worker.jsonl")); err != nil {
		t.Fatal(err)
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	entry := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker")
	if !entry.Missing || entry.Source != evidenceExportSourceTranscript {
		t.Fatalf("bound missing transcript entry = %#v", entry)
	}
	if exportHasEntry(export.Manifest, "live/transcripts/claude/session-sealed-worker") {
		t.Fatal("bound export disclosed a missing transcript under the live prefix")
	}
}

func TestExportEvidenceRequiresRuntimeTaskIdentityForDefaultTargets(t *testing.T) {
	t.Parallel()
	t.Run("live attempt without a runtime binding", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.skipRuntimeEvidence = true
		})
		if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
			t.Fatal("export accepted a live attempt without a resolvable runtime task identity")
		}
	})
	t.Run("recent sealed attempt without a canonical association", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.skipLiveAttempt = true
			f.skipRuntimeEvidence = true
		})
		if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
			t.Fatal("export accepted a sealed attempt without a canonical runtime task association")
		}
	})
}

func TestExportEvidenceAggregatesParentEvidenceRepetitions(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	ledger := evidenceTestParentEvidenceLedger(t, time.Unix(6500, 0).UTC())
	if err := os.WriteFile(fixture.runtime.Path(evidenceExportParentEvidenceFile), ledger, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"parent-evidence-ledger.lock", "worker.ready"} {
		if err := os.WriteFile(fixture.runtime.Path(name), []byte("ephemeral\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entry := exportEntryByPath(t, export.Manifest, "live/state/parent-evidence.aggregate.json")
	if entry.Missing || entry.Unreadable != "" || entry.OmittedPayload == "" || entry.Basis != evidenceExportBasisLiveRuntime {
		t.Fatalf("parent evidence aggregate entry = %#v", entry)
	}
	if entry.SHA256 != digestBytes(exportFileByPath(t, export, "live/state/parent-evidence.aggregate.json")) {
		t.Fatal("aggregate entry digest does not match archived bytes")
	}
	var aggregate evidenceParentEvidenceAggregate
	if err := json.Unmarshal(exportFileByPath(t, export, "live/state/parent-evidence.aggregate.json"), &aggregate); err != nil {
		t.Fatal(err)
	}
	if aggregate.Records != 6 || aggregate.GroupCount != 4 || aggregate.RepetitionsOmitted != 2 ||
		aggregate.Outcomes[state.ParentEvidenceOutcomeProjected] != 3 ||
		aggregate.Outcomes[state.ParentEvidenceOutcomeDuplicate] != 2 ||
		aggregate.Outcomes[state.ParentEvidenceOutcomeError] != 1 ||
		aggregate.SourceSHA256 != digestBytes(ledger) || aggregate.SourceBytes != int64(len(ledger)) {
		t.Fatalf("parent evidence aggregate = %#v", aggregate)
	}
	if len(aggregate.Groups) != 4 || aggregate.Groups[0].Count != 2 ||
		aggregate.Groups[0].LastTime.IsZero() || aggregate.Groups[2].Outcome != state.ParentEvidenceOutcomeDuplicate {
		t.Fatalf("aggregate groups lost transitions or counts: %#v", aggregate.Groups)
	}
	if !exportHasEntry(export.Manifest, "live/state/worker.id") {
		t.Fatal("aggregate projection dropped the canonical worker identity file")
	}
	for _, path := range []string{
		"live/state/parent-evidence.jsonl",
		"live/state/parent-evidence-ledger.lock",
		"live/state/worker.ready",
	} {
		if exportHasEntry(export.Manifest, path) {
			t.Fatalf("export embedded %s", path)
		}
	}
}

func TestExportEvidenceRejectsUnknownTargetsAndSelectors(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: "runtime-task-unknown"}); err == nil {
		t.Fatal("export accepted an unknown runtime task id")
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: "../escape"}); err == nil {
		t.Fatal("export accepted an unsafe runtime task id")
	}
	empty := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.skipSealedEvidence = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	if _, _, err := empty.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export accepted a repository without executed tasks")
	}
}

func TestExportEvidenceTaskIDResolvesSurvivingRuntimeBinding(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.skipLiveHead = true
	})
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: fixture.runtimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.SelectionBasis != evidenceExportBasisRuntimeBinding || result.Target.AttemptID != evidenceExportLiveAttemptID {
		t.Fatalf("runtime binding target = %#v", result.Target)
	}
	if result.RuntimeStatus != evidenceExportRuntimeCollected || export.Manifest.Runtime.Mode != evidenceExportRuntimeModeBound {
		t.Fatalf("non-live binding target runtime section = %#v", export.Manifest.Runtime)
	}
	if result.Coverage != evidenceExportCoveragePartial {
		t.Fatalf("non-live binding target coverage = %q", result.Coverage)
	}
	if export.Manifest.Runtime.Basis != evidenceExportBasisRuntimeBinding {
		t.Fatalf("bound runtime section basis = %q", export.Manifest.Runtime.Basis)
	}
	if export.Manifest.Runtime.Window == nil || export.Manifest.Runtime.Window.EndBasis != evidenceExportBoundNoEndBoundary ||
		!export.Manifest.Runtime.Window.End.IsZero() {
		t.Fatalf("unsealed bound runtime window = %#v", export.Manifest.Runtime.Window)
	}
	boundGate := exportEntryByPath(t, export.Manifest, "bound/validation/quality-gate/"+evidenceExportGateRunID+"/run.json")
	if boundGate.Basis != evidenceExportBasisRuntimeValidation || boundGate.Missing {
		t.Fatalf("bound gate run entry = %#v", boundGate)
	}
	if !exportHasEntry(export.Manifest, "bound/validation/install-smoke/"+evidenceExportSmokeRunID+"/smoke.log") {
		t.Fatal("bound export lost the install smoke evidence")
	}
	boundPlan := exportEntryByPath(t, export.Manifest, "bound/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath)
	if boundPlan.Unreadable != evidenceExportInstructionNotSealedReason {
		t.Fatalf("bound plan snapshot entry = %#v", boundPlan)
	}
	boundTelemetry := exportEntryByPath(t, export.Manifest, "bound/task/telemetry.jsonl")
	if boundTelemetry.InProgress || boundTelemetry.Basis != evidenceExportBasisLiveRuntime {
		t.Fatalf("bound telemetry entry = %#v", boundTelemetry)
	}
	liveTelemetry := false
	for _, entry := range export.Manifest.Entries {
		if entry.Path == "live/task/telemetry.jsonl" {
			liveTelemetry = true
		}
	}
	if liveTelemetry {
		t.Fatal("bound export wrote in-progress live entries")
	}
}

func TestExportEvidenceBoundSectionExcludesPostAttemptSessionRecords(t *testing.T) {
	t.Parallel()
	sealedOptions := func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	}
	transcriptLine := func(seconds int64, line string) string {
		return `{"timestamp":"` + time.Unix(seconds, 0).UTC().Format(time.RFC3339Nano) + `","line":"` + line + `"}` + "\n"
	}

	t.Run("timestamped records after the seal are excluded", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, sealedOptions)
		fixture.bindSealedAttemptRuntime(t,
			transcriptLine(4990, "earlier-task")+
				transcriptLine(5010, "attempt-own")+
				transcriptLine(5100, "attempt-final")+
				transcriptLine(5200, "contaminating-later-task"))
		export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
		if err != nil {
			t.Fatal(err)
		}
		if result.RuntimeStatus != evidenceExportRuntimeCollected || export.Manifest.Runtime.Mode != evidenceExportRuntimeModeBound {
			t.Fatalf("bound sealed export runtime = %#v", export.Manifest.Runtime)
		}
		window := export.Manifest.Runtime.Window
		if window == nil || !window.End.Equal(fixture.seal.SealedAt) || window.EndBasis != evidenceExportBasisAttemptSeal {
			t.Fatalf("bound sealed window = %#v", window)
		}
		entry := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker/0")
		if entry.Unattributed != "" || entry.Window == nil || entry.Window.RecordsBefore != 1 || entry.Window.RecordsAfter != 1 ||
			entry.Window.Basis != evidenceWindowBasisTimestamp {
			t.Fatalf("bound transcript entry = %#v", entry)
		}
		if entry.SHA256 != digestBytes(exportFileByPath(t, export, "bound/transcripts/claude/session-sealed-worker/0")) {
			t.Fatal("bound transcript digest does not match archived bytes")
		}
		for _, file := range export.Files {
			if bytes.Contains(file.Data, []byte("contaminating-later-task")) {
				t.Fatalf("export %s leaked a record appended after the attempt sealed", file.Path)
			}
		}
	})

	t.Run("timestamp-less records are bounded by canonical capture offsets", func(t *testing.T) {
		attemptDedicated := "{\"line\":\"attempt-own\"}\n{\"line\":\"attempt-final\"}\n"
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			sealedOptions(f)
			f.sealedModelWindows = []runtimeTranscriptWindowRecord{{
				SessionID:  "session-sealed-worker",
				TotalBytes: int64(len(attemptDedicated)), StartOffset: 0, EndOffset: int64(len(attemptDedicated)),
				Basis: evidenceWindowBasisCaptureOffsets,
			}}
		})
		fixture.bindSealedAttemptRuntime(t, attemptDedicated+"{\"line\":\"contaminating-later-task\"}\n")
		export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
		if err != nil {
			t.Fatal(err)
		}
		entry := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker/0")
		if entry.Unattributed != "" || entry.Window == nil || entry.Window.Basis != evidenceWindowBasisCaptureOffsets ||
			entry.Window.RecordsAfter != 1 {
			t.Fatalf("capture-offset bound entry = %#v", entry)
		}
		if string(exportFileByPath(t, export, "bound/transcripts/claude/session-sealed-worker/0")) != attemptDedicated {
			t.Fatalf("capture-offset bound bytes = %q", exportFileByPath(t, export, "bound/transcripts/claude/session-sealed-worker/0"))
		}
	})

	t.Run("timestamp-less records without canonical offsets stay unattributed", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, sealedOptions)
		fixture.bindSealedAttemptRuntime(t, "{\"line\":\"attempt-own\"}\n{\"line\":\"contaminating-later-task\"}\n")
		export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
		if err != nil {
			t.Fatal(err)
		}
		entry := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker/0")
		if entry.Unattributed == "" || entry.SHA256 != "" {
			t.Fatalf("unattributed bound entry = %#v", entry)
		}
		if export.Manifest.Runtime.Unattributed == nil {
			t.Fatalf("runtime section hides unattributed transcripts: %#v", export.Manifest.Runtime)
		}
		for _, file := range export.Files {
			if file.Path == "bound/transcripts/claude/session-sealed-worker/0" {
				t.Fatal("unattributed transcript bytes were archived without an attempt-dedicated range")
			}
		}
	})
}

func rolloutTimestampedLine(seconds int64, line string) string {
	return `{"timestamp":"` + time.Unix(seconds, 0).UTC().Format(time.RFC3339Nano) + `","line":"` + line + `"}` + "\n"
}

func rolloutMetaLine(seconds int64, id, parentThreadID string, guardian bool) string {
	source := ""
	if guardian {
		source = `,"source":{"subagent":{"other":"guardian"}}`
	}
	parent := ""
	if parentThreadID != "" {
		parent = `,"parent_thread_id":"` + parentThreadID + `"`
	}
	return `{"type":"session_meta","timestamp":"` + time.Unix(seconds, 0).UTC().Format(time.RFC3339Nano) + `","payload":{"id":"` + id + `"` + parent + source + `}}` + "\n"
}

func writeEvidenceExportRollout(t *testing.T, codexHome, name string, data []byte) string {
	t.Helper()
	path := filepath.Join(codexHome, "sessions", name)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func (f *evidenceExportFixture) bindRuntimeParentIdentity(t *testing.T, threadID, sessionID string) *state.StateStore {
	t.Helper()
	runtimeCfg := f.cfg
	runtimeCfg.RepoHash = f.runtimeRepoHash()
	runtime, err := state.NewStateStore(runtimeCfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetParentCodexIdentity(threadID, sessionID, func() *state.SessionLimitReading { return nil }); err != nil {
		t.Fatal(err)
	}
	return runtime
}

func writeParentGuardianRolloutSet(t *testing.T, fixture *evidenceExportFixture, attemptStart int64) (string, []byte) {
	t.Helper()
	parentBytes := []byte(strings.Join([]string{
		strings.TrimSuffix(rolloutMetaLine(attemptStart-16, evidenceExportParentThreadID, "", false), "\n"),
		rolloutTimestampedLine(attemptStart-11, "before-attempt-window"),
		rolloutTimestampedLine(attemptStart+9, "attempt-own"),
		rolloutTimestampedLine(attemptStart+99, "attempt-final"),
		rolloutTimestampedLine(attemptStart+199, "after-seal"),
	}, "\n") + "\n")
	parentPath := writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, evidenceExportParentThreadID+".jsonl", parentBytes)
	earlyGuardian := []byte(strings.TrimSuffix(rolloutMetaLine(attemptStart-51, evidenceExportEarlyGuardianID, evidenceExportParentThreadID, true), "\n") + "\n" +
		rolloutTimestampedLine(attemptStart-41, "guardian-before-attempt"))
	writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, evidenceExportEarlyGuardianID+".jsonl", earlyGuardian)
	activeGuardian := []byte(strings.TrimSuffix(rolloutMetaLine(attemptStart+9, evidenceExportActiveGuardianID, evidenceExportParentThreadID, true), "\n") + "\n" +
		rolloutTimestampedLine(attemptStart+14, "guardian-attempt-own") +
		rolloutTimestampedLine(attemptStart+199, "guardian-after-seal"))
	writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, evidenceExportActiveGuardianID+".jsonl", activeGuardian)
	writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, evidenceExportForeignThreadID+".jsonl",
		[]byte(rolloutMetaLine(attemptStart+5, evidenceExportForeignThreadID, "", false)+rolloutTimestampedLine(attemptStart+6, "foreign-parent-record")))
	writeEvidenceExportRollout(t, fixture.cfg.CodexConfigDir, "guardian-foreign.jsonl",
		[]byte(rolloutMetaLine(attemptStart+5, "guardian-foreign", evidenceExportForeignThreadID, true)+rolloutTimestampedLine(attemptStart+6, "foreign-guardian-record")))
	return parentPath, parentBytes
}

func TestExportEvidenceBoundSectionCollectsParentAndGuardianRawLogs(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	fixture.bindSealedAttemptRuntime(t,
		rolloutTimestampedLine(4990, "claude-earlier-task")+
			rolloutTimestampedLine(5010, "claude-attempt-own")+
			rolloutTimestampedLine(5200, "claude-after-seal"))
	fixture.bindRuntimeParentIdentity(t, evidenceExportParentThreadID, evidenceExportParentSessionID)
	parentPath, parentBytes := writeParentGuardianRolloutSet(t, fixture, 5001)
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeStatus != evidenceExportRuntimeCollected || export.Manifest.Runtime.Mode != evidenceExportRuntimeModeBound {
		t.Fatalf("bound sealed export runtime = %#v", export.Manifest.Runtime)
	}
	parent := exportEntryByPath(t, export.Manifest, "bound/transcripts/parent/"+evidenceExportParentThreadID+"/0")
	if parent.SHA256 != digestBytes(parentBytes) || parent.Bytes != int64(len(parentBytes)) {
		t.Fatalf("parent raw entry = %#v", parent)
	}
	if string(exportFileByPath(t, export, "bound/transcripts/parent/"+evidenceExportParentThreadID+"/0")) != string(parentBytes) {
		t.Fatal("parent rollout was trimmed instead of archived as the associated raw log")
	}
	if parent.Window == nil || parent.Window.Basis != evidenceWindowBasisRawLog || parent.Window.TotalBytes != int64(len(parentBytes)) {
		t.Fatalf("parent raw window = %#v", parent.Window)
	}
	for _, path := range []string{
		"bound/transcripts/guardian/" + evidenceExportEarlyGuardianID,
		"bound/transcripts/guardian/" + evidenceExportActiveGuardianID,
	} {
		guardian := exportEntryByPath(t, export.Manifest, path)
		if guardian.Window == nil || guardian.Window.Basis != evidenceWindowBasisRawLog || guardian.Missing || guardian.Unreadable != "" {
			t.Fatalf("guardian raw entry %s = %#v", path, guardian)
		}
	}
	if !bytes.Contains(exportFileByPath(t, export, "bound/transcripts/guardian/"+evidenceExportEarlyGuardianID), []byte("guardian-before-attempt")) {
		t.Fatal("export dropped a guardian rollout that ended before the target attempt started")
	}
	if !bytes.Contains(exportFileByPath(t, export, "bound/transcripts/guardian/"+evidenceExportActiveGuardianID), []byte("guardian-after-seal")) {
		t.Fatal("export trimmed guardian records appended after the attempt sealed")
	}
	for _, file := range export.Files {
		if bytes.Contains(file.Data, []byte("foreign-parent-record")) || bytes.Contains(file.Data, []byte("foreign-guardian-record")) {
			t.Fatalf("export absorbed a rollout outside the parent association: %s", file.Path)
		}
	}
	claude := exportEntryByPath(t, export.Manifest, "bound/transcripts/claude/session-sealed-worker/0")
	if claude.Window == nil || claude.Window.Basis != evidenceWindowBasisTimestamp || claude.Window.RecordsBefore != 1 || claude.Window.RecordsAfter != 1 {
		t.Fatalf("model transcript kept its attempt-dedicated window = %#v", claude.Window)
	}
	grown := append([]byte(nil), parentBytes...)
	grown = append(grown, []byte(rolloutTimestampedLine(5300, "grown-after-export"))...)
	if err := os.WriteFile(parentPath, grown, 0o600); err != nil {
		t.Fatal(err)
	}
	reexport, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(exportFileByPath(t, reexport, "bound/transcripts/parent/"+evidenceExportParentThreadID+"/0"), []byte("grown-after-export")) {
		t.Fatal("re-export did not pick up parent log growth from after the previous bundle")
	}
}

func TestExportEvidenceLiveSectionCollectsFullParentRollout(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	fixture.bindRuntimeParentIdentity(t, evidenceExportParentThreadID, evidenceExportParentSessionID)
	_, parentBytes := writeParentGuardianRolloutSet(t, fixture, 6000)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	entry := exportEntryByPath(t, export.Manifest, "live/transcripts/parent/"+evidenceExportParentThreadID+"/0")
	if entry.SHA256 != digestBytes(parentBytes) || entry.Window == nil || entry.Window.Basis != evidenceWindowBasisRawLog {
		t.Fatalf("live parent raw entry = %#v", entry)
	}
	if string(exportFileByPath(t, export, "live/transcripts/parent/"+evidenceExportParentThreadID+"/0")) != string(parentBytes) {
		t.Fatal("live export trimmed the parent rollout instead of archiving the associated raw log")
	}
	if !exportHasEntry(export.Manifest, "live/transcripts/guardian/"+evidenceExportEarlyGuardianID) {
		t.Fatal("live export dropped a guardian rollout that predates the live attempt")
	}
}

func TestExportEvidenceDisclosesMissingValidationLogs(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	logless := qualitygate.RunRecord{
		ValidationRunID: "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", Form: "go-test",
		Repository: fixture.repo, WorkingDir: fixture.repo,
		StartedAt: time.Unix(6100, 0).UTC(), Status: qualitygate.StatusPass,
		Log: "quality-gate-runs/eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee/gate.log",
	}
	runData, err := json.Marshal(logless)
	if err != nil {
		t.Fatal(err)
	}
	runDir := fixture.runtime.Path(filepath.Join(qualitygate.RunDirectory, logless.ValidationRunID))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runDir, qualitygate.RunFile), runData, 0o600); err != nil {
		t.Fatal(err)
	}

	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	missingLog := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+logless.ValidationRunID+"/gate.log")
	if !missingLog.Missing || missingLog.Source != evidenceExportSourceGateRun {
		t.Fatalf("missing gate log entry = %#v", missingLog)
	}
	foundMissing := false
	for _, path := range export.Manifest.Runtime.Missing {
		if path == missingLog.Path {
			foundMissing = true
		}
	}
	if !foundMissing {
		t.Fatalf("runtime section hides the missing gate log: %#v", export.Manifest.Runtime.Missing)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	for _, run := range analysis.CanonicalValidationRuns {
		if run.RunID == logless.ValidationRunID && (run.Log != "" || run.Basis != evidenceExportBasisRuntimeValidation) {
			t.Fatalf("logless run analysis relation = %#v", run)
		}
	}
}

func TestExportEvidenceFailsClosedOnForeignGateRunInTaskStore(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	conflicting := qualitygate.RunRecord{
		ValidationRunID: "abababababababababababababababab", Form: "go-test",
		Repository: fixture.repo, WorkingDir: fixture.repo,
		StartedAt: time.Unix(6100, 0).UTC(), Status: qualitygate.StatusPass,
		TaskID: "another-runtime-task",
	}
	writeEvidenceGateRun(t, fixture.runtime, conflicting)
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export absorbed a gate run whose record is bound to another task")
	}
}

func TestExportEvidenceFailsClosedOnRuntimeRebinding(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	binding, err := fixture.runtime.LoadControllerRuntimeBinding()
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err != nil {
		t.Fatal(err)
	}
	if err := fixture.runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID: "attempt-other", TaskPath: fixture.seal.SemanticTaskRef.TaskPath,
		TaskContractDigest: fixture.seal.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export accepted a runtime source rebound to another attempt")
	}
	if err := verifyExportRuntimeBindingStable(fixture.runtime, binding); err == nil {
		t.Fatal("binding stability check accepted a rebound runtime source")
	}
	if err := fixture.runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID: evidenceExportLiveAttemptID, TaskPath: "IMPLEMENTATION_TASKS/other.md",
		TaskContractDigest: fixture.seal.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export accepted a runtime source rebound to another task")
	}
}

func TestExportEvidenceRejectsUnverifiedWorkspace(t *testing.T) {
	t.Parallel()
	fixture := newEvidenceExportFixture(t)
	lease, err := fixture.store.loadLease(evidenceExportLiveLeaseID)
	if err != nil {
		t.Fatal(err)
	}
	lease.WorkspaceID = "workspace-other"
	data, err := json.Marshal(lease)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(fixture.store.dir, "leases", evidenceExportLiveLeaseID+".json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{}); err == nil {
		t.Fatal("export accepted a lease workspace identity that does not match the repository")
	}
}

func TestEvidenceExportObservationAndFileChangeDetection(t *testing.T) {
	t.Parallel()
	before := RepositoryControllerHead{ControllerGeneration: 3, LiveAttemptID: "a1", LiveLeaseID: "l1"}
	same := evidenceExportControllerObservation(before, before)
	if same.AuthorityChangedDuringExport {
		t.Fatalf("stable head reported authority change: %#v", same)
	}
	after := before
	after.ControllerGeneration = 4
	after.LiveLeaseID = "l2"
	changed := evidenceExportControllerObservation(before, after)
	if !changed.AuthorityChangedDuringExport || changed.GenerationBefore != 3 || changed.GenerationAfter != 4 {
		t.Fatalf("changed head observation = %#v", changed)
	}

	path := filepath.Join(t.TempDir(), "changing.jsonl")
	if err := os.WriteFile(path, []byte("{\"a\":1}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(2 * time.Millisecond)
	if err := os.WriteFile(path, []byte("{\"a\":1}\n{\"b\""), 0o600); err != nil {
		t.Fatal(err)
	}
	grown, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if !evidenceExportFileChanged(info, grown) {
		t.Fatal("file growth was not detected as a change")
	}
	records, trailing := evidenceExportJSONLStats("live/task/x.jsonl", []byte("{\"a\":1}\n{\"b\""))
	if records != 1 || trailing != true {
		t.Fatalf("jsonl stats records=%d trailing=%t", records, trailing)
	}
	completeRecords, completeTrailing := evidenceExportJSONLStats("live/task/x.jsonl", []byte("{\"a\":1}\n"))
	if completeRecords != 1 || completeTrailing {
		t.Fatalf("complete jsonl stats records=%d trailing=%t", completeRecords, completeTrailing)
	}
}

func TestScanTranscriptWindowBoundsReusedSessions(t *testing.T) {
	t.Parallel()
	data := []byte("{\"timestamp\":\"2026-10-05T00:00:00Z\",\"n\":1}\n" +
		"{\"timestamp\":\"2026-10-05T12:00:00Z\",\"n\":2}\n" +
		"{\"timestamp\":\"2026-10-06T00:00:00Z\",\"n\":3}\n" +
		"{\"timestamp\":\"2026-10-07T00:00:00Z\",\"n\":4}\n" +
		"{\"timestamp\":\"2026-10-08T00:00:00Z\",\"n\":5}\n")
	window, windowed := scanTranscriptWindow(data, time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC), time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC), nil)
	if window.RecordsBefore != 2 || window.RecordsAfter != 1 {
		t.Fatalf("window records before=%d after=%d", window.RecordsBefore, window.RecordsAfter)
	}
	if window.Basis != evidenceWindowBasisTimestamp || window.TotalBytes != int64(len(data)) {
		t.Fatalf("window = %#v", window)
	}
	if string(windowed) != "{\"timestamp\":\"2026-10-06T00:00:00Z\",\"n\":3}\n{\"timestamp\":\"2026-10-07T00:00:00Z\",\"n\":4}\n" {
		t.Fatalf("windowed bytes = %q", windowed)
	}
	excluded := len("{\"timestamp\":\"2026-10-08T00:00:00Z\",\"n\":5}\n")
	if window.EndOffset != int64(len(data)-excluded) || window.StartOffset <= 0 {
		t.Fatalf("window offsets = %#v", window)
	}
	unattributed, none := scanTranscriptWindow([]byte("{\"line\":1}\n"), time.Now(), time.Time{}, nil)
	if unattributed.Basis != evidenceWindowBasisUnbounded || unattributed.Unattributed == "" || none != nil {
		t.Fatalf("timestamp-less window without capture offsets = %#v bytes = %q", unattributed, none)
	}
	attemptDedicated := "{\"line\":1}\n{\"line\":2}\n"
	grown := []byte(attemptDedicated + "{\"line\":3}\n")
	capture := &runtimeTranscriptWindowRecord{
		TotalBytes: int64(len(attemptDedicated)), StartOffset: 0, EndOffset: int64(len(attemptDedicated)),
		Basis: evidenceWindowBasisCaptureOffsets,
	}
	offsetWindow, offsetBytes := scanTranscriptWindow(grown, time.Time{}, time.Time{}, capture)
	if offsetWindow.Basis != evidenceWindowBasisCaptureOffsets || offsetWindow.Unattributed != "" ||
		offsetWindow.RecordsAfter != 1 || string(offsetBytes) != attemptDedicated {
		t.Fatalf("capture-offset window = %#v bytes = %q", offsetWindow, offsetBytes)
	}
	stale := &runtimeTranscriptWindowRecord{TotalBytes: int64(len(grown)), StartOffset: 0, EndOffset: int64(len(grown)) + 1}
	staleWindow, staleBytes := scanTranscriptWindow(grown, time.Time{}, time.Time{}, stale)
	if staleWindow.Unattributed == "" || staleBytes != nil {
		t.Fatalf("stale capture-offset window = %#v bytes = %q", staleWindow, staleBytes)
	}
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
