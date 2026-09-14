package state

import (
	"bufio"
	"errors"
	"fmt"
	"os"
)

func (s *StateStore) projectCurrentRepoSearchStats() {
	stats, err := s.loadTaskStats()
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		warnStatsFailure("repo-search archive投影の読み込み", err)
		return
	}

	measure, err := s.repoSearchMeasureFromTaskEvents(stats.TaskID)
	if err != nil {
		WarnTaskEventSkip("repo-search archive投影", err)
		measure = RepoSearchMeasure{}
	}
	stats.RepoSearchCalls = measure.Calls
	stats.RepoSearchQueriesByCategory = measure.QueriesByCategory
	stats.RepoSearchOutcomes = measure.Outcomes
	stats.RepoSearchResults = measure.Results
	stats.RepoSearchDurationMS = measure.DurationMS
	if err := s.writeTaskStats(stats); err != nil {
		warnStatsFailure("repo-search archive投影", err)
	}
}

func (s *StateStore) repoSearchMeasureFromTaskEvents(taskID string) (RepoSearchMeasure, error) {
	file, err := os.Open(s.TaskEventLogPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return RepoSearchMeasure{}, nil
	}
	if err != nil {
		return RepoSearchMeasure{}, err
	}
	defer func() { _ = file.Close() }()

	var measure RepoSearchMeasure
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		record, err := ParseTaskEventLine(scanner.Bytes())
		if err != nil {
			return RepoSearchMeasure{}, fmt.Errorf("repo-search archive投影でtask eventを読めません: %w", err)
		}
		if !IsRepoSearchRouteEvent(record) {
			continue
		}
		measure.absorbRoute(record.Phase, record.Subtype, len(record.SearchPaths), record.DurationMS)
	}
	if err := scanner.Err(); err != nil {
		return RepoSearchMeasure{}, fmt.Errorf("repo-search archive投影でtask eventを走査できません: %w", err)
	}
	return measure, nil
}
