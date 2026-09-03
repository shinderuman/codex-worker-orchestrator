package taskcontract

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type TaskCorpusEntry struct {
	Path    string
	Regular bool
}

type ScheduleClosureFailure struct {
	Kind   string
	Path   string
	Reason string
}

const (
	ScheduleClosureScheduleParse   = "schedule-parse"
	ScheduleClosureInvalidTaskPath = "invalid-task-path"
	ScheduleClosureDuplicateEntry  = "duplicate-schedule-entry"
	ScheduleClosureMissingTask     = "missing-task"
	ScheduleClosureNonRegularTask  = "non-regular-task"
	ScheduleClosureUnscheduledTask = "unscheduled-task"
)

func EnumerateTaskCorpus(repoRoot string) ([]TaskCorpusEntry, error) {
	dir := filepath.Join(repoRoot, TasksDir)
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("stat %s: %w", TasksDir, err)
	}
	var entries []TaskCorpusEntry
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		relative, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		corpusPath := filepath.ToSlash(relative)
		if corpusPath == TasksDir || !strings.HasSuffix(corpusPath, ".md") {
			return nil
		}
		entries = append(entries, TaskCorpusEntry{Path: corpusPath, Regular: entry.Type().IsRegular()})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("walk %s: %w", TasksDir, err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func (s PlanSchedule) ClosureFailures(entries []TaskCorpusEntry) []ScheduleClosureFailure {
	if err := s.parseError(); err != nil {
		return []ScheduleClosureFailure{{
			Kind:   ScheduleClosureScheduleParse,
			Reason: "IMPLEMENTATION_PLAN.local.mdのscheduleを解析できないためclosureを検証できません: " + err.Error(),
		}}
	}
	failures := scheduledClosureFailures(s, entries)
	failures = append(failures, unscheduledClosureFailures(s, entries)...)
	sort.Slice(failures, func(i, j int) bool {
		if failures[i].Path != failures[j].Path {
			return failures[i].Path < failures[j].Path
		}
		return failures[i].Kind < failures[j].Kind
	})
	return failures
}

func (s PlanSchedule) parseError() error {
	for _, err := range []error{s.activeErr, s.nextErr, s.blockedErr} {
		if err != nil {
			return err
		}
	}
	return nil
}

func scheduledClosureFailures(s PlanSchedule, entries []TaskCorpusEntry) []ScheduleClosureFailure {
	corpus := make(map[string]bool, len(entries))
	for _, entry := range entries {
		corpus[entry.Path] = entry.Regular
	}
	counts := scheduleEntryCounts(s)
	paths := make([]string, 0, len(counts))
	for path := range counts {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var failures []ScheduleClosureFailure
	for _, path := range paths {
		if err := ValidateActiveTaskPath(path); err != nil {
			failures = append(failures, ScheduleClosureFailure{
				Kind:   ScheduleClosureInvalidTaskPath,
				Path:   path,
				Reason: fmt.Sprintf("schedule項目 %sがtask path配置契約に違反しています: %v", path, err),
			})
			continue
		}
		if counts[path] > 1 {
			failures = append(failures, ScheduleClosureFailure{
				Kind:   ScheduleClosureDuplicateEntry,
				Path:   path,
				Reason: fmt.Sprintf("task %sがPlanのscheduleへ重複して列挙されています(%d回)", path, counts[path]),
			})
		}
		entryRegular, present := corpus[path]
		switch {
		case !present:
			failures = append(failures, ScheduleClosureFailure{
				Kind:   ScheduleClosureMissingTask,
				Path:   path,
				Reason: fmt.Sprintf("schedule項目 %sがcurrent task corpusへ存在しません", path),
			})
		case !entryRegular:
			failures = append(failures, ScheduleClosureFailure{
				Kind:   ScheduleClosureNonRegularTask,
				Path:   path,
				Reason: fmt.Sprintf("task %sはregular fileではありません", path),
			})
		}
	}
	return failures
}

func unscheduledClosureFailures(s PlanSchedule, entries []TaskCorpusEntry) []ScheduleClosureFailure {
	scheduled := scheduleEntryCounts(s)
	var failures []ScheduleClosureFailure
	for _, entry := range entries {
		if _, ok := scheduled[entry.Path]; ok {
			continue
		}
		if !entry.Regular {
			failures = append(failures, ScheduleClosureFailure{
				Kind:   ScheduleClosureNonRegularTask,
				Path:   entry.Path,
				Reason: fmt.Sprintf("task %sはregular fileではありません", entry.Path),
			})
			continue
		}
		failures = append(failures, ScheduleClosureFailure{
			Kind:   ScheduleClosureUnscheduledTask,
			Path:   entry.Path,
			Reason: fmt.Sprintf("task %sがPlanのACTIVE/NEXT/BLOCKEDいずれにも列挙されていません", entry.Path),
		})
	}
	return failures
}

func scheduleEntryCounts(s PlanSchedule) map[string]int {
	counts := make(map[string]int)
	for _, section := range [][]string{s.Active, s.Next, s.Blocked} {
		for _, path := range section {
			counts[path]++
		}
	}
	return counts
}
