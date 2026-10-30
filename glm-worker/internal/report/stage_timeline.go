package report

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

type StageTimelineOutput struct {
	TaskID               string                     `json:"task_id"`
	TaskStatus           *string                    `json:"task_status"`
	GeneratedAt          time.Time                  `json:"generated_at"`
	Sources              stageTimelineSources       `json:"sources"`
	Entries              []stageTimelineEntry       `json:"entries"`
	Gaps                 []stageTimelineGap         `json:"gaps"`
	InnerGaps            []stageTimelineGap         `json:"inner_unmeasured_gaps"`
	NotRunStages         []stageTimelineNotRun      `json:"unobserved_stages,omitempty"`
	UnattributedAttempts []stageTimelineAttemptView `json:"unattributed_attempts,omitempty"`
	Coverage             stageTimelineCoverage      `json:"coverage"`
	Wall                 stageTimelineWall          `json:"wall"`
	Slowest              []stageTimelineEntry       `json:"slowest"`
	Overhead             stageTimelineOverhead      `json:"overhead"`
	ToolTotals           []TimelineTool             `json:"tool_totals"`
	ModelCalls           int                        `json:"model_calls"`
	TaskBaseline         *state.GitBaselineEvidence `json:"task_baseline,omitempty"`
}

type stageTimelineSources struct {
	Telemetry   timelineSourceState `json:"telemetry"`
	Attempts    timelineSourceState `json:"parent_action_attempts"`
	Lifecycle   timelineSourceState `json:"lifecycle"`
	Events      timelineSourceState `json:"event_log"`
	Archive     timelineSourceState `json:"startup_stage_archive"`
	Stages      timelineSourceState `json:"execution_stages"`
	Validations timelineSourceState `json:"validations"`
}

type stageTimelineEntry struct {
	Index                int        `json:"index"`
	Kind                 string     `json:"kind"`
	Name                 string     `json:"name"`
	AttemptID            string     `json:"attempt_id,omitempty"`
	CallID               string     `json:"call_id,omitempty"`
	Role                 string     `json:"role,omitempty"`
	Outcome              string     `json:"outcome,omitempty"`
	ClockOrigin          string     `json:"clock_origin,omitempty"`
	StartedAt            *time.Time `json:"started_at,omitempty"`
	CompletedAt          *time.Time `json:"completed_at,omitempty"`
	DurationMS           *int64     `json:"duration_ms,omitempty"`
	FirstEventAt         *time.Time `json:"first_event_at,omitempty"`
	FirstCLIInitAt       *time.Time `json:"first_cli_init_at,omitempty"`
	FirstModelActivityAt *time.Time `json:"first_model_activity_at,omitempty"`
	Error                string     `json:"error,omitempty"`
	Locator              string     `json:"locator,omitempty"`
	SourceHead           string     `json:"source_head,omitempty"`
	WorkerRevision       string     `json:"worker_revision,omitempty"`
	SourceDigest         string     `json:"source_digest,omitempty"`
	RunID                string     `json:"run_id,omitempty"`
	RetryOf              string     `json:"retry_of,omitempty"`
	FailureClass         string     `json:"failure_class,omitempty"`
	WallShare            *float64   `json:"wall_share,omitempty"`
}

type stageTimelineGap struct {
	AfterIndex     int       `json:"after_index"`
	BeforeIndex    int       `json:"before_index"`
	Classification string    `json:"classification"`
	StartedAt      time.Time `json:"started_at"`
	EndedAt        time.Time `json:"ended_at"`
	DurationMS     int64     `json:"duration_ms"`
	WallShare      float64   `json:"wall_share"`
}

type stageTimelineNotRun struct {
	AttemptID string   `json:"attempt_id"`
	Action    string   `json:"action"`
	Outcome   string   `json:"outcome"`
	NotRun    []string `json:"not_run"`
}

type stageTimelineAttemptView struct {
	AttemptID  string    `json:"attempt_id"`
	Action     string    `json:"action"`
	Outcome    string    `json:"outcome"`
	EnteredAt  time.Time `json:"entered_at"`
	ExitedAt   time.Time `json:"exited_at"`
	DurationMS int64     `json:"duration_ms"`
}

