package report

import (
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/qualitygate"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

type stageTimelineOverhead struct {
	WriteWallNS   int64  `json:"write_wall_ns"`
	WriteCalls    int    `json:"write_calls"`
	WriteFailures int    `json:"write_failures"`
	CPUTimeNS     *int64 `json:"cpu_time_ns"`
	Coverage      string `json:"coverage"`
}

func correlateStageAttempts(attributed, unattributed []state.ParentActionAttemptRecord, logs []state.ModelCallLog) ([]state.ParentActionAttemptRecord, []state.ParentActionAttemptRecord) {
	observed := map[string]bool{}
	for _, log := range logs {
		if log.Stage != nil && log.Stage.AttemptID != "" {
			observed[log.Stage.AttemptID] = true
		}
	}
	var remaining []state.ParentActionAttemptRecord
	for _, attempt := range unattributed {
		if observed[attempt.AttemptID] {
			attributed = append(attributed, attempt)
		} else {
			remaining = append(remaining, attempt)
		}
	}
	return attributed, remaining
}

func finalizedStageLogs(logs []state.ModelCallLog) []state.ModelCallLog {
	positions := map[string]int{}
	var result []state.ModelCallLog
	for _, log := range logs {
		if log.CallType != state.CallTypeStage || log.CallID == "" {
			result = append(result, log)
			continue
		}
		if index, ok := positions[log.CallID]; ok {
			result[index] = log
		} else {
			positions[log.CallID] = len(result)
			result = append(result, log)
		}
	}
	return result
}

func stageEntryLocator(st *state.StateStore, taskID string, entry stageTimelineEntry) string {
	switch entry.Kind {
	case stageTimelineKindParentAction:
		return st.Path("parent-action-attempts.jsonl") + "#attempt_id=" + entry.AttemptID + ";action=" + entry.Name
	case stageTimelineKindLifecycle:
		return st.TaskLifecycleLogPath(taskID) + "#timestamp=" + entry.StartedAt.Format("2006-01-02T15:04:05.999999999Z07:00")
	default:
		if entry.Kind == stageTimelineKindStartupStage {
			return st.TaskStageLogPath(taskID) + "#call_id=" + entry.RunID + ";attempt_id=" + entry.AttemptID + ";stage=" + entry.Name
		}
		return st.ModelCallLogPath(taskID) + "#call_id=" + entry.CallID + ";attempt_id=" + entry.AttemptID + ";stage=" + entry.Name
	}
}

func slowestStageEntries(entries []stageTimelineEntry) []stageTimelineEntry {
	var result []stageTimelineEntry
	for _, entry := range entries {
		if entry.DurationMS != nil {
			result = append(result, entry)
		}
	}
	sort.SliceStable(result, func(i, j int) bool { return *result[i].DurationMS > *result[j].DurationMS })
	if len(result) > 10 {
		result = result[:10]
	}
	return result
}

func stageTimelineValidationEntries(st *state.StateStore, taskID string) ([]stageTimelineEntry, timelineSourceState) {
	root := st.Path(qualitygate.RunDirectory)
	files, err := os.ReadDir(root)
	source := stageTimelineSourceState(err, root, 0)
	if err != nil {
		return nil, source
	}
	var entries []stageTimelineEntry
	for _, file := range files {
		if !file.IsDir() || !qualitygate.ValidRunID(file.Name()) {
			continue
		}
		record, err := qualitygate.Read(st, file.Name())
		if err != nil {
			source.Status = taskview.StatusUnreadable
			continue
		}
		if record.TaskID != taskID {
			continue
		}
		entries = append(entries, stageValidationEntry(record, filepath.Join(root, file.Name(), qualitygate.RunFile)))
	}
	source.Records = len(entries)
	if len(entries) == 0 && source.Status == TimelineSourceOK {
		source.Status = taskview.StatusNone
	}
	return entries, source
}

func stageValidationEntry(record qualitygate.RunRecord, path string) stageTimelineEntry {
	entry := stageTimelineEntry{
		Kind: "validation", Name: record.Form, RunID: record.ValidationRunID,
		Outcome: record.Status, StartedAt: &record.StartedAt, CompletedAt: record.CompletedAt,
		SourceHead: record.Head, SourceDigest: record.WorktreeDigest, ClockOrigin: "quality-gate", Locator: path,
	}
	if record.CompletedAt != nil {
		duration := record.DurationMS
		entry.DurationMS = &duration
	}
	return entry
}

func stageTimelineInnerGaps(entries []stageTimelineEntry, attributed []state.ParentActionAttemptRecord) []stageTimelineGap {
	var gaps []stageTimelineGap
	for _, attempt := range attributed {
		if attempt.ExitedAt.IsZero() {
			continue
		}
		gaps = append(gaps, stageTimelineAttemptGaps(entries, attempt)...)
	}
	return gaps
}

func stageTimelineAttemptGaps(entries []stageTimelineEntry, attempt state.ParentActionAttemptRecord) []stageTimelineGap {
	var gaps []stageTimelineGap
	cursor := attempt.EnteredAt
	for _, entry := range entries {
		if entry.Kind == stageTimelineKindParentAction || entry.Kind == stageTimelineKindLifecycle {
			continue
		}
		interval, ok := entryInterval(entry)
		if !ok || interval.start.Before(attempt.EnteredAt) || interval.end.After(attempt.ExitedAt) {
			continue
		}
		if interval.start.After(cursor) {
			gaps = append(gaps, stageTimelineGap{Classification: stageTimelineGapInAttempt, StartedAt: cursor, EndedAt: interval.start, DurationMS: interval.start.Sub(cursor).Milliseconds()})
		}
		if interval.end.After(cursor) {
			cursor = interval.end
		}
	}
	if attempt.ExitedAt.After(cursor) {
		gaps = append(gaps, stageTimelineGap{Classification: stageTimelineGapInAttempt, StartedAt: cursor, EndedAt: attempt.ExitedAt, DurationMS: attempt.ExitedAt.Sub(cursor).Milliseconds()})
	}
	return gaps
}

func stageObservationOverhead(logs []state.ModelCallLog) stageTimelineOverhead {
	result := stageTimelineOverhead{Coverage: "partial: append wall/I/O callsのみ。CPU・最終summary append・pre-task archive・親attempt ledger書込は未計測"}
	for _, log := range logs {
		if log.CallType == state.CallTypeEvent && log.Stage != nil && log.Stage.Stage == "observation-overhead" {
			result.WriteWallNS += log.Stage.WriteWallNS
			result.WriteCalls += log.Stage.WriteCalls
			result.WriteFailures += log.Stage.WriteFailures
		}
	}
	return result
}

func unobservedStartupStages(stages map[string]struct{}) []string {
	var missing []string
	for _, stage := range state.StartupStageSequence {
		if _, ok := stages[stage]; !ok {
			missing = append(missing, stage)
		}
	}
	return missing
}

func callInitBoundary(events []state.TaskEventRecord, callID string) *time.Time {
	var first *time.Time
	for _, event := range events {
		if event.CallID != callID || event.Kind != "system" || event.Subtype != "init" {
			continue
		}
		if first == nil || event.Timestamp.Before(*first) {
			at := event.Timestamp
			first = &at
		}
	}
	return first
}
