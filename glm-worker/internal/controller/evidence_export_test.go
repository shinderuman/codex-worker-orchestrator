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
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type evidenceExportFixture struct {
	store               *Store
	cfg                 config.AppConfig
	repo                string
	runtime             *state.StateStore
	runtimeTask         string
	seal                AttemptSeal
	sealRef             EvidenceObjectRef
	skipSealedEvidence  bool
	skipLiveAttempt     bool
	skipRuntimeEvidence bool
	ambiguousTaskPath   bool
}

const evidenceExportLiveAttemptID = "attempt-live-1"

const evidenceExportLiveLeaseID = "lease-live-1"

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
	sealRef, seal := storeBundleAttemptSeal(t, f.store, source, "attempt-b1", "IMPLEMENTATION_TASKS/B.md", "episode-1", 1)
	taskRef := storeBundleTaskRevision(t, f.store, seal.SemanticTaskRef, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	episodeRef := storeBundleEpisodeRevision(t, f.store, seal, taskRef, 1, 1, nil, []EvidenceObjectRef{sealRef}, nil)
	taskHeads := []EvidenceSubjectHead{{SubjectID: taskEvidenceSubjectID(seal.SemanticTaskRef), RevisionRef: taskRef}}
	if f.ambiguousTaskPath {
		otherRef, other := storeBundleAttemptSeal(t, f.store, source, "attempt-b2", "IMPLEMENTATION_TASKS/B.md", "", 1)
		otherTaskRef := storeBundleTaskRevision(t, f.store, other.SemanticTaskRef, 1, nil, []EvidenceObjectRef{otherRef}, nil)
		taskHeads = append(taskHeads, EvidenceSubjectHead{SubjectID: taskEvidenceSubjectID(other.SemanticTaskRef), RevisionRef: otherTaskRef})
	}
	publishBundleAuthority(t, f.store, 1, "snapshot-1", nil, nil,
		taskHeads,
		[]EvidenceSubjectHead{{SubjectID: seal.EpisodeID, RevisionRef: episodeRef}},
	)
	f.seal = seal
	f.sealRef = sealRef
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
	if err := f.store.writeHeadCAS(head.ControllerGeneration, next); err != nil {
		t.Fatal(err)
	}
}