type stageTimelineCoverage struct {
	ProviderSendBoundary string   `json:"provider_send_boundary"`
	ParentTurnBoundary   string   `json:"parent_turn_boundary"`
	Status               string   `json:"status"`
	Missing              []string `json:"missing"`
	StartupStageSequence []string `json:"startup_stage_sequence"`
}

type stageTimelineWall struct {
	Start           *time.Time `json:"start,omitempty"`
	End             *time.Time `json:"end,omitempty"`
	WallMS          int64      `json:"wall_ms"`
	MeasuredUnionMS int64      `json:"measured_union_ms"`
	GapUnionMS      int64      `json:"gap_union_ms"`
	OverlapMS       int64      `json:"overlap_ms"`
}

type stageTimelineInterval struct{ start, end time.Time }

const (
	stageTimelineKindParentAction = "parent-action"
	stageTimelineKindStartupStage = "execution-stage"
	stageTimelineKindModelCall    = "model-call"
	stageTimelineKindLifecycle    = "lifecycle-transition"

	stageTimelineGapParentWait   = "parent-wait"
	stageTimelineGapInAttempt    = "in-attempt-unmeasured"
	stageTimelineGapUnattributed = "unattributed-span"
	stageTimelineGapTailOpen     = "tail-open"

	stageTimelineCoverageProviderSend = "provider API送信時刻はCLI/SDKから観測できないためunknown。claude-process開始から最初のmodel activity eventまでを最近傍境界として扱う"
	stageTimelineCoverageParentTurn   = "親Codexのmodel turn・tool往復はこのstateの収集境界外。parent-wait gapは待機実測であって内訳ではない"
)

func PrintStageTimeline(st *state.StateStore, taskIDArg string, stdout io.Writer) error {
	explicit := taskIDArg != ""
	taskID := taskIDArg
	if taskID == "" {
		taskID = st.ReadOr("task.id", "")
	}
	if !validTimelineTaskID(taskID, explicit) {
		return &machinecli.UsageError{Message: fmt.Sprintf("task IDが生成されるUUID v4形式と一致しません: %q", taskID)}
	}
	if taskID == "" {
		return &machinecli.UsageError{Message: "stage timelineにはtask IDが必要です: glm-worker --stage-timeline <task-id>"}
	}
	return machinecli.WriteJSON(stdout, buildStageTimeline(st, taskID, explicit))
}

func buildStageTimeline(st *state.StateStore, taskID string, explicit bool) StageTimelineOutput {
	attempts, attemptsErr := st.ReadParentActionAttempts()
	modelLogs, telemetryErr := readTimelineTelemetry(st, taskID)
	stages, stageErr := st.ReadStageLogs(taskID)
	logs := append(append([]state.ModelCallLog(nil), modelLogs...), stages...)
	lifecycle, lifecycleErr := state.ReadTaskLifecycle(st.TaskLifecycleLogPath(taskID))
	events, _, eventsErr := ReadTaskEventRecords(st, taskID)
	archive, archiveErr := st.ReadStartupStageArchive()

	generatedAt := time.Now().UTC()
	attributed, unattributed := splitAttempts(attempts, taskID)
	attributed, unattributed = correlateStageAttempts(attributed, unattributed, logs)
	entries := buildStageTimelineEntries(attributed, unattributed, logs, lifecycle, archive, events)
	validationEntries, validationSource := stageTimelineValidationEntries(st, taskID)
	entries = append(entries, validationEntries...)
	for i := range entries {
		if strings.HasPrefix(entries[i].Locator, "startup-stage-archive.jsonl#") {
			entries[i].Locator = st.Path(entries[i].Locator)
		}
		if entries[i].Locator == "" {
			entries[i].Locator = stageEntryLocator(st, taskID, entries[i])
		}
	}
	sortStageTimelineEntries(entries)
	wall := stageTimelineWallFromEntries(entries, generatedAt)
	gaps := stageTimelineGaps(entries, wall, attributed, unattributed, generatedAt)
	wall.GapUnionMS = stageTimelineGapUnionMS(gaps)

	output := StageTimelineOutput{
		TaskID:               taskID,
		TaskStatus:           timelineTaskStatus(st, taskID, explicit),
		GeneratedAt:          generatedAt,
		Sources:              stageTimelineSourceStates(st, taskID, attemptsErr, telemetryErr, lifecycleErr, eventsErr, archiveErr, attempts, modelLogs, lifecycle, events, archive),
		Entries:              entries,
		Gaps:                 gaps,
		InnerGaps:            stageTimelineInnerGaps(entries, attributed),
		NotRunStages:         stageTimelineNotRunStages(attributed, logs, archive),
		UnattributedAttempts: stageTimelineAttemptViews(unattributed),
		Coverage: stageTimelineCoverage{
			ProviderSendBoundary: stageTimelineCoverageProviderSend,
			ParentTurnBoundary:   stageTimelineCoverageParentTurn,
			StartupStageSequence: state.StartupStageSequence,
		},
		Wall:       wall,
		Slowest:    slowestStageEntries(entries),
		Overhead:   stageObservationOverhead(logs),
		ToolTotals: timelineTools(state.SumCallTimelineTools(state.CallsFromTaskEvents(events))),
	}
	completeStageTimeline(&output, logs)
	if st.ReadOr("task.id", "") == taskID {
		output.TaskBaseline = st.BaselineEvidence()
	}
	output.Sources.Validations = validationSource
	output.Sources.Stages = stageTimelineOptionalSourceState(stageErr, st.TaskStageLogPath(taskID), len(stages))
	return output
}

