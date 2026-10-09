package controller

import (
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

const evidenceExportOlderAttemptID = "attempt-b0"

func (f *evidenceExportFixture) continueLiveAttemptFromSeal(t *testing.T) {
	t.Helper()
	f.rewriteLiveContinuation(t, evidenceExportSealedAttemptID, f.sealRef.LogicalIdentity)
}

func (f *evidenceExportFixture) rewriteLiveContinuation(t *testing.T, predecessorID, resumedFromSealID string) {
	t.Helper()
	attempt, err := f.store.loadAttempt(evidenceExportLiveAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	attempt.PredecessorAttemptID = predecessorID
	attempt.ResumedFromSealID = resumedFromSealID
	if err := f.store.writeAttempt(attempt); err != nil {
		t.Fatal(err)
	}
}

func (f *evidenceExportFixture) rewriteSealedAttempt(t *testing.T, mutate func(*AttemptRecord)) {
	t.Helper()
	attempt, err := f.store.loadAttempt(evidenceExportSealedAttemptID)
	if err != nil {
		t.Fatal(err)
	}
	mutate(&attempt)
	if err := f.store.writeAttempt(attempt); err != nil {
		t.Fatal(err)
	}
}

func decodeAnalysisIndex(t *testing.T, export EvidenceExport) EvidenceAnalysisIndex {
	t.Helper()
	var analysis EvidenceAnalysisIndex
	if err := json.Unmarshal(exportFileByPath(t, export, "analysis-index.json"), &analysis); err != nil {
		t.Fatal(err)
	}
	return analysis
}

func TestExportEvidenceTaskContinuationIncludesPredecessorSeal(t *testing.T) {
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.sealedAssociationRuntimeTask = "runtime-task-uuid-1"
	})
	fixture.continueLiveAttemptFromSeal(t)
	objectsBefore := evidenceObjectTreeDigest(t, fixture.store)

	export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
	if !result.Target.Live || result.Target.AttemptID != evidenceExportLiveAttemptID {
		t.Fatalf("continuation target = %#v", result.Target)
	}
	if export.Manifest.Attempt.Status != evidenceExportAttemptAbsent || export.Manifest.Attempt.RuntimeEvidence {
		t.Fatalf("current attempt section = %#v", export.Manifest.Attempt)
	}
	predecessors := export.Manifest.Attempt.Predecessors
	if len(predecessors) != 1 {
		t.Fatalf("predecessor sections = %#v", predecessors)
	}
	predecessor := predecessors[0]
	if predecessor.AttemptID != evidenceExportSealedAttemptID || predecessor.Status != evidenceExportPredecessorCollected ||
		predecessor.SealDigest != fixture.sealRef.Digest || predecessor.Relation != evidenceExportPredecessorRelation ||
		predecessor.EntriesPrefix != evidenceExportPredecessorRoot+"/"+evidenceExportSealedAttemptID ||
		predecessor.Task.TaskPath != fixture.seal.SemanticTaskRef.TaskPath ||
		predecessor.RuntimeTaskID != fixture.runtimeTask {
		t.Fatalf("predecessor section = %#v", predecessor)
	}
	if predecessor.Window == nil || !predecessor.Window.Start.Equal(fixture.seal.StartedAt) ||
		!predecessor.Window.End.Equal(fixture.seal.SealedAt) || predecessor.Window.EndBasis != evidenceExportBasisAttemptSeal {
		t.Fatalf("predecessor window = %#v", predecessor.Window)
	}
	sealEntry := exportEntryByPath(t, export.Manifest, "predecessors/"+evidenceExportSealedAttemptID+"/seal.json")
	if sealEntry.Basis != evidenceExportBasisPredecessorSeal || sealEntry.CanonicalDigest != fixture.sealRef.Digest {
		t.Fatalf("predecessor seal entry = %#v", sealEntry)
	}
	for _, path := range []string{
		"predecessors/" + evidenceExportSealedAttemptID + "/session-association.json",
		"predecessors/" + evidenceExportSealedAttemptID + "/telemetry.jsonl",
		"predecessors/" + evidenceExportSealedAttemptID + "/state/worker.id",
	} {
		entry := exportEntryByPath(t, export.Manifest, path)
		if entry.Missing || entry.Unreadable != "" || entry.Basis != evidenceExportBasisPredecessorSeal {
			t.Fatalf("predecessor entry %s = %#v", path, entry)
		}
	}
	if exportHasEntry(export.Manifest, "attempt/state/worker.id") {
		t.Fatal("predecessor state leaked into the current attempt section")
	}
	if !export.Manifest.Runtime.Window.Start.Equal(time.Unix(6000, 0).UTC()) {
		t.Fatalf("current window was widened to the predecessor: %#v", export.Manifest.Runtime.Window)
	}
	analysis := decodeAnalysisIndex(t, export)
	if len(analysis.Predecessors) != 1 || analysis.Predecessors[0].Status != evidenceExportPredecessorCollected ||
		analysis.Predecessors[0].RuntimeTaskID != fixture.runtimeTask || analysis.Predecessors[0].EntriesPrefix != predecessor.EntriesPrefix {
		t.Fatalf("analysis predecessors = %#v", analysis.Predecessors)
	}
	for _, session := range analysis.Sessions.Sessions {
		if session.SessionID == "session-sealed-worker" {
			t.Fatalf("predecessor session merged into current sessions: %#v", analysis.Sessions.Sessions)
		}
	}
	if evidenceObjectTreeDigest(t, fixture.store) != objectsBefore {
		t.Fatal("predecessor projection mutated controller evidence objects")
	}
}