func (f *evidenceExportFixture) runtimeRepoHash() string {
	return digestStrings(f.store.Identity().LineageID, f.seal.SemanticTaskRef.TaskPath)
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
	if err := os.WriteFile(runtime.TaskEventLogPath(runtimeTask), []byte(`{"version":1,"task_id":"`+runtimeTask+`","call_id":"c1","session_id":"session-worker","role":"worker","phase":"completed","seq":1,"timestamp":"2026-10-06T00:01:00Z","kind":"call"}`+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(runtime.ArtifactDir(runtimeTask), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime.ArtifactDir(runtimeTask), "report.txt"), []byte("artifact bytes\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	projects := filepath.Join(f.cfg.ClaudeConfigDir, "projects", "fixture")
	if err := os.MkdirAll(projects, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projects, "session-worker.jsonl"), []byte("{\"line\":1}\n"), 0o600); err != nil {
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

func TestExportEvidenceLiveAttemptCollectsSealedAndRawSections(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	objectsBefore := evidenceObjectTreeDigest(t, fixture.store)
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.LiveStatus != evidenceExportLiveCollected || result.Coverage != evidenceExportCoveragePartial {
		t.Fatalf("live export result = %#v", result)
	}
	if result.SealedStatus != evidenceExportSealedPresent {
		t.Fatalf("sealed section status = %q", result.SealedStatus)
	}
	if result.Target.Live != true || result.Target.AttemptID != evidenceExportLiveAttemptID {
		t.Fatalf("export target = %#v", result.Target)
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
	if transcript.SHA256 != digestBytes([]byte("{\"line\":1}\n")) {
		t.Fatalf("transcript entry = %#v", transcript)
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
	archiveEntry := exportEntryByPath(t, export.Manifest, "live/git/object-archive.json")
	if archiveEntry.SHA256 != export.Manifest.Git.ObjectArchiveDigest {
		t.Fatalf("git object archive entry = %#v", archiveEntry)
	}
	baseOID := controllerGitOutput(t, fixture.repo, "rev-parse", "HEAD")
	foundBase := false
	for _, root := range export.Manifest.Git.ObjectArchiveRoots {
		if root.OID == baseOID && root.Type == "commit" {
			foundBase = true
		}
	}
	if !foundBase {
		t.Fatalf("git object archive roots omit execution base: %#v", export.Manifest.Git.ObjectArchiveRoots)
	}
	if export.Manifest.Sealed.TaskBundle == nil || export.Manifest.Sealed.EpisodeBundle == nil {
		t.Fatalf("sealed summaries = %#v", export.Manifest.Sealed)
	}
	if len(export.Manifest.Live.Missing) != 1 || export.Manifest.Live.Missing[0] != "live/transcripts/claude/session-reviewer" {
		t.Fatalf("live missing list = %#v", export.Manifest.Live.Missing)
	}
	if evidenceObjectTreeDigest(t, fixture.store) != objectsBefore {
		t.Fatal("live evidence export mutated controller evidence objects")
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
	projection := exportFileByPath(t, export, "sealed/task-bundle.json")
	var bundle EvidenceBundleProjection
	if err := json.Unmarshal(projection, &bundle); err != nil {
		t.Fatal(err)
	}
	if bundle.Kind != evidenceBundleTask || bundleContainsRef(bundle, fixture.sealRef) != true {
		t.Fatalf("task projection = %#v", bundle)
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

func TestExportEvidenceSealedOnlyTargetAndFirstLiveAttempt(t *testing.T) {
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.skipLiveAttempt = true
	})
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskPath: fixture.seal.SemanticTaskRef.TaskPath})
	if err != nil {
		t.Fatal(err)
	}
	if result.LiveStatus != evidenceExportLiveAbsent || result.Coverage != evidenceExportCoverageSealedOnly {
		t.Fatalf("sealed-only export result = %#v", result)
	}
	if result.SealedStatus != evidenceExportSealedPresent || export.Manifest.Live.AbsenceReason == "" {
		t.Fatalf("sealed-only export manifest = %#v", export.Manifest.Live)
	}
	if export.Manifest.Git != nil {
		t.Fatal("sealed-only export collected live git observation")
	}
}

func TestExportEvidenceRecordsAbsentSealedRootForFirstLiveAttempt(t *testing.T) {
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.skipSealedEvidence = true
	})
	export, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.SealedStatus != evidenceExportSealedAbsent {
		t.Fatalf("first live attempt sealed status = %q", result.SealedStatus)
	}
	if result.LiveStatus != evidenceExportLiveCollected {
		t.Fatalf("first live attempt live status = %q", result.LiveStatus)
	}
	if export.Manifest.Sealed.AbsenceReason == "" {
		t.Fatalf("first live attempt sealed absence reason is empty")
	}
}

func TestExportEvidenceWithoutRuntimeRecordsLiveAbsence(t *testing.T) {
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.skipRuntimeEvidence = true
	})
	_, result, err := fixture.store.ExportEvidence(EvidenceExportRequest{})
	if err != nil {
		t.Fatal(err)
	}
	if result.LiveStatus != evidenceExportLiveAbsent {
		t.Fatalf("runtime-less live export status = %q", result.LiveStatus)
	}
}

func TestExportEvidenceRejectsUnknownAndAmbiguousTargets(t *testing.T) {
	fixture := newEvidenceExportFixture(t)
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskPath: "IMPLEMENTATION_TASKS/missing.md"}); err == nil {
		t.Fatal("export accepted an unknown task path")
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{AttemptID: "attempt-unknown"}); err == nil {
		t.Fatal("export accepted an unknown attempt id")
	}
	if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskPath: "a", AttemptID: "b"}); err == nil {
		t.Fatal("export accepted conflicting target fields")
	}

	ambiguous := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.ambiguousTaskPath = true
	})
	if _, _, err := ambiguous.store.ExportEvidence(EvidenceExportRequest{TaskPath: ambiguous.seal.SemanticTaskRef.TaskPath}); err == nil {
		t.Fatal("export accepted an ambiguous task path")
	}
	if _, _, err := ambiguous.store.ExportEvidence(EvidenceExportRequest{AttemptID: evidenceExportLiveAttemptID}); err != nil {
		t.Fatalf("ambiguous fixture rejected explicit live attempt export: %v", err)
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