func completeStageTimeline(output *StageTimelineOutput, logs []state.ModelCallLog) {
	for _, log := range logs {
		if log.CallType == state.CallTypeTask || log.CallType == state.CallTypeProbe {
			output.ModelCalls++
		}
	}
	if output.Wall.WallMS > 0 {
		for i := range output.Slowest {
			share := float64(*output.Slowest[i].DurationMS) / float64(output.Wall.WallMS)
			output.Slowest[i].WallShare = &share
		}
	}
	output.Coverage.Status = TimelineStatusPartial
	output.Coverage.Missing = []string{"provider-send", "parent-transcript-final-report", "CI実行時間（別source）"}
}

func splitAttempts(attempts []state.ParentActionAttemptRecord, taskID string) (attributed, unattributed []state.ParentActionAttemptRecord) {
	positions := map[string]int{}
	var merged []state.ParentActionAttemptRecord
	for _, attempt := range attempts {
		key := attempt.AttemptID + ":" + attempt.Action
		if index, ok := positions[key]; ok {
			merged[index] = attempt
		} else {
			positions[key] = len(merged)
			merged = append(merged, attempt)
		}
	}
	for _, attempt := range merged {
		switch attempt.TaskID {
		case taskID:
			attributed = append(attributed, attempt)
		case "":
			unattributed = append(unattributed, attempt)
		}
	}
	return attributed, unattributed
}

func buildStageTimelineEntries(
	attributed, _ []state.ParentActionAttemptRecord,
	logs []state.ModelCallLog,
	lifecycle []state.TaskLifecycleRecord,
	archive []state.ModelCallLog,
	events []state.TaskEventRecord,
) []stageTimelineEntry {
	var overlapping []state.ParentActionAttemptRecord
	entries := make([]stageTimelineEntry, 0, len(attributed)+len(logs)+len(lifecycle))
	for _, attempt := range append(append([]state.ParentActionAttemptRecord(nil), attributed...), overlapping...) {
		entries = append(entries, stageTimelineAttemptEntry(attempt))
	}
	logs = finalizedStageLogs(logs)
	for _, log := range logs {
		if entry, ok := stageTimelineLogEntry(log, events); ok {
			entries = append(entries, entry)
		}
	}
	for _, record := range finalizedStageLogs(archive) {
		entry, ok := stageTimelineLogEntry(record, events)
		if !ok {
			continue
		}
		if stageTimelineArchiveCorrelated(entry, attributed, overlapping) {
			entry.Locator = "startup-stage-archive.jsonl#call_id=" + record.CallID + ";attempt_id=" + entry.AttemptID
			entries = append(entries, entry)
		}
	}
	for _, record := range lifecycle {
		entries = append(entries, stageTimelineLifecycleEntry(record))
	}
	return entries
}