func TestExportEvidenceTaskContinuationFollowsMultiHopLineage(t *testing.T) {
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.sealedAssociationRuntimeTask = "runtime-task-uuid-1"
		f.withOlderSeal = true
		f.olderAssociationRuntimeTask = "runtime-task-uuid-1"
	})
	fixture.continueLiveAttemptFromSeal(t)

	export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
	predecessors := export.Manifest.Attempt.Predecessors
	if len(predecessors) != 2 {
		t.Fatalf("multi-hop predecessors = %#v", predecessors)
	}
	requireCollectedPredecessor(t, export, evidenceExportSealedAttemptID, "runtime-task-uuid-1", fixture.sealRef.Digest)
	older := requireCollectedPredecessor(t, export, evidenceExportOlderAttemptID, "runtime-task-uuid-1", fixture.olderSealRef.Digest)
	if !older.Window.Start.Equal(fixture.olderSeal.StartedAt) || !older.Window.End.Equal(fixture.olderSeal.SealedAt) {
		t.Fatalf("older predecessor window = %#v", older.Window)
	}
}

func TestExportEvidenceTaskContinuationVerifiesRuntimeTaskEqually(t *testing.T) {
	aligned := "runtime-task-uuid-1"
	liveOptions := func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.sealedAssociationRuntimeTask = aligned
	}
	t.Run("live task-id export", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, liveOptions)
		fixture.continueLiveAttemptFromSeal(t)
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		requireCollectedPredecessor(t, export, evidenceExportSealedAttemptID, aligned, fixture.sealRef.Digest)
	})
	t.Run("live no-arg export", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, liveOptions)
		fixture.continueLiveAttemptFromSeal(t)
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{})
		requireCollectedPredecessor(t, export, evidenceExportSealedAttemptID, aligned, fixture.sealRef.Digest)
	})
	t.Run("completed recent no-arg export", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			liveOptions(f)
			f.skipLiveAttempt = true
			f.withOlderSeal = true
			f.olderAssociationRuntimeTask = aligned
		})
		export, result := fixture.exportContinuation(t, EvidenceExportRequest{})
		if result.Target.SelectionBasis != evidenceExportBasisExecutionTimeline || result.Target.AttemptID != evidenceExportSealedAttemptID {
			t.Fatalf("recent target = %#v", result.Target)
		}
		requireCollectedPredecessor(t, export, evidenceExportOlderAttemptID, aligned, fixture.olderSealRef.Digest)
	})
}

