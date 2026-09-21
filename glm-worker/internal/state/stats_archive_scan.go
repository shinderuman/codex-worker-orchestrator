package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// TaskStatsArchiveScan reports bounded coverage for the archive files considered
// by aggregate task-stats consumers. It intentionally does not expose file names
// or rejected archive contents.
type TaskStatsArchiveScan struct {
	FilesConsidered                    int `json:"files_considered"`
	FilesAccepted                      int `json:"files_accepted"`
	UnsupportedSchemaOrRevisionSkipped int `json:"unsupported_schema_or_revision_skipped"`
}

// AllTaskStatsScanResult keeps the existing aggregate task set together with the
// coverage of the archive scan that feeds it.
type AllTaskStatsScanResult struct {
	Stats       []TaskStats
	ArchiveScan TaskStatsArchiveScan
}

// ScanTaskStatsArchives reports archive coverage using exactly the same decoder
// acceptance boundary as AllTaskStats. Unsupported machine schemas are counted
// and skipped; malformed current-schema archives remain errors.
func (s *StateStore) ScanTaskStatsArchives() (TaskStatsArchiveScan, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "stats", "*.json"))
	if err != nil {
		return TaskStatsArchiveScan{}, err
	}

	scan := TaskStatsArchiveScan{FilesConsidered: len(paths)}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return TaskStatsArchiveScan{}, err
		}
		_, err = decodeTaskStats(data)
		switch {
		case err == nil:
			scan.FilesAccepted++
		case errors.Is(err, errUnsupportedTaskStatsVersion):
			scan.UnsupportedSchemaOrRevisionSkipped++
		default:
			return TaskStatsArchiveScan{}, fmt.Errorf("task stats historyを読めません: %w", err)
		}
	}
	return scan, nil
}

// AllTaskStatsWithArchiveScan preserves AllTaskStats aggregate semantics while
// making unsupported archive skips observable to callers that surface coverage.
func (s *StateStore) AllTaskStatsWithArchiveScan() (AllTaskStatsScanResult, error) {
	stats, err := s.AllTaskStats()
	if err != nil {
		return AllTaskStatsScanResult{}, err
	}
	scan, err := s.ScanTaskStatsArchives()
	if err != nil {
		return AllTaskStatsScanResult{}, err
	}
	return AllTaskStatsScanResult{Stats: stats, ArchiveScan: scan}, nil
}