func stageTimelineAttemptEntry(attempt state.ParentActionAttemptRecord) stageTimelineEntry {
	entry := stageTimelineEntry{
		Kind:      stageTimelineKindParentAction,
		Name:      attempt.Action,
		AttemptID: attempt.AttemptID,
		Outcome:   attempt.Outcome,
	}
	startedAt := attempt.EnteredAt
	entry.StartedAt = &startedAt
	if !attempt.ExitedAt.IsZero() {
		completedAt := attempt.ExitedAt
		entry.CompletedAt = &completedAt
		duration := attempt.DurationMS
		entry.DurationMS = &duration
	}
	return entry
}

func stageTimelineLogEntry(log state.ModelCallLog, events []state.TaskEventRecord) (stageTimelineEntry, bool) {
	switch log.CallType {
	case state.CallTypeStage:
		if log.Stage == nil {
			return stageTimelineEntry{}, false
		}
		entry := stageTimelineEntry{
			Kind:        stageTimelineKindStartupStage,
			Name:        log.Stage.Stage,
			AttemptID:   log.Stage.AttemptID,
			Outcome:     log.Outcome,
			ClockOrigin: log.Stage.ClockOrigin,
			CallID:      log.Stage.ModelCallID,
			RunID:       log.CallID,
		}
		startedAt := log.StartedAt
		completedAt := log.CompletedAt
		entry.StartedAt = &startedAt
		if !completedAt.IsZero() && !completedAt.Before(startedAt) {
			entry.CompletedAt = &completedAt
			duration := log.WallDurationMS
			entry.DurationMS = &duration
		}
		return entry, true
	case state.CallTypeTask, state.CallTypeProbe:
		entry := stageTimelineEntry{
			Kind:         stageTimelineKindModelCall,
			Name:         log.Phase,
			CallID:       log.CallID,
			Role:         string(log.Role),
			Outcome:      log.Outcome,
			RetryOf:      log.RetryOf,
			FailureClass: log.ProviderClassification,
		}
		startedAt := log.StartedAt
		completedAt := log.CompletedAt
		entry.StartedAt = &startedAt
		if !completedAt.IsZero() {
			entry.CompletedAt = &completedAt
			duration := log.WallDurationMS
			entry.DurationMS = &duration
		}
		if log.Runtime != nil {
			entry.WorkerRevision = log.Runtime.WorkerRevision
		}
		if log.Snapshot != nil && log.Snapshot.Current != nil {
			entry.SourceHead = log.Snapshot.Current.Head
			entry.SourceDigest = log.Snapshot.Current.WorktreeDigest
		}
		firstEventAt, firstModelActivityAt := callEventBoundaries(events, log.CallID)
		entry.FirstEventAt = firstEventAt
		entry.FirstModelActivityAt = firstModelActivityAt
		entry.FirstCLIInitAt = callInitBoundary(events, log.CallID)
		return entry, true
	default:
		return stageTimelineEntry{}, false
	}
}

func stageTimelineArchiveCorrelated(entry stageTimelineEntry, attributed, overlapping []state.ParentActionAttemptRecord) bool {
	for _, attempt := range append(append([]state.ParentActionAttemptRecord(nil), attributed...), overlapping...) {
		if attempt.AttemptID == entry.AttemptID {
			return true
		}
	}
	return false
}

func callEventBoundaries(events []state.TaskEventRecord, callID string) (*time.Time, *time.Time) {
	var firstEvent, firstModelActivity *time.Time
	for _, record := range events {
		if record.CallID != callID {
			continue
		}
		if firstEvent == nil || record.Timestamp.Before(*firstEvent) {
			timestamp := record.Timestamp
			firstEvent = &timestamp
		}
		if state.IsModelActivityEvent(record) && (firstModelActivity == nil || record.Timestamp.Before(*firstModelActivity)) {
			timestamp := record.Timestamp
			firstModelActivity = &timestamp
		}
	}
	return firstEvent, firstModelActivity
}

func stageTimelineLifecycleEntry(record state.TaskLifecycleRecord) stageTimelineEntry {
	entry := stageTimelineEntry{
		Kind:    stageTimelineKindLifecycle,
		Name:    record.From + "->" + record.To,
		Outcome: record.To,
	}
	timestamp := record.Timestamp
	entry.StartedAt = &timestamp
	return entry
}

func sortStageTimelineEntries(entries []stageTimelineEntry) {
	sort.SliceStable(entries, func(i, j int) bool {
		left, right := entries[i], entries[j]
		if left.StartedAt == nil || right.StartedAt == nil {
			return false
		}
		if !left.StartedAt.Equal(*right.StartedAt) {
			return left.StartedAt.Before(*right.StartedAt)
		}
		return left.Index < right.Index
	})
	for index := range entries {
		entries[index].Index = index + 1
	}
}