func TestExportEvidenceTaskContinuationRequiresPredecessorRuntimeAssociation(t *testing.T) {
	aligned := "runtime-task-uuid-1"
	recentOptions := func(older func(*evidenceExportFixture)) func(*evidenceExportFixture) {
		return func(f *evidenceExportFixture) {
			f.withSealedRuntime = true
			f.sealedAssociationRuntimeTask = aligned
			f.skipLiveAttempt = true
			f.withOlderSeal = true
			older(f)
		}
	}
	t.Run("predecessor association absent", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, recentOptions(func(*evidenceExportFixture) {}))
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{})
		requireUnverifiedPredecessor(t, export, evidenceExportOlderAttemptID)
	})
	t.Run("predecessor association runtime task unknown", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, recentOptions(func(f *evidenceExportFixture) {
			f.olderAssociationWithoutRuntimeTask = true
		}))
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{})
		requireUnverifiedPredecessor(t, export, evidenceExportOlderAttemptID)
	})
	t.Run("predecessor binds different runtime task", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, recentOptions(func(f *evidenceExportFixture) {
			f.olderAssociationRuntimeTask = "runtime-task-other"
		}))
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{})
		requireUnverifiedPredecessor(t, export, evidenceExportOlderAttemptID)
	})
}

func TestExportEvidenceSelectsNewestAttemptOfTaskAndNamesArchiveByRuntimeTask(t *testing.T) {
	aligned := "runtime-task-uuid-1"
	options := func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.sealedAssociationRuntimeTask = aligned
		f.skipLiveAttempt = true
		f.withOlderSeal = true
		f.olderAssociationRuntimeTask = aligned
	}
	t.Run("task-id export with multiple attempts of the task", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, options)
		export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: aligned})
		if result.Target.AttemptID != evidenceExportSealedAttemptID || result.Target.RuntimeTaskID != aligned {
			t.Fatalf("task-id target = %#v", result.Target)
		}
		if result.ArchiveName != aligned+".zip" {
			t.Fatalf("archive name = %q", result.ArchiveName)
		}
		requireCollectedPredecessor(t, export, evidenceExportOlderAttemptID, aligned, fixture.olderSealRef.Digest)
	})
	t.Run("no-arg recent export resolves the runtime task identity", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			options(f)
			f.sealedWorkspaceFromRepo = true
		})
		historyLease := ExecutionLease{
			SchemaVersion: controllerSchemaVersion, LeaseID: "lease-b1-history",
			AttemptID: evidenceExportSealedAttemptID, SemanticTaskRef: fixture.seal.SemanticTaskRef,
			Purpose: "blocker-execution", ControllerGeneration: fixture.seal.ControllerGeneration,
			WorkspaceID: "workspace-legacy", EpisodeID: fixture.seal.EpisodeID,
		}
		if err := fixture.store.writeLease(historyLease); err != nil {
			t.Fatal(err)
		}
		repoRoot, err := canonicalPath(fixture.repo)
		if err != nil {
			t.Fatal(err)
		}
		completedAt := fixture.olderSeal.StartedAt.Add(2 * time.Minute)
		olderWindowRun := qualitygate.RunRecord{
			ValidationRunID: "12121212121212121212121212121212", Form: "go-test",
			Repository: repoRoot, WorkingDir: filepath.Join(repoRoot, "glm-worker"),
			StartedAt: fixture.olderSeal.StartedAt.Add(time.Minute), CompletedAt: &completedAt,
			Status: qualitygate.StatusPass,
		}
		fixture.writeModuleValidationRun(t, olderWindowRun)
		export, result := fixture.exportContinuation(t, EvidenceExportRequest{})
		if result.Target.SelectionBasis != evidenceExportBasisExecutionTimeline || result.Target.AttemptID != evidenceExportSealedAttemptID ||
			result.Target.RuntimeTaskID != aligned {
			t.Fatalf("recent target = %#v", result.Target)
		}
		if result.ArchiveName != aligned+".zip" {
			t.Fatalf("archive name = %q", result.ArchiveName)
		}
		requireCollectedPredecessor(t, export, evidenceExportOlderAttemptID, aligned, fixture.olderSealRef.Digest)
		olderRun := exportEntryByPath(t, export.Manifest,
			"predecessors/"+evidenceExportOlderAttemptID+"/state/quality-gate-runs/"+olderWindowRun.ValidationRunID+"/run.json")
		if olderRun.Missing || olderRun.Unreadable != "" || olderRun.Basis != evidenceExportBasisRepositoryWorkspace {
			t.Fatalf("seal-derived workspace validation run entry = %#v", olderRun)
		}
	})
}

