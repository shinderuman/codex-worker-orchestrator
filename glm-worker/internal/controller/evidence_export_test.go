package controller

import (
	"bytes"
	"encoding/json"
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
}

const (
	evidenceExportLiveAttemptID     = "attempt-live-1"
	evidenceExportSealedAttemptID   = "attempt-b1"
	evidenceExportSealedRuntimeTask = "runtime-task-b1"
	evidenceExportLiveLeaseID       = "lease-live-1"
	evidenceExportGateRunID         = "0123456789abcdef0123456789abcdef"
	evidenceExportSmokeRunID        = "fedcba9876543210fedcba9876543210"
)

func newEvidenceExportFixture(t *testing.T, options ...func(*evidenceExportFixture)) *evidenceExportFixture {
	t.Helper()
	repo, _ := newControllerLinkedWorktree(t)
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
	source, _ := newControllerLinkedWorktree(t)
	sealRef, seal := storeBundleAttemptSeal(t, f.store, source, evidenceExportSealedAttemptID, "IMPLEMENTATION_TASKS/B.md", "episode-1", 1)
	if f.withSealedRuntime {
		sealRef, seal = f.attachSealedRuntimeEvidence(t, seal)
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
	fixture := newEvidenceExportFixture(t)
	objectsBefore := evidenceObjectTreeDigest(t, fixture.store)
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.RuntimeStatus != evidenceExportRuntimeCollected || result.Coverage != evidenceExportCoveragePartial {
		t.Fatalf("live export result = %#v", result)
	}
	if result.AttemptSectionStatus != evidenceExportAttemptAbsent {
		t.Fatalf("live attempt section status = %q", result.AttemptSectionStatus)
	}
	if result.Target.Live != true || result.Target.AttemptID != evidenceExportLiveAttemptID || result.Target.Kind != evidenceExportTargetRunning {
		t.Fatalf("export target = %#v", result.Target)
	}
	if result.Target.RuntimeTaskID != fixture.runtimeTask || result.Target.SelectionBasis != evidenceExportBasisLiveAttempt {
		t.Fatalf("export target selection = %#v", result.Target)
	}
	if result.ManifestDigest != digestBytes(exportFileByPath(t, export, "manifest.json")) {
		t.Fatal("manifest digest does not match manifest bytes")
	}
	telemetry := exportEntryByPath(t, export.Manifest, "live/task/telemetry.jsonl")
	if telemetry.TrailingFragment != true || telemetry.Records != 1 || telemetry.InProgress != true {
		t.Fatalf("telemetry entry = %#v", telemetry)
	}
	if telemetry.SHA256 != digestBytes(exportFileByPath(t, export, "live/task/telemetry.jsonl")) {
		t.Fatal("telemetry entry digest does not match stored bytes")
	}
	transcript := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/session-worker/0")
	if transcript.SHA256 != digestBytes([]byte(`{"timestamp":"1970-01-01T02:00:01Z","line":"in-window"}`+"\n")) {
		t.Fatalf("transcript entry = %#v", transcript)
	}
	if transcript.Window == nil || transcript.Window.RecordsBefore != 1 || transcript.Window.Basis != evidenceWindowBasisTimestamp {
		t.Fatalf("transcript window = %#v", transcript.Window)
	}
	missing := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/session-reviewer")
	if missing.Missing != true {
		t.Fatalf("missing transcript entry = %#v", missing)
	}
	unsafe := exportEntryByPath(t, export.Manifest, "live/transcripts/claude/../escape")
	if unsafe.Unreadable == "" || unsafe.Missing {
		t.Fatalf("unsafe session id entry = %#v", unsafe)
	}
	for _, file := range export.Files {
		if strings.Contains(file.Path, "..") || strings.Contains(file.Path, `\`) || strings.HasPrefix(file.Path, "/") {
			t.Fatalf("export archive entry escapes its prefix: %s", file.Path)
		}
	}
	artifact := exportEntryByPath(t, export.Manifest, "live/task/artifacts/report.txt")
	if artifact.SHA256 != digestBytes([]byte("artifact bytes\n")) {
		t.Fatalf("artifact entry = %#v", artifact)
	}
	staged := exportFileByPath(t, export, "live/git/diff-staged.patch")
	if !bytes.Contains(staged, []byte("source.txt")) {
		t.Fatalf("staged diff omits staged change: %s", staged)
	}
	untracked := exportEntryByPath(t, export.Manifest, "live/git/untracked/untracked-note.txt")
	if untracked.SHA256 != digestBytes([]byte("untracked bytes\n")) {
		t.Fatalf("untracked entry = %#v", untracked)
	}
	planSnapshot := exportEntryByPath(t, export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath)
	if planSnapshot.Basis != evidenceExportBasisWorkspaceObservation || planSnapshot.SHA256 == "" {
		t.Fatalf("plan snapshot entry = %#v", planSnapshot)
	}
	if !exportHasEntry(export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionRulesPath) {
		t.Fatal("live export lost the rules snapshot")
	}
	if !exportHasEntry(export.Manifest, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionHistoryPath) {
		t.Fatal("live export lost the history snapshot")
	}
	for _, entry := range export.Manifest.Entries {
		if entry.Path == "live/git/object-archive.json" {
			t.Fatal("live export embedded a git object archive payload")
		}
	}
	if export.Manifest.Git == nil || export.Manifest.Git.ExecutionBaseOID == "" || export.Manifest.Git.Snapshot == nil {
		t.Fatalf("live git audit = %#v", export.Manifest.Git)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	if analysis.Sessions.Status != evidenceAnalysisStatusCollected || len(analysis.Sessions.Sessions) == 0 {
		t.Fatalf("analysis sessions = %#v", analysis.Sessions)
	}
	if analysis.Window.Start.IsZero() || analysis.Window.End.IsZero() {
		t.Fatalf("analysis window = %#v", analysis.Window)
	}
	if analysis.Instructions.Status != evidenceAnalysisStatusCollected || analysis.Instructions.Task == nil ||
		analysis.Instructions.Task.Entry != "live/task/authority.md" || analysis.Instructions.Plan == nil ||
		analysis.Instructions.Plan.Entry != "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath ||
		analysis.Instructions.Rules == nil || analysis.Instructions.Rules.SHA256 == "" ||
		analysis.Instructions.History == nil || analysis.Instructions.History.SHA256 == "" {
		t.Fatalf("analysis instructions = %#v", analysis.Instructions)
	}
	if export.Manifest.Attempt.AbsenceReason == "" {
		t.Fatalf("live attempt absence reason is empty: %#v", export.Manifest.Attempt)
	}
	if evidenceObjectTreeDigest(t, fixture.store) != objectsBefore {
		t.Fatal("live evidence export mutated controller evidence objects")
	}
}

func TestExportEvidenceCollectsTaskValidationEvidence(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	objectsBefore := evidenceObjectTreeDigest(t, fixture.store)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	gateMeta := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+evidenceExportGateRunID+"/run.json")
	if gateMeta.Basis != evidenceExportBasisRuntimeValidation || gateMeta.Missing || gateMeta.Unreadable != "" {
		t.Fatalf("gate run metadata entry = %#v", gateMeta)
	}
	if string(exportFileByPath(t, export, "live/validation/quality-gate/"+evidenceExportGateRunID+"/gate.log")) != "gate log for "+evidenceExportGateRunID+"\n" {
		t.Fatal("gate run log bytes were not archived")
	}
	smoke := exportEntryByPath(t, export.Manifest, "live/validation/install-smoke/"+evidenceExportSmokeRunID+"/smoke.log")
	if smoke.Missing || smoke.Unreadable != "" {
		t.Fatalf("install smoke entry = %#v", smoke)
	}
	smokeMeta := exportEntryByPath(t, export.Manifest, "live/validation/install-smoke/"+evidenceExportSmokeRunID+"/run.json")
	if smokeMeta.Basis != evidenceExportBasisRuntimeValidation || smokeMeta.Missing {
		t.Fatalf("install smoke run record entry = %#v", smokeMeta)
	}
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	if !analysisValidationRunPresent(analysis.CanonicalValidationRuns, evidenceExportGateRunID, evidenceExportBasisRuntimeValidation) {
		t.Fatalf("analysis canonical runs miss the gate run: %#v", analysis.CanonicalValidationRuns)
	}
	for _, run := range analysis.CanonicalValidationRuns {
		if run.RunID == evidenceExportGateRunID && run.Basis == evidenceExportBasisRuntimeValidation &&
			run.Binding != evidenceValidationBindingTaskStore {
			t.Fatalf("task-store gate run binding = %#v", run)
		}
	}
	if !analysisValidationRunPresent(analysis.CanonicalValidationRuns, evidenceExportSmokeRunID, evidenceExportBasisRuntimeValidation) {
		t.Fatalf("analysis canonical runs miss the install smoke run: %#v", analysis.CanonicalValidationRuns)
	}
	gateCanonicalRows := 0
	for _, run := range analysis.CanonicalValidationRuns {
		if run.Basis == evidenceAnalysisBasisBlockObservation || run.Basis == evidenceAnalysisBasisEvents {
			t.Fatalf("observation leaked into canonical runs: %#v", run)
		}
		if run.RunID == evidenceExportGateRunID {
			gateCanonicalRows++
		}
		if run.Log != "" && !exportHasEntry(export.Manifest, run.Log) {
			t.Fatalf("canonical run log does not reference an archive entry: %#v", run)
		}
	}
	if gateCanonicalRows != 1 {
		t.Fatalf("gate run appears %d times in canonical runs: %#v", gateCanonicalRows, analysis.CanonicalValidationRuns)
	}
	eventObserved := false
	mergedDuplicates := 0
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
	if !eventObserved {
		t.Fatalf("observed validation actions lost the gate run event relation: %#v", analysis.ObservedValidationActions)
	}
	if mergedDuplicates != 2 || len(analysis.ObservedValidationActions) != 3 {
		t.Fatalf("observed validation actions lost duplicate provenance: %#v", analysis.ObservedValidationActions)
	}
	if len(analysis.Retries.ValidationReruns) == 0 {
		t.Fatalf("analysis lost the retried observations: %#v", analysis.Retries)
	}
	if len(analysis.EvidenceStates.InProgress) == 0 {
		t.Fatalf("analysis in-progress states are empty: %#v", analysis.EvidenceStates)
	}
	if evidenceObjectTreeDigest(t, fixture.store) != objectsBefore {
		t.Fatal("live evidence export mutated controller evidence objects")
	}
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

func TestExportEvidenceDecodesRoundTrip(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	export, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	data := exportFileByPath(t, export, "manifest.json")
	var decoded EvidenceExportManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Format != evidenceExportFormat || decoded.SchemaVersion != evidenceSchemaVersion {
		t.Fatalf("manifest identity = %#v", decoded)
	}
	if decoded.AnalysisIndex.Path != "analysis-index.json" || decoded.AnalysisIndex.SHA256 != digestBytes(exportFileByPath(t, export, "analysis-index.json")) {
		t.Fatalf("analysis index reference = %#v", decoded.AnalysisIndex)
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

func TestExportEvidenceSealedAttemptMaterializesRuntimeEvidence(t *testing.T) {
	export, result := sealedRuntimeExportFixture(t)
	if result.Target.Kind != evidenceExportTargetTaskID || result.Target.AttemptID != evidenceExportSealedAttemptID {
		t.Fatalf("sealed target = %#v", result.Target)
	}
	if result.Target.RuntimeTaskID != evidenceExportSealedRuntimeTask || result.Target.SelectionBasis != evidenceExportBasisSessionAssociation {
		t.Fatalf("sealed target binding = %#v", result.Target)
	}
	if result.RuntimeStatus != evidenceExportRuntimeAbsent || result.Coverage != evidenceExportCoverageAttemptOnly {
		t.Fatalf("sealed export result = %#v", result)
	}
	if result.AttemptSectionStatus != evidenceExportAttemptPresent {
		t.Fatalf("sealed attempt section status = %q", result.AttemptSectionStatus)
	}
	if export.Manifest.Attempt.RuntimeTaskID != evidenceExportSealedRuntimeTask || !export.Manifest.Attempt.RuntimeEvidence {
		t.Fatalf("sealed attempt section = %#v", export.Manifest.Attempt)
	}
	expectedEntries := []string{
		"attempt/seal.json",
		"attempt/session-association.json",
		"attempt/telemetry.jsonl",
		"attempt/events.jsonl",
		"attempt/rounds.jsonl",
		"attempt/state/worker.id",
		"attempt/transcripts/model/session-sealed-worker/0",
		"attempt/transcripts/parent/parent-thread-1/0",
		"attempt/transcripts/guardian/guardian-rollout-1",
	}
	for _, path := range expectedEntries {
		entry := exportEntryByPath(t, export.Manifest, path)
		if entry.Missing || entry.Unreadable != "" {
			t.Fatalf("sealed entry %s = %#v", path, entry)
		}
		if entry.CanonicalDigest == "" || entry.Basis == "" {
			t.Fatalf("sealed entry %s lacks canonical association: %#v", path, entry)
		}
		if entry.SHA256 != digestBytes(exportFileByPath(t, export, path)) {
			t.Fatalf("sealed entry %s digest mismatch", path)
		}
	}
	workerState := exportFileByPath(t, export, "attempt/state/worker.id")
	if string(workerState) != "session-sealed-worker\n" {
		t.Fatalf("sealed worker state bytes = %q", workerState)
	}
}

func TestExportEvidenceSealedAttemptProjectsParentEvidenceAggregate(t *testing.T) {
	export, _ := sealedRuntimeExportFixture(t)
	sealedLedger := exportEntryByPath(t, export.Manifest, "attempt/state/parent-evidence.aggregate.json")
	if sealedLedger.CanonicalDigest == "" || sealedLedger.OmittedPayload == "" {
		t.Fatalf("sealed parent evidence aggregate entry = %#v", sealedLedger)
	}
	var sealedAggregate evidenceParentEvidenceAggregate
	if err := json.Unmarshal(exportFileByPath(t, export, "attempt/state/parent-evidence.aggregate.json"), &sealedAggregate); err != nil {
		t.Fatal(err)
	}
	if sealedAggregate.Records != 6 || sealedAggregate.GroupCount != 4 || sealedAggregate.RepetitionsOmitted != 2 ||
		sealedAggregate.SourceSHA256 != sealedLedger.CanonicalDigest {
		t.Fatalf("sealed parent evidence aggregate = %#v", sealedAggregate)
	}
	for _, file := range export.Files {
		if file.Path == "attempt/state/parent-evidence.jsonl" {
			t.Fatal("sealed export embedded the raw parent evidence ledger")
		}
	}
}

func TestExportEvidenceSealedAttemptGitAuditStaysMetadataOnly(t *testing.T) {
	export, _ := sealedRuntimeExportFixture(t)
	if export.Manifest.Git == nil || len(export.Manifest.Git.Archives) != 1 {
		t.Fatalf("sealed git audit = %#v", export.Manifest.Git)
	}
	archiveMetadata := exportFileByPath(t, export, "attempt/git/archives/0.json")
	if bytes.Contains(archiveMetadata, []byte(`"pack":`)) {
		t.Fatal("sealed export embedded a git pack payload")
	}
	var metadata evidenceExportGitArchiveMetadata
	if err := json.Unmarshal(archiveMetadata, &metadata); err != nil {
		t.Fatal(err)
	}
	if metadata.PackDigest == "" || metadata.CanonicalDigest != export.Manifest.Git.Archives[0].Digest || metadata.OmittedPayload == "" {
		t.Fatalf("archive metadata = %#v", metadata)
	}
	for _, file := range export.Files {
		if file.Path == "sealed/task-bundle.json" {
			t.Fatal("sealed export kept the nested task bundle projection")
		}
	}
}

func TestExportEvidenceSealedAttemptAnalysisIndex(t *testing.T) {
	export, _ := sealedRuntimeExportFixture(t)
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	if len(analysis.CanonicalValidationRuns) != 0 || len(analysis.ObservedValidationActions) != 2 || len(analysis.Retries.ValidationReruns) != 1 {
		t.Fatalf("analysis validation runs = %#v observed = %#v retries = %#v", analysis.CanonicalValidationRuns, analysis.ObservedValidationActions, analysis.Retries)
	}
	for _, run := range analysis.ObservedValidationActions {
		if run.Basis != evidenceAnalysisBasisEvents || run.Log != "" {
			t.Fatalf("sealed validation event observation = %#v", run)
		}
	}
	if analysis.Sessions.Status != evidenceAnalysisStatusCollected || len(analysis.Sessions.Sessions) != 1 {
		t.Fatalf("analysis sessions = %#v", analysis.Sessions)
	}
	if analysis.Parent == nil || analysis.Parent.Status != evidenceAnalysisStatusCollected || analysis.Parent.ThreadID != "parent-thread-1" {
		t.Fatalf("analysis parent = %#v", analysis.Parent)
	}
	if len(analysis.Guardian) != 1 {
		t.Fatalf("analysis guardian = %#v", analysis.Guardian)
	}
	if analysis.ModelCalls.Status != evidenceAnalysisStatusCollected || analysis.ModelCalls.Total != 1 {
		t.Fatalf("analysis model calls = %#v", analysis.ModelCalls)
	}
	if analysis.ReviewRounds.Status != evidenceAnalysisStatusCollected || analysis.ReviewRounds.Count != 1 {
		t.Fatalf("analysis review rounds = %#v", analysis.ReviewRounds)
	}
}

func TestExportEvidenceRequiresRuntimeTaskIdentityForDefaultTargets(t *testing.T) {
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

func TestExportEvidenceTaskIDResolvesLiveRuntimeBinding(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	_, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: fixture.runtimeTask})
	if err != nil {
		t.Fatal(err)
	}
	if result.Target.Live != true || result.Target.SelectionBasis != evidenceExportBasisLiveBinding {
		t.Fatalf("live task id target = %#v", result.Target)
	}
}

func TestExportEvidenceTaskIDResolvesSurvivingRuntimeBinding(t *testing.T) {
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

func TestExportEvidenceDisclosesMissingValidationLogs(t *testing.T) {
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