func entryInterval(entry stageTimelineEntry) (stageTimelineInterval, bool) {
	if entry.StartedAt == nil || entry.StartedAt.IsZero() {
		return stageTimelineInterval{}, false
	}
	end := *entry.StartedAt
	if entry.CompletedAt != nil && entry.CompletedAt.After(end) {
		end = *entry.CompletedAt
	}
	return stageTimelineInterval{start: *entry.StartedAt, end: end}, true
}

func stageTimelineWallFromEntries(entries []stageTimelineEntry, generatedAt time.Time) stageTimelineWall {
	if len(entries) == 0 {
		return stageTimelineWall{}
	}
	var start, end *time.Time
	intervals := make([]stageTimelineInterval, 0, len(entries))
	for _, entry := range entries {
		interval, ok := entryInterval(entry)
		if !ok {
			continue
		}
		intervals = append(intervals, interval)
		if start == nil || interval.start.Before(*start) {
			startedAt := interval.start
			start = &startedAt
		}
		if end == nil || interval.end.After(*end) {
			endedAt := interval.end
			end = &endedAt
		}
	}
	if start == nil || end == nil {
		return stageTimelineWall{}
	}
	if end.After(generatedAt) {
		end = &generatedAt
	}
	wall := stageTimelineWall{Start: start, End: end, WallMS: end.Sub(*start).Milliseconds()}
	wall.MeasuredUnionMS = intervalUnionMS(intervals)
	var summed int64
	for _, interval := range intervals {
		summed += interval.end.Sub(interval.start).Milliseconds()
	}
	wall.OverlapMS = summed - wall.MeasuredUnionMS
	return wall
}

func stageTimelineGaps(
	entries []stageTimelineEntry,
	wall stageTimelineWall,
	attributed, unattributed []state.ParentActionAttemptRecord,
	_ time.Time,
) []stageTimelineGap {
	var gaps []stageTimelineGap
	var previous *stageTimelineEntry
	for index := range entries {
		current := entries[index]
		currentInterval, ok := entryInterval(current)
		if !ok {
			continue
		}
		if previous != nil {
			previousInterval, _ := entryInterval(*previous)
			if currentInterval.start.After(previousInterval.end) {
				gapStart := previousInterval.end
				gapEnd := currentInterval.start
				gaps = append(gaps, stageTimelineGap{
					AfterIndex:     previous.Index,
					BeforeIndex:    current.Index,
					Classification: classifyStageTimelineGap(gapStart, gapEnd, attributed, unattributed),
					StartedAt:      gapStart,
					EndedAt:        gapEnd,
					DurationMS:     gapEnd.Sub(gapStart).Milliseconds(),
				})
			}
		}
		if previous == nil {
			previous = &entries[index]
		} else {
			furthest, _ := entryInterval(*previous)
			if currentInterval.end.After(furthest.end) {
				previous = &entries[index]
			}
		}
	}
	applyStageTimelineWallShare(gaps, wall.WallMS)
	return gaps
}

func classifyStageTimelineGap(start, end time.Time, attributed, _ []state.ParentActionAttemptRecord) string {
	attempts := attributed
	endedBeforeGap := false
	startsAfterGap := false
	for _, attempt := range attempts {
		if !start.Before(attempt.EnteredAt) && !end.After(attempt.ExitedAt) {
			return stageTimelineGapInAttempt
		}
		if !attempt.ExitedAt.After(start) {
			endedBeforeGap = true
		}
		if !attempt.EnteredAt.Before(end) {
			startsAfterGap = true
		}
	}
	if endedBeforeGap && startsAfterGap {
		return stageTimelineGapParentWait
	}
	return stageTimelineGapUnattributed
}

func applyStageTimelineWallShare(gaps []stageTimelineGap, wallMS int64) {
	if wallMS <= 0 {
		return
	}
	for index := range gaps {
		gaps[index].WallShare = float64(gaps[index].DurationMS) / float64(wallMS)
	}
}