func TestEvidenceExportTaskIDTargetMatchRejectsCrossTaskAmbiguity(t *testing.T) {
	match := func(attemptID, taskPath string, createdAt time.Time) evidenceExportTaskMatch {
		return evidenceExportTaskMatch{attempt: AttemptRecord{
			AttemptID: attemptID, SemanticTaskRef: SemanticTaskRef{TaskPath: taskPath}, CreatedAt: createdAt,
		}}
	}
	sameTask := []evidenceExportTaskMatch{
		match("attempt-a", "IMPLEMENTATION_TASKS/B.md", time.Unix(1, 0)),
		match("attempt-b", "IMPLEMENTATION_TASKS/B.md", time.Unix(2, 0)),
	}
	selected, err := evidenceExportTaskIDTargetMatch("runtime-task-uuid-1", sameTask)
	if err != nil || selected.attempt.AttemptID != "attempt-b" {
		t.Fatalf("same-task selection = %#v err = %v", selected.attempt, err)
	}
	crossTask := []evidenceExportTaskMatch{sameTask[0], sameTask[1],
		match("attempt-c", "IMPLEMENTATION_TASKS/other.md", time.Unix(3, 0))}
	if _, err := evidenceExportTaskIDTargetMatch("runtime-task-uuid-1", crossTask); err == nil {
		t.Fatal("task-id target selection accepted attempts of different tasks")
	}
}

func requireCollectedPredecessor(t *testing.T, export EvidenceExport, attemptID, runtimeTaskID, sealDigest string) EvidenceExportPredecessorSection {
	t.Helper()
	for _, section := range export.Manifest.Attempt.Predecessors {
		if section.AttemptID != attemptID {
			continue
		}
		if section.Status != evidenceExportPredecessorCollected || section.SealDigest != sealDigest ||
			section.RuntimeTaskID != runtimeTaskID || section.Window == nil || section.Problem != "" {
			t.Fatalf("collected predecessor section = %#v", section)
		}
		if !exportHasEntry(export.Manifest, section.EntriesPrefix+"/seal.json") {
			t.Fatalf("collected predecessor %s has no projected seal", attemptID)
		}
		return section
	}
	t.Fatalf("predecessor section %s is missing", attemptID)
	return EvidenceExportPredecessorSection{}
}

func TestExportEvidenceTaskContinuationDisclosesBrokenLineage(t *testing.T) {
	t.Run("unknown predecessor record", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t)
		fixture.rewriteLiveContinuation(t, "attempt-ghost", fixture.sealRef.LogicalIdentity)
		export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		section := requireUnverifiedPredecessor(t, export, "attempt-ghost")
		if section.Task.TaskPath != "" {
			t.Fatalf("unresolved predecessor claimed a task ref: %#v", section)
		}
		if result.RuntimeStatus != evidenceExportRuntimeCollected {
			t.Fatalf("broken lineage lost current evidence: %#v", result)
		}
	})
	t.Run("resumed-from identity mismatch", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t)
		fixture.rewriteLiveContinuation(t, evidenceExportSealedAttemptID, "wrong-seal-identity")
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		section := requireUnverifiedPredecessor(t, export, evidenceExportSealedAttemptID)
		if section.Task.TaskPath != fixture.seal.SemanticTaskRef.TaskPath {
			t.Fatalf("mismatched predecessor section = %#v", section)
		}
	})
	t.Run("different task path", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t)
		fixture.rewriteSealedAttempt(t, func(attempt *AttemptRecord) {
			attempt.SemanticTaskRef.TaskPath = "IMPLEMENTATION_TASKS/other.md"
		})
		fixture.continueLiveAttemptFromSeal(t)
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		section := requireUnverifiedPredecessor(t, export, evidenceExportSealedAttemptID)
		if section.Task.TaskPath != "IMPLEMENTATION_TASKS/other.md" {
			t.Fatalf("foreign-task predecessor section = %#v", section)
		}
	})
	t.Run("different runtime task", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.withSealedRuntime = true
		})
		fixture.continueLiveAttemptFromSeal(t)
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		requireUnverifiedPredecessor(t, export, evidenceExportSealedAttemptID)
	})
	t.Run("cyclic chain stops with disclosure", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.withSealedRuntime = true
			f.sealedAssociationRuntimeTask = "runtime-task-uuid-1"
		})
		fixture.continueLiveAttemptFromSeal(t)
		fixture.rewriteSealedAttempt(t, func(attempt *AttemptRecord) {
			attempt.PredecessorAttemptID = evidenceExportLiveAttemptID
			attempt.ResumedFromSealID = fixture.sealRef.LogicalIdentity
		})
		export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		predecessors := export.Manifest.Attempt.Predecessors
		if len(predecessors) != 2 || predecessors[0].Status != evidenceExportPredecessorCollected ||
			predecessors[0].SealDigest != fixture.sealRef.Digest {
			t.Fatalf("cyclic chain predecessors = %#v", predecessors)
		}
		requireUnverifiedPredecessor(t, export, evidenceExportLiveAttemptID)
		if result.RuntimeStatus != evidenceExportRuntimeCollected {
			t.Fatalf("cyclic chain lost current evidence: %#v", result)
		}
	})
}

