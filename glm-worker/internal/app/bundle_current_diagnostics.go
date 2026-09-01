package app

import (
	"os"
	"path"
	"path/filepath"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func (c *bundleCollector) collectCurrentValidationDiagnostics(st *state.StateStore) {
	stats, err := st.CurrentTaskStats()
	if err != nil || stats.TaskID == "" || stats.StartedAt.IsZero() {
		return
	}

	eventRuns := analysisTaskEventValidationRuns(st, stats.TaskID)
	roundSeqByDigest := analysisRoundDigestSeqs(st, stats.TaskID)
	entries, err := os.ReadDir(st.Path(qualityGateRunDirectory))
	if err != nil {
		return
	}

	end := time.Now().UTC()
	for _, entry := range entries {
		runID := entry.Name()
		if !entry.IsDir() || !validValidationRunID(runID) {
			continue
		}
		sourceRoot := st.Path(filepath.Join(qualityGateRunDirectory, runID))
		archiveRoot := path.Join("current-state", "diagnostics", qualityGateRunDirectory, runID)
		if _, linked := eventRuns[runID]; linked {
			c.addTreeIfPresent(sourceRoot, archiveRoot)
			continue
		}

		record, err := readAnalysisRunRecord(filepath.Join(sourceRoot, qualityGateRunFile))
		if err != nil {
			continue
		}
		_, digestMatched := roundSeqByDigest[roundSnapshotDigestKey(record.Head, record.IndexDigest, record.WorktreeDigest)]
		overlaps := analysisRunOverlapsWindow(record, stats.StartedAt.UTC(), end)
		switch {
		case digestMatched:
			c.addTreeIfPresent(sourceRoot, archiveRoot)
		case overlaps:
			c.addUnattributedTreeIfPresent(sourceRoot, archiveRoot)
		}
	}
}
