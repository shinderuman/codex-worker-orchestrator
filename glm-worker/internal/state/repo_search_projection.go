package state

import (
	"bufio"
	"errors"
	"fmt"
	"os"
)

func (s *StateStore) EnableRepoSearchReadProjection() {
	s.repoSearchReadProjection = true
}

func (s *StateStore) projectCurrentRepoSearchStats() (TaskStats, error) {
	stats, err := s.loadTaskStats()
	if err != nil {
		return TaskStats{}, err
	}

	measure, retained, err := s.RepoSearchMeasureFromTaskEvents(stats.TaskID)
	if err != nil {
		return TaskStats{}, err
	}
	if !retained {
		return stats, nil
	}
	applyRepoSearchMeasure(&stats, measure)
	return stats, nil
}

func (s *StateStore) projectRepoSearchStatsForRead(stats TaskStats) (TaskStats, error) {
	if !s.repoSearchReadProjection {
		return stats, nil
	}
	measure, retained, err := s.RepoSearchMeasureFromTaskEvents(stats.TaskID)
	if err != nil {
		return TaskStats{}, err
	}
	if !retained {
		return stats, nil
	}
	applyRepoSearchMeasure(&stats, measure)
	return stats, nil
}

func applyRepoSearchMeasure(stats *TaskStats, measure RepoSearchMeasure) {
	stats.RepoSearchCalls = measure.Calls
	stats.RepoSearchQueriesByCategory = measure.QueriesByCategory
	stats.RepoSearchOutcomes = measure.Outcomes
	stats.RepoSearchResults = measure.Results
	stats.RepoSearchDurationMS = measure.DurationMS
}

func (s *StateStore) RepoSearchMeasureFromTaskEvents(taskID string) (RepoSearchMeasure, bool, error) {
	file, err := os.Open(s.TaskEventLogPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return RepoSearchMeasure{}, false, nil
	}
	if err != nil {
		return RepoSearchMeasure{}, false, err
	}
	defer func() { _ = file.Close() }()

	var measure RepoSearchMeasure
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		record, err := ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			return RepoSearchMeasure{}, true, fmt.Errorf("repo-search task eventを読めません: %w", err)
		}
		if !IsRepoSearchRouteEvent(record) {
			continue
		}
		measure.absorbRoute(record.Phase, record.Subtype, len(record.SearchPaths), record.DurationMS)
	}
	if err := scanner.Err(); err != nil {
		return RepoSearchMeasure{}, true, fmt.Errorf("repo-search task eventを走査できません: %w", err)
	}
	return measure, true, nil
}