func predecessorValidationRun(id string, start, end time.Time, repository string) qualitygate.RunRecord {
	return qualitygate.RunRecord{
		ValidationRunID: id, Form: "go-test",
		Repository: repository, WorkingDir: filepath.Join(repository, "glm-worker"),
		StartedAt: start, CompletedAt: &end, Status: qualitygate.StatusPass,
	}
}

func predecessorValidationFixture(t *testing.T, build func(fixture *evidenceExportFixture, repoRoot string) []qualitygate.RunRecord) (*evidenceExportFixture, string) {
	t.Helper()
	fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
		f.withSealedRuntime = true
		f.sealedAssociationRuntimeTask = "runtime-task-uuid-1"
	})
	fixture.continueLiveAttemptFromSeal(t)
	repoRoot, err := canonicalPath(fixture.repo)
	if err != nil {
		t.Fatal(err)
	}
	for _, record := range build(fixture, repoRoot) {
		fixture.writeModuleValidationRun(t, record)
	}
	return fixture, repoRoot
}

func TestExportEvidenceTaskContinuationIncludesPredecessorValidationRuns(t *testing.T) {
	fixture, _ := predecessorValidationFixture(t, func(fixture *evidenceExportFixture, repoRoot string) []qualitygate.RunRecord {
		return []qualitygate.RunRecord{
			predecessorValidationRun("77777777777777777777777777777777",
				fixture.seal.StartedAt.Add(time.Minute), fixture.seal.StartedAt.Add(2*time.Minute), repoRoot),
			predecessorValidationRun("88888888888888888888888888888888",
				fixture.seal.StartedAt.Add(-2*time.Hour), fixture.seal.StartedAt.Add(-time.Hour), repoRoot),
			predecessorValidationRun("99999999999999999999999999999999",
				fixture.seal.StartedAt.Add(time.Minute), fixture.seal.StartedAt.Add(2*time.Minute), t.TempDir()),
		}
	})
	inWindow := "77777777777777777777777777777777"
	stale := "88888888888888888888888888888888"
	foreign := "99999999999999999999999999999999"

	export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
	if result.RuntimeStatus != evidenceExportRuntimeCollected {
		t.Fatalf("validation fill lost current evidence: %#v", result)
	}
	requireCollectedPredecessor(t, export, evidenceExportSealedAttemptID, "runtime-task-uuid-1", fixture.sealRef.Digest)
	prefix := "predecessors/" + evidenceExportSealedAttemptID + "/state/quality-gate-runs/" + inWindow
	runEntry := exportEntryByPath(t, export.Manifest, prefix+"/run.json")
	if runEntry.Basis != evidenceExportBasisRepositoryWorkspace || runEntry.Source != evidenceExportSourceGateRun ||
		runEntry.Missing || runEntry.Unreadable != "" {
		t.Fatalf("predecessor validation run entry = %#v", runEntry)
	}
	if runEntry.SHA256 != digestBytes(exportFileByPath(t, export, prefix+"/run.json")) {
		t.Fatal("predecessor validation run bytes do not match its entry digest")
	}
	logEntry := exportEntryByPath(t, export.Manifest, prefix+"/gate.log")
	if logEntry.Missing || logEntry.Unreadable != "" || logEntry.SHA256 != digestBytes(exportFileByPath(t, export, prefix+"/gate.log")) {
		t.Fatalf("predecessor validation log entry = %#v", logEntry)
	}
	for _, excluded := range []string{stale, foreign} {
		if exportHasEntry(export.Manifest, "predecessors/"+evidenceExportSealedAttemptID+"/state/quality-gate-runs/"+excluded+"/run.json") {
			t.Fatalf("predecessor window absorbed unrelated run %s", excluded)
		}
	}
	if exportHasEntry(export.Manifest, "live/validation/quality-gate/"+inWindow+"/run.json") {
		t.Fatal("predecessor-window run leaked into the current live section")
	}
	analysis := decodeAnalysisIndex(t, export)
	if !analysisValidationRunPresent(analysis.CanonicalValidationRuns, inWindow, evidenceExportBasisRepositoryWorkspace) {
		t.Fatalf("analysis lost the predecessor validation run: %#v", analysis.CanonicalValidationRuns)
	}
	for _, entry := range analysis.CanonicalValidationRuns {
		if entry.RunID == inWindow && entry.Log != prefix+"/gate.log" {
			t.Fatalf("predecessor validation run analysis relation = %#v", entry)
		}
	}
}

