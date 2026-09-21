package state

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

type TaskStatsArchiveScan struct {
	FilesConsidered                    int `json:"files_considered"`
	FilesAccepted                      int `json:"files_accepted"`
	UnsupportedSchemaOrRevisionSkipped int `json:"unsupported_schema_or_revision_skipped"`
}

type AllTaskStatsScanResult struct {
	Stats       []TaskStats
	ArchiveScan TaskStatsArchiveScan
}

func (s *StateStore) scanTaskStatsArchives() ([]TaskStats, TaskStatsArchiveScan, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "stats", "*.json"))
	if err != nil {
		return nil, TaskStatsArchiveScan{}, err
	}
	sort.Strings(paths)

	stats := make([]TaskStats, 0, len(paths))
	scan := TaskStatsArchiveScan{FilesConsidered: len(paths)}
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, TaskStatsArchiveScan{}, err
		}
		archive, err := decodeTaskStats(data)
		switch {
		case err == nil:
			scan.FilesAccepted++
			stats = append(stats, archive)
		case errors.Is(err, errUnsupportedTaskStatsVersion):
			scan.UnsupportedSchemaOrRevisionSkipped++
		default:
			return nil, TaskStatsArchiveScan{}, fmt.Errorf("task stats historyを読めません: %w", err)
		}
	}
	return stats, scan, nil
}

func (s *StateStore) ScanTaskStatsArchives() (TaskStatsArchiveScan, error) {
	_, scan, err := s.scanTaskStatsArchives()
	return scan, err
}

func (s *StateStore) AllTaskStatsWithArchiveScan() (AllTaskStatsScanResult, error) {
	stats, scan, err := s.scanTaskStatsArchives()
	if err != nil {
		return AllTaskStatsScanResult{}, err
	}

	current, err := s.CurrentTaskStats()
	switch {
	case err == nil:
		current, err = s.projectRepoSearchStatsForRead(current)
		if err != nil {
			return AllTaskStatsScanResult{}, fmt.Errorf("current repo-search evidenceを読めません: %w", err)
		}
		stats = append(stats, current)
	case errors.Is(err, errUnsupportedTaskStatsVersion):
		return AllTaskStatsScanResult{Stats: stats, ArchiveScan: scan}, nil
	case !errors.Is(err, os.ErrNotExist):
		return AllTaskStatsScanResult{}, err
	}

	return AllTaskStatsScanResult{Stats: stats, ArchiveScan: scan}, nil
}
