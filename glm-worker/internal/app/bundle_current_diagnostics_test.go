package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBundleCurrentDiagnosticsIgnoreUnrelatedHistory(t *testing.T) {
	cfg, st := newBundleTestState(t)
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(-time.Hour)
	st.UpdateTaskStats(func(stats *state.TaskStats) { stats.StartedAt = start })
	writeBundleAuthority(t, cfg, st, "IMPLEMENTATION_TASKS/current.md")

	linkedRun := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	matchedRun := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	unmatchedRun := "cccccccccccccccccccccccccccccccc"
	st.RecordValidation("quality-gate", "go-test", "", state.ValidationResultPass, 0,
		state.ValidationExitSourceTarget, 1, "quality-gate-runs/"+linkedRun+"/gate.log")

	digest := state.SnapshotDigest{Head: "head", IndexDigest: "index", WorktreeDigest: "worktree"}
	if err := st.AppendRoundRecord(state.RoundRecord{
		Version: 1, TaskID: taskID, WorkerPhase: "worker-new", CapturedAt: start.Add(10 * time.Minute), Snapshot: digest,
	}); err != nil {
		t.Fatal(err)
	}
	currentAt := start.Add(20 * time.Minute)
	writeAnalysisRun(t, st, linkedRun, "go-test", "pass", currentAt, currentAt, state.GitSnapshot{})
	writeAnalysisRun(t, st, matchedRun, "go-test-race", "pass", start.Add(-4*time.Hour), start.Add(-3*time.Hour), state.GitSnapshot{
		Head: digest.Head, IndexDigest: digest.IndexDigest, WorktreeDigest: digest.WorktreeDigest,
	})
	writeAnalysisRun(t, st, unmatchedRun, "go-test", "pass", currentAt, currentAt, state.GitSnapshot{})
	for index := 0; index < 24; index++ {
		runID := fmt.Sprintf("%032x", index+1)
		writeAnalysisRun(t, st, runID, "go-test", "pass", start.Add(-6*time.Hour), start.Add(-5*time.Hour), state.GitSnapshot{})
	}

	var stdout bytes.Buffer
	if err := Execute(Command{Mode: ModeBundle}, cfg, nil, &stdout, nil); err != nil {
		t.Fatal(err)
	}
	var output bundleOutput
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		t.Fatal(err)
	}
	archive := readBundleArchive(t, output.ArchivePath)
	for _, runID := range []string{linkedRun, matchedRun, unmatchedRun} {
		archivePath := filepath.ToSlash(filepath.Join("current-state", "diagnostics", qualityGateRunDirectory, runID, qualityGateRunFile))
		if _, ok := archive[archivePath]; !ok {
			t.Fatalf("relevant validation run missing: %s", archivePath)
		}
	}
	for index := 0; index < 24; index++ {
		runID := fmt.Sprintf("%032x", index+1)
		archivePath := filepath.ToSlash(filepath.Join("current-state", "diagnostics", qualityGateRunDirectory, runID, qualityGateRunFile))
		if _, ok := archive[archivePath]; ok {
			t.Fatalf("unrelated historical validation included: %s", archivePath)
		}
		if slices.Contains(output.Unattributed, archivePath) {
			t.Fatalf("receipt enumerates unrelated historical validation: %s", archivePath)
		}
	}
	unmatchedPath := filepath.ToSlash(filepath.Join("current-state", "diagnostics", qualityGateRunDirectory, unmatchedRun, qualityGateRunFile))
	if !slices.Contains(output.Unattributed, unmatchedPath) {
		t.Fatalf("in-window unmatched validation lost conservative attribution: %v", output.Unattributed)
	}

	var manifest bundleManifest
	if err := json.Unmarshal(archive["manifest.json"], &manifest); err != nil {
		t.Fatal(err)
	}
	wantIncluded := []string{"manifest.json", bundleCollectionEntryPath, bundleAnalysisEntryPath}
	if !slices.Equal(manifest.Included, wantIncluded) {
		t.Fatalf("manifest included = %v, want %v", manifest.Included, wantIncluded)
	}

	var analysis bundleAnalysisIndex
	if err := json.Unmarshal(archive[bundleAnalysisEntryPath], &analysis); err != nil {
		t.Fatal(err)
	}
	if len(analysis.ValidationRuns.Runs) != 3 {
		t.Fatalf("analysis validation runs = %#v", analysis.ValidationRuns.Runs)
	}
}