func TestExportEvidenceTaskContinuationBindsTaskIdentifiedRunsByWindow(t *testing.T) {
	fixture, _ := predecessorValidationFixture(t, func(fixture *evidenceExportFixture, repoRoot string) []qualitygate.RunRecord {
		inWindow := predecessorValidationRun("aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			fixture.seal.StartedAt.Add(time.Minute), fixture.seal.StartedAt.Add(2*time.Minute), repoRoot)
		inWindow.TaskID = "runtime-task-uuid-1"
		currentEnd := time.Unix(6200, 0).UTC()
		current := predecessorValidationRun("cccccccccccccccccccccccccccccccc", time.Unix(6100, 0).UTC(), currentEnd, repoRoot)
		current.TaskID = "runtime-task-uuid-1"
		return []qualitygate.RunRecord{inWindow, current}
	})

	export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
	requireCollectedPredecessor(t, export, evidenceExportSealedAttemptID, "runtime-task-uuid-1", fixture.sealRef.Digest)
	identifiedPrefix := "predecessors/" + evidenceExportSealedAttemptID + "/state/quality-gate-runs/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	identifiedEntry := exportEntryByPath(t, export.Manifest, identifiedPrefix+"/run.json")
	if identifiedEntry.Missing || identifiedEntry.Unreadable != "" || identifiedEntry.SHA256 != digestBytes(exportFileByPath(t, export, identifiedPrefix+"/run.json")) {
		t.Fatalf("task-identified predecessor run entry = %#v", identifiedEntry)
	}
	if exportHasEntry(export.Manifest, "predecessors/"+evidenceExportSealedAttemptID+"/state/quality-gate-runs/cccccccccccccccccccccccccccccccc/run.json") {
		t.Fatal("current-window task-identified run duplicated into the predecessor window")
	}
	currentEntry := exportEntryByPath(t, export.Manifest, "live/validation/quality-gate/cccccccccccccccccccccccccccccccc/run.json")
	if currentEntry.Missing || currentEntry.Basis != evidenceExportBasisRepositoryWorkspace {
		t.Fatalf("current-window task-identified run entry = %#v", currentEntry)
	}
	if exportHasEntry(export.Manifest, "live/validation/quality-gate/aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa/run.json") {
		t.Fatal("predecessor-window task-identified run leaked into the current live section")
	}
	analysis := decodeAnalysisIndex(t, export)
	for _, entry := range analysis.CanonicalValidationRuns {
		if entry.RunID == "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" && entry.Log != identifiedPrefix+"/gate.log" {
			t.Fatalf("task-identified predecessor run analysis relation = %#v", entry)
		}
		if entry.RunID == "cccccccccccccccccccccccccccccccc" && entry.Binding != evidenceValidationBindingTaskRecord {
			t.Fatalf("current-window task-identified run analysis binding = %#v", entry)
		}
	}
}

func TestExportEvidenceTaskContinuationDisclosesUnreadableModuleRunRecords(t *testing.T) {
	fixture, repoRoot := predecessorValidationFixture(t, func(*evidenceExportFixture, string) []qualitygate.RunRecord { return nil })
	garbageModule := state.AttachStateStore(config.AppConfig{StateBase: fixture.cfg.StateBase, RepoHash: config.RepoHashFor(repoRoot)})
	if err := garbageModule.Write("repo-root", repoRoot); err != nil {
		t.Fatal(err)
	}
	garbageDir := garbageModule.Path(filepath.Join(qualitygate.RunDirectory, "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"))
	if err := os.MkdirAll(garbageDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(garbageDir, qualitygate.RunFile), []byte(`{"validation_run_id`), 0o600); err != nil {
		t.Fatal(err)
	}

	export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
	if result.RuntimeStatus != evidenceExportRuntimeCollected {
		t.Fatalf("unreadable module record lost current evidence: %#v", result)
	}
	garbageEntry := exportEntryByPath(t, export.Manifest, "predecessors/"+evidenceExportSealedAttemptID+"/state/quality-gate-runs/bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb/run.json")
	if garbageEntry.Unreadable == "" || garbageEntry.Missing {
		t.Fatalf("unreadable module record disclosure = %#v", garbageEntry)
	}
	for _, file := range export.Files {
		if file.Path == garbageEntry.Path && len(file.Data) != 0 {
			t.Fatal("unreadable module record archived raw bytes")
		}
	}
}

func TestExportEvidenceTaskContinuationCollectsValidationRunsWithPluralLeaseHistory(t *testing.T) {
	fixture, _ := predecessorValidationFixture(t, func(fixture *evidenceExportFixture, repoRoot string) []qualitygate.RunRecord {
		return []qualitygate.RunRecord{predecessorValidationRun("77777777777777777777777777777777",
			fixture.seal.StartedAt.Add(time.Minute), fixture.seal.StartedAt.Add(2*time.Minute), repoRoot)}
	})
	liveLease, err := fixture.store.loadLease(evidenceExportLiveLeaseID)
	if err != nil {
		t.Fatal(err)
	}
	liveLease.LeaseID = "lease-live-1-history"
	if err := fixture.store.writeLease(liveLease); err != nil {
		t.Fatal(err)
	}

	export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
	if result.RuntimeStatus != evidenceExportRuntimeCollected {
		t.Fatalf("plural lease history blocked the live export: %#v", result)
	}
	entry := exportEntryByPath(t, export.Manifest,
		"predecessors/"+evidenceExportSealedAttemptID+"/state/quality-gate-runs/77777777777777777777777777777777/run.json")
	if entry.Missing || entry.Unreadable != "" || entry.Basis != evidenceExportBasisRepositoryWorkspace {
		t.Fatalf("plural-lease validation run entry = %#v", entry)
	}
}

func TestExportEvidenceTaskBundleDisclosesAssociationReadFailures(t *testing.T) {
	aligned := "runtime-task-uuid-1"
	t.Run("unreadable association stays unverified", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.withSealedRuntime = true
			f.sealedAssociationUnreadable = true
		})
		export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		if result.RuntimeStatus != evidenceExportRuntimeCollected {
			t.Fatalf("unreadable association lost current evidence: %#v", result)
		}
		section := requireUnverifiedPredecessor(t, export, evidenceExportSealedAttemptID)
		if section.Task.TaskPath != fixture.seal.SemanticTaskRef.TaskPath ||
			section.Relation != evidenceExportPredecessorAssociationRelation {
			t.Fatalf("unreadable association disclosure lost identity: %#v", section)
		}
	})
	t.Run("vanished seal stays unverified without hiding the verified attempt", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.withSealedRuntime = true
			f.sealedAssociationRuntimeTask = aligned
		})
		ghost := AttemptRecord{
			SchemaVersion: controllerSchemaVersion, AttemptID: "attempt-ghost-seal",
			SemanticTaskRef: fixture.seal.SemanticTaskRef, EpisodeID: fixture.seal.EpisodeID,
			AttemptSealID: "seal-ghost", CreatedAt: fixture.seal.StartedAt.Add(-2 * time.Hour),
		}
		if err := fixture.store.writeAttempt(ghost); err != nil {
			t.Fatal(err)
		}
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		requireCollectedPredecessor(t, export, evidenceExportSealedAttemptID, aligned, fixture.sealRef.Digest)
		requireUnverifiedPredecessor(t, export, "attempt-ghost-seal")
	})
	t.Run("unreadable attempt inventory fails closed", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t)
		if err := os.MkdirAll(filepath.Join(fixture.store.dir, "attempts"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(fixture.store.dir, "attempts", "attempt-inventory-corrupt.json"), []byte(`{"schema_version":`), 0o600); err != nil {
			t.Fatal(err)
		}
		if _, _, err := fixture.store.ExportEvidence(EvidenceExportRequest{TaskID: fixture.runtimeTask}); err == nil {
			t.Fatal("export judged coverage with an unreadable attempt inventory")
		}
	})
}