func stageTimelineNotRunStages(attributed []state.ParentActionAttemptRecord, logs, archive []state.ModelCallLog) []stageTimelineNotRun {
	observed := map[string]map[string]struct{}{}
	for _, log := range append(append([]state.ModelCallLog(nil), logs...), archive...) {
		if log.CallType != state.CallTypeStage || log.Stage == nil {
			continue
		}
		if observed[log.Stage.AttemptID] == nil {
			observed[log.Stage.AttemptID] = map[string]struct{}{}
		}
		observed[log.Stage.AttemptID][log.Stage.Stage] = struct{}{}
	}
	var result []stageTimelineNotRun
	for _, attempt := range attributed {
		stages := observed[attempt.AttemptID]
		if len(stages) == 0 {
			continue
		}
		notRun := unobservedStartupStages(stages)
		if len(notRun) == 0 {
			continue
		}
		result = append(result, stageTimelineNotRun{
			AttemptID: attempt.AttemptID,
			Action:    attempt.Action,
			Outcome:   "unknown: 記録がないstageの未実行は推定しない",
			NotRun:    notRun,
		})
	}
	return result
}

func stageTimelineAttemptViews(unattributed []state.ParentActionAttemptRecord) []stageTimelineAttemptView {
	views := make([]stageTimelineAttemptView, 0, len(unattributed))
	for _, attempt := range unattributed {
		views = append(views, stageTimelineAttemptView{
			AttemptID:  attempt.AttemptID,
			Action:     attempt.Action,
			Outcome:    attempt.Outcome,
			EnteredAt:  attempt.EnteredAt,
			ExitedAt:   attempt.ExitedAt,
			DurationMS: attempt.DurationMS,
		})
	}
	return views
}

func stageTimelineSourceStates(
	st *state.StateStore,
	taskID string,
	attemptsErr, telemetryErr, lifecycleErr, eventsErr, archiveErr error,
	attempts []state.ParentActionAttemptRecord,
	logs []state.ModelCallLog,
	lifecycle []state.TaskLifecycleRecord,
	events []state.TaskEventRecord,
	archive []state.ModelCallLog,
) stageTimelineSources {
	return stageTimelineSources{
		Telemetry: stageTimelineSourceState(telemetryErr, st.ModelCallLogPath(taskID), len(logs)),
		Attempts:  stageTimelineOptionalSourceState(attemptsErr, st.Path("parent-action-attempts.jsonl"), len(attempts)),
		Lifecycle: stageTimelineSourceState(lifecycleErr, st.TaskLifecycleLogPath(taskID), len(lifecycle)),
		Events:    stageTimelineSourceState(eventsErr, st.TaskEventLogPath(taskID), len(events)),
		Archive:   stageTimelineOptionalSourceState(archiveErr, st.Path("startup-stage-archive.jsonl"), len(archive)),
	}
}

func stageTimelineSourceState(err error, path string, records int) timelineSourceState {
	status := TimelineSourceOK
	if err != nil {
		status = taskview.StatusUnreadable
		if errors.Is(err, os.ErrNotExist) {
			status = taskview.StatusNone
		}
	}
	return timelineSourceState{Status: status, Path: path, Records: records}
}

func stageTimelineOptionalSourceState(err error, path string, records int) timelineSourceState {
	source := stageTimelineSourceState(err, path, records)
	if err == nil && records == 0 {
		source.Status = taskview.StatusNone
	}
	return source
}

func stageTimelineGapUnionMS(gaps []stageTimelineGap) int64 {
	intervals := make([]stageTimelineInterval, 0, len(gaps))
	for _, gap := range gaps {
		intervals = append(intervals, stageTimelineInterval{start: gap.StartedAt, end: gap.EndedAt})
	}
	return intervalUnionMS(intervals)
}

func intervalUnionMS(intervals []stageTimelineInterval) int64 {
	if len(intervals) == 0 {
		return 0
	}
	sorted := append([]stageTimelineInterval(nil), intervals...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].start.Before(sorted[j].start) })
	var total int64
	current := sorted[0]
	for _, interval := range sorted[1:] {
		if interval.start.After(current.end) {
			total += current.end.Sub(current.start).Milliseconds()
			current = interval
			continue
		}
		if interval.end.After(current.end) {
			current.end = interval.end
		}
	}
	total += current.end.Sub(current.start).Milliseconds()
	return total
}
