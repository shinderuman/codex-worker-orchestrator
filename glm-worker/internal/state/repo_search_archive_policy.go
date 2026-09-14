package state

import (
	"errors"
	"os"
)

func (s *StateStore) prepareRepoSearchArchiveStats() (TaskStats, bool, error) {
	taskID := s.ReadOr("task.id", "")
	if taskID == "" {
		return TaskStats{}, false, nil
	}
	measure, retained, err := s.RepoSearchMeasureFromTaskEvents(taskID)
	if err != nil {
		return TaskStats{}, true, err
	}
	if !retained || measure.Calls == 0 {
		return TaskStats{}, false, nil
	}
	stats, err := s.loadTaskStats()
	if err != nil {
		return TaskStats{}, true, err
	}
	applyRepoSearchMeasure(&stats, measure)
	return stats, true, nil
}

func (s *StateStore) archiveCurrentStatsBestEffort(parentIdentity *ParentCodexIdentity) {
	stats, err := s.projectCurrentRepoSearchStats()
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		warnStatsFailure("repo-search archive投影", err)
		return
	}
	if err := s.archiveCurrentStats(parentIdentity, stats); err != nil {
		warnStatsFailure("archive", err)
	}
}