func TestExportEvidenceTaskBundleAccumulatesAllAttemptsOfTask(t *testing.T) {
	aligned := "runtime-task-uuid-1"
	t.Run("association recovers evidence past a broken chain link", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.withSealedRuntime = true
			f.sealedAssociationRuntimeTask = aligned
		})
		fixture.rewriteLiveContinuation(t, evidenceExportSealedAttemptID, "wrong-seal-identity")
		export, result := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		if result.RuntimeStatus != evidenceExportRuntimeCollected {
			t.Fatalf("recovered export lost current evidence: %#v", result)
		}
		predecessors := export.Manifest.Attempt.Predecessors
		if len(predecessors) != 1 {
			t.Fatalf("recovered predecessor sections = %#v", predecessors)
		}
		section := predecessors[0]
		if section.Status != evidenceExportPredecessorPartial || section.Problem == "" ||
			section.SealDigest != fixture.sealRef.Digest || section.RuntimeTaskID != aligned ||
			section.Relation != evidenceExportPredecessorAssociationRelation || section.Window == nil {
			t.Fatalf("recovered predecessor section = %#v", section)
		}
		for _, path := range []string{
			"predecessors/" + evidenceExportSealedAttemptID + "/seal.json",
			"predecessors/" + evidenceExportSealedAttemptID + "/telemetry.jsonl",
			"predecessors/" + evidenceExportSealedAttemptID + "/state/worker.id",
		} {
			if !exportHasEntry(export.Manifest, path) {
				t.Fatalf("recovered predecessor lost evidence: %s", path)
			}
		}
	})
	t.Run("attempt without a chain link is included via its association", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.withSealedRuntime = true
			f.sealedAssociationRuntimeTask = aligned
		})
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		section := requireCollectedPredecessor(t, export, evidenceExportSealedAttemptID, aligned, fixture.sealRef.Digest)
		if section.Relation != evidenceExportPredecessorAssociationRelation {
			t.Fatalf("association-only predecessor relation = %#v", section)
		}
	})
	t.Run("foreign runtime task association stays excluded", func(t *testing.T) {
		fixture := newEvidenceExportFixture(t, func(f *evidenceExportFixture) {
			f.withSealedRuntime = true
		})
		export, _ := fixture.exportContinuation(t, EvidenceExportRequest{TaskID: fixture.runtimeTask})
		if len(export.Manifest.Attempt.Predecessors) != 0 {
			t.Fatalf("foreign runtime task predecessor sections = %#v", export.Manifest.Attempt.Predecessors)
		}
	})
}

func (f *evidenceExportFixture) exportContinuation(t *testing.T, request EvidenceExportRequest) (EvidenceExport, EvidenceExportResult) {
	t.Helper()
	export, result, err := f.store.ExportEvidence(request)
	if err != nil {
		t.Fatalf("continuation export: %v", err)
	}
	return export, result
}

func requireUnverifiedPredecessor(t *testing.T, export EvidenceExport, attemptID string) EvidenceExportPredecessorSection {
	t.Helper()
	for _, section := range export.Manifest.Attempt.Predecessors {
		if section.AttemptID != attemptID {
			continue
		}
		if section.Status != evidenceExportPredecessorPartial || section.Problem == "" ||
			section.SealDigest != "" || section.Window != nil || section.RuntimeTaskID != "" {
			t.Fatalf("unverified predecessor section = %#v", section)
		}
		for _, entry := range export.Manifest.Entries {
			if strings.HasPrefix(entry.Path, section.EntriesPrefix+"/") {
				t.Fatalf("unverified predecessor projected evidence: %s", entry.Path)
			}
		}
		return section
	}
	t.Fatalf("predecessor section %s is missing", attemptID)
	return EvidenceExportPredecessorSection{}
}
