package controller

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type evidenceExportLaneFixture struct {
	*evidenceExportFixture
	primary      string
	laneRoot     string
	noncePath    string
	workspaceID  string
	laneHeadOID  string
	lanePlanByte []byte
}

const (
	evidenceExportLaneGateRunID    = "1234567890abcdef1234567890abcdef"
	evidenceExportPrimaryGateRunID = "fedcba0987654321fedcba0987654321"
)

func newEvidenceExportLaneFixture(t *testing.T, options ...func(*evidenceExportFixture)) *evidenceExportLaneFixture {
	t.Helper()
	fixture := newEvidenceExportFixture(t, options...)
	lane := &evidenceExportLaneFixture{evidenceExportFixture: fixture, primary: fixture.store.Identity().PrimaryRoot}
	laneRoot, err := fixture.store.executionLaneRoot()
	if err != nil {
		t.Fatal(err)
	}
	lane.laneRoot = laneRoot
	runControllerGit(t, fixture.repo, "worktree", "add", "--detach", laneRoot, laneAttemptBaseOID(t, fixture))
	lane.seedLaneWorkspace(t)
	lane.rebindLeaseToLaneWorkspace(t)
	lane.writeLaneValidationRuns(t)
	if err := os.WriteFile(fixture.runtime.Path("lock"), []byte("runtime lock holder\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return lane
}

func laneAttemptBaseOID(t *testing.T, fixture *evidenceExportFixture) string {
	t.Helper()
	attempt, err := fixture.store.loadAttempt(evidenceExportLiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	return attempt.ExecutionBaseOID
}

func (l *evidenceExportLaneFixture) seedLaneWorkspace(t *testing.T) {
	t.Helper()
	l.laneHeadOID = controllerGitOutput(t, l.laneRoot, "rev-parse", "HEAD")
	l.lanePlanByte = []byte("# Lane plan\n\nlane workspace is the evidence source\n")
	laneFiles := []struct {
		name string
		data []byte
	}{
		{"IMPLEMENTATION_PLAN.local.md", l.lanePlanByte},
		{"IMPLEMENTATION_RULES.md", []byte("# Lane rules\n")},
		{"lane-untracked.txt", []byte("lane untracked bytes\n")},
		{"source.txt", []byte("lane-staged\n")},
	}
	for _, file := range laneFiles {
		if err := os.WriteFile(filepath.Join(l.laneRoot, file.name), file.data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	runControllerGit(t, l.laneRoot, "add", "source.txt")
}

func (l *evidenceExportLaneFixture) writeLaneNonce(t *testing.T, id string) {
	t.Helper()
	identity, err := ResolveWorkspaceIdentity(l.laneRoot, l.store.Identity())
	if err != nil {
		t.Fatal(err)
	}
	if l.noncePath == "" {
		l.noncePath = filepath.Join(identity.GitDir, executionWorkspaceIdentityFile)
	}
	identity.ID = id
	data, err := json.Marshal(identity)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(l.noncePath, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (l *evidenceExportLaneFixture) rebindLeaseToLaneWorkspace(t *testing.T) {
	t.Helper()
	id, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	l.workspaceID = id
	l.writeLaneNonce(t, id)
	lease, err := l.store.loadLease(evidenceExportLiveLeaseID)
	if err != nil {
		t.Fatal(err)
	}
	lease.WorkspaceID = id
	if err := l.store.writeLease(lease); err != nil {
		t.Fatal(err)
	}
}

func (l *evidenceExportLaneFixture) writeLaneValidationRuns(t *testing.T) {
	t.Helper()
	started := time.Unix(6100, 0).UTC()
	completed := time.Unix(6200, 0).UTC()
	laneRun := qualitygate.RunRecord{
		ValidationRunID: evidenceExportLaneGateRunID, Form: "go-test",
		Repository: l.laneRoot, WorkingDir: filepath.Join(l.laneRoot, "glm-worker"),
		StartedAt: started, CompletedAt: &completed, Status: qualitygate.StatusPass,
	}
	l.writeModuleValidationRunAt(t, l.laneRoot, laneRun)
	primaryRun := laneRun
	primaryRun.ValidationRunID = evidenceExportPrimaryGateRunID
	primaryRun.Repository = l.primary
	primaryRun.WorkingDir = filepath.Join(l.primary, "glm-worker")
	l.writeModuleValidationRunAt(t, l.primary, primaryRun)
}

func (f *evidenceExportFixture) writeModuleValidationRunAt(t *testing.T, repoRoot string, record qualitygate.RunRecord) {
	t.Helper()
	canonical, err := canonicalPath(repoRoot)
	if err != nil {
		t.Fatal(err)
	}
	module := state.AttachStateStore(config.AppConfig{StateBase: f.cfg.StateBase, RepoHash: config.RepoHashFor(canonical)})
	if err := module.Write("repo-root", canonical); err != nil {
		t.Fatal(err)
	}
	writeEvidenceGateRun(t, module, record)
}

func (l *evidenceExportLaneFixture) exportFrom(t *testing.T, root string, request EvidenceExportRequest) (EvidenceExport, EvidenceExportResult) {
	t.Helper()
	cfg := controllerTestConfig(root, l.cfg.StateBase)
	cfg.ClaudeConfigDir = l.cfg.ClaudeConfigDir
	cfg.CodexConfigDir = l.cfg.CodexConfigDir
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	export, result, err := store.ExportEvidence(request)
	if err != nil {
		t.Fatalf("export from %s: %v", root, err)
	}
	return export, result
}

func TestExportEvidencePrimaryCallerReadsLaneWorkspace(t *testing.T) {
	lane := newEvidenceExportLaneFixture(t)
	objectsBefore := evidenceObjectTreeDigest(t, lane.store)
	headBefore, err := lane.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	export, result := lane.exportFrom(t, lane.primary, EvidenceExportRequest{})
	if !result.Target.Live || result.Target.AttemptID != evidenceExportLiveAttemptID {
		t.Fatalf("primary-caller target = %#v", result.Target)
	}
	if result.RuntimeStatus != evidenceExportRuntimeCollected {
		t.Fatalf("primary-caller runtime status = %#v", result)
	}
	git := export.Manifest.Git
	if git == nil || git.WorkspaceID != lane.workspaceID || git.HeadOID != lane.laneHeadOID || git.Snapshot == nil {
		t.Fatalf("lane git audit = %#v", git)
	}
	staged := exportFileByPath(t, export, "live/git/diff-staged.patch")
	if !bytes.Contains(staged, []byte("lane-staged")) || bytes.Contains(staged, []byte("base-staged")) {
		t.Fatalf("staged diff did not come from the lane workspace: %s", staged)
	}
	untracked := exportEntryByPath(t, export.Manifest, "live/git/untracked/lane-untracked.txt")
	if untracked.SHA256 != digestBytes([]byte("lane untracked bytes\n")) {
		t.Fatalf("lane untracked entry = %#v", untracked)
	}
	if exportHasEntry(export.Manifest, "live/git/untracked/untracked-note.txt") {
		t.Fatal("primary caller exported the caller checkout's untracked files")
	}
	if got := exportFileByPath(t, export, "live/"+evidenceExportSnapshotEntryDir+"/"+evidenceExportInstructionPlanPath); !bytes.Equal(got, lane.lanePlanByte) {
		t.Fatalf("plan snapshot bytes = %q want lane workspace copy", got)
	}
	laneRun := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/"+evidenceExportLaneGateRunID+"/run.json")
	if laneRun.Basis != evidenceExportBasisRepositoryWorkspace || laneRun.Missing {
		t.Fatalf("lane validation run entry = %#v", laneRun)
	}
	if exportHasEntry(export.Manifest, "live/validation/quality-gate/"+evidenceExportPrimaryGateRunID+"/run.json") {
		t.Fatal("export attributed the caller checkout's validation run to the lane attempt")
	}
	if exportHasEntry(export.Manifest, "live/state/lock") {
		t.Fatal("export leaked the active runtime lock holder file")
	}
	if export.Manifest.Runtime.WorkspaceID != lane.workspaceID {
		t.Fatalf("runtime section workspace = %#v", export.Manifest.Runtime)
	}
	if export.Manifest.Runtime.Mode != evidenceExportRuntimeModeLive {
		t.Fatalf("runtime section = %#v", export.Manifest.Runtime)
	}
	lane.exportFrom(t, lane.laneRoot, EvidenceExportRequest{})
	headAfter, err := lane.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if observation := evidenceExportControllerObservation(headBefore, headAfter); observation.AuthorityChangedDuringExport ||
		headAfter.EvidenceLedgerSequence != headBefore.EvidenceLedgerSequence {
		t.Fatalf("export mutated controller authority: %#v -> %#v", headBefore, headAfter)
	}
	if evidenceObjectTreeDigest(t, lane.store) != objectsBefore {
		t.Fatal("export mutated controller evidence objects")
	}
}

func TestExportEvidencePrimaryCallerTaskIDSelectsLiveLaneAttempt(t *testing.T) {
	lane := newEvidenceExportLaneFixture(t)
	export, result := lane.exportFrom(t, lane.primary, EvidenceExportRequest{TaskID: lane.runtimeTask})
	if !result.Target.Live || result.Target.SelectionBasis != evidenceExportBasisLiveBinding {
		t.Fatalf("task-id target = %#v", result.Target)
	}
	if export.Manifest.Git == nil || export.Manifest.Git.WorkspaceID != lane.workspaceID {
		t.Fatalf("task-id git audit = %#v", export.Manifest.Git)
	}
}

func TestExportEvidencePrimaryCallerStoppedAttemptKeepsLaneBinding(t *testing.T) {
	lane := newEvidenceExportLaneFixture(t, func(f *evidenceExportFixture) {
		f.skipLiveHead = true
	})
	export, result := lane.exportFrom(t, lane.primary, EvidenceExportRequest{TaskID: lane.runtimeTask})
	if result.Target.Live || result.Target.SelectionBasis != evidenceExportBasisRuntimeBinding {
		t.Fatalf("stopped target = %#v", result.Target)
	}
	if result.RuntimeStatus != evidenceExportRuntimeCollected || export.Manifest.Runtime.Mode != evidenceExportRuntimeModeBound {
		t.Fatalf("stopped runtime section = %#v", export.Manifest.Runtime)
	}
	if result.Coverage != evidenceExportCoveragePartial {
		t.Fatalf("stopped coverage = %q", result.Coverage)
	}
	boundRun := exportEntryByPath(t, export.Manifest, "bound/validation/quality-gate/"+evidenceExportLaneGateRunID+"/run.json")
	if boundRun.Basis != evidenceExportBasisRepositoryWorkspace || boundRun.Missing {
		t.Fatalf("stopped lane validation run entry = %#v", boundRun)
	}
	if exportHasEntry(export.Manifest, "bound/validation/quality-gate/"+evidenceExportPrimaryGateRunID+"/run.json") {
		t.Fatal("stopped export attributed the caller checkout's validation run to the lane attempt")
	}
}

func TestExportEvidencePrimaryCallerCompletedAttemptWithoutLane(t *testing.T) {
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.skipLiveAttempt = true
		f.skipRuntimeEvidence = true
	})
	lane := &evidenceExportLaneFixture{evidenceExportFixture: fixture, primary: fixture.store.Identity().PrimaryRoot}
	export, result := lane.exportFrom(t, lane.primary, EvidenceExportRequest{TaskID: evidenceExportSealedRuntimeTask})
	if result.Target.SelectionBasis != evidenceExportBasisSessionAssociation || result.Target.AttemptID != evidenceExportSealedAttemptID {
		t.Fatalf("completed target = %#v", result.Target)
	}
	if result.AttemptSectionStatus != evidenceExportAttemptPresent || result.Coverage != evidenceExportCoverageAttemptOnly {
		t.Fatalf("completed result = %#v", result)
	}
	if result.RuntimeStatus != evidenceExportRuntimeAbsent {
		t.Fatalf("completed runtime status = %q", result.RuntimeStatus)
	}
	if !export.Manifest.Attempt.RuntimeEvidence {
		t.Fatalf("completed attempt section = %#v", export.Manifest.Attempt)
	}
	if export.Manifest.Git == nil || len(export.Manifest.Git.Archives) != 1 {
		t.Fatalf("completed git audit = %#v", export.Manifest.Git)
	}
}

func TestExportEvidenceRejectsTamperedWorkspaceBinding(t *testing.T) {
	t.Run("nonce identity no longer matches the lease", func(t *testing.T) {
		lane := newEvidenceExportLaneFixture(t)
		other, err := state.NewUUID()
		if err != nil {
			t.Fatal(err)
		}
		lane.writeLaneNonce(t, other)
		if _, _, err := lane.exportStore(t, lane.primary).ExportEvidence(EvidenceExportRequest{}); err == nil {
			t.Fatal("export accepted a lane nonce that no longer matches the attempt lease")
		}
	})
	t.Run("lease workspace points outside canonical bindings", func(t *testing.T) {
		lane := newEvidenceExportLaneFixture(t)
		lease, err := lane.store.loadLease(evidenceExportLiveLeaseID)
		if err != nil {
			t.Fatal(err)
		}
		lease.WorkspaceID = "workspace-other"
		if err := lane.store.writeLease(lease); err != nil {
			t.Fatal(err)
		}
		if _, _, err := lane.exportStore(t, lane.laneRoot).ExportEvidence(EvidenceExportRequest{}); err == nil {
			t.Fatal("export accepted a lease bound to an unknown workspace")
		}
	})
	t.Run("plural lease history does not block a stopped export", func(t *testing.T) {
		lane := newEvidenceExportLaneFixture(t, func(f *evidenceExportFixture) {
			f.skipLiveHead = true
		})
		lease, err := lane.store.loadLease(evidenceExportLiveLeaseID)
		if err != nil {
			t.Fatal(err)
		}
		lease.LeaseID = "lease-duplicate-1"
		if err := lane.store.writeLease(lease); err != nil {
			t.Fatal(err)
		}
		export, result := lane.exportFrom(t, lane.primary, EvidenceExportRequest{TaskID: lane.runtimeTask})
		if result.RuntimeStatus != evidenceExportRuntimeCollected {
			t.Fatalf("stopped export with plural lease history = %#v", result)
		}
		if exportHasEntry(export.Manifest, "bound/validation/quality-gate/"+evidenceExportLaneGateRunID+"/run.json") {
			t.Fatal("stopped export claimed validation runs without a canonical workspace selector")
		}
	})
}

func (l *evidenceExportLaneFixture) exportStore(t *testing.T, root string) *Store {
	t.Helper()
	cfg := controllerTestConfig(root, l.cfg.StateBase)
	cfg.ClaudeConfigDir = l.cfg.ClaudeConfigDir
	cfg.CodexConfigDir = l.cfg.CodexConfigDir
	store, err := Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
