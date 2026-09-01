package state

import (
	"path/filepath"
	"strings"
)

type TaskCallCoverage struct {
	TaskID     string
	Archived   bool
	StatsCalls int
	RawRecords int
	Unreadable bool
}

type TelemetryCoverage struct {
	Tasks         []TaskCallCoverage
	StatsCalls    int
	RawRecords    int
	MissingCalls  int
	ExcessRecords int
	OrphanFiles   int
	Status        string
	UsageKnown    bool
}

const (
	CoverageComplete      = "complete"
	CoverageIncomplete    = "incomplete"
	CoverageUnreadable    = "unreadable"
	CoverageHistoricalGap = "historical-gap"
)

func (c TaskCallCoverage) MissingCalls() int {
	if c.Unreadable || c.StatsCalls <= c.RawRecords {
		return 0
	}
	return c.StatsCalls - c.RawRecords
}

func (c TaskCallCoverage) ExcessRecords() int {
	if c.Unreadable || c.RawRecords <= c.StatsCalls {
		return 0
	}
	return c.RawRecords - c.StatsCalls
}

func (c TaskCallCoverage) Classification() string {
	switch {
	case c.Unreadable:
		return CoverageUnreadable
	case c.MissingCalls() > 0:
		if c.Archived {
			return CoverageHistoricalGap
		}
		return CoverageIncomplete
	case c.ExcessRecords() > 0:
		return CoverageIncomplete
	}
	return CoverageComplete
}

func (s *StateStore) ComputeTelemetryCoverage(tasks []TaskStats) TelemetryCoverage {
	coverage := TelemetryCoverage{
		Tasks:  make([]TaskCallCoverage, 0, len(tasks)),
		Status: CoverageComplete,
	}
	known := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		known[task.TaskID] = true
		entry := TaskCallCoverage{
			TaskID:     task.TaskID,
			Archived:   task.ArchivedAt != nil,
			StatsCalls: task.ModelCalls,
		}
		records, err := s.CountFinalizedTaskCalls(task.TaskID)
		if err != nil {
			entry.Unreadable = true
		} else {
			entry.RawRecords = records
		}
		coverage.Tasks = append(coverage.Tasks, entry)
		coverage.StatsCalls += entry.StatsCalls
		coverage.RawRecords += entry.RawRecords
		coverage.MissingCalls += entry.MissingCalls()
		coverage.ExcessRecords += entry.ExcessRecords()

		switch entry.Classification() {
		case CoverageUnreadable:
			coverage.Status = CoverageUnreadable
		case CoverageIncomplete, CoverageHistoricalGap:
			if coverage.Status == CoverageComplete {
				coverage.Status = CoverageIncomplete
			}
		}
	}
	coverage.OrphanFiles = s.countOrphanTelemetryFiles(known)
	if coverage.OrphanFiles > 0 && coverage.Status == CoverageComplete {
		coverage.Status = CoverageIncomplete
	}
	coverage.UsageKnown = coverage.Status == CoverageComplete
	return coverage
}

func (s *StateStore) countOrphanTelemetryFiles(known map[string]bool) int {
	paths, err := filepath.Glob(filepath.Join(s.dir, "telemetry", "*.jsonl"))
	if err != nil {
		return 0
	}
	count := 0
	for _, path := range paths {
		if !known[strings.TrimSuffix(filepath.Base(path), ".jsonl")] {
			count++
		}
	}
	return count
}
