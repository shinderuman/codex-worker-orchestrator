package state

import (
	"encoding/json"
	"fmt"
	"os"
	"time"
	"unicode/utf8"
)

type StageTiming struct {
	Stage         string `json:"stage"`
	AttemptID     string `json:"attempt_id,omitempty"`
	ClockOrigin   string `json:"clock_origin"`
	ModelCallID   string `json:"model_call_id,omitempty"`
	WriteWallNS   int64  `json:"write_wall_ns,omitempty"`
	WriteCalls    int    `json:"write_calls,omitempty"`
	WriteFailures int    `json:"write_failures,omitempty"`
}

type StageRecorder struct {
	store           *StateStore
	attemptID       string
	parentEnteredAt time.Time
	parentBound     bool
	taskID          string
	buffered        []ModelCallLog
	modelCallID     string
	writeWallNS     int64
	writeCalls      int
	writeFailures   int
	clockOrigin     string
}

type StageSpan struct {
	recorder  *StageRecorder
	stage     string
	startedAt time.Time
	id        string
}

const (
	CallTypeStage = "stage"

	StageOutcomeCompleted = "completed"
	StageOutcomeFailed    = "failed"
	StageOutcomeNotRun    = "not-run"
	StageOutcomeStarted   = "started"

	ClockOriginWorker           = "worker"
	ClockOriginParentToWorker   = "parent-worker-wall"
	StagePhasePrefix            = "execution-"
	stageTimingErrorMaxBytes    = 256
	startupStageArchiveFile     = "startup-stage-archive.jsonl"
	ParentActionAttemptIDEnv    = "GLM_PARENT_ACTION_ATTEMPT_ID"
	ParentActionAttemptEnterEnv = "GLM_PARENT_ACTION_ATTEMPT_ENTERED_AT"
	WorkerProcessLaunchEnv      = "GLM_WORKER_PROCESS_LAUNCHED_AT"
)

const (
	StageParentDispatch        = "parent-dispatch"
	StageWorkflowLock          = "workflow-lock"
	StageParentAdmission       = "parent-admission"
	StageControllerActivate    = "controller-activate"
	StageQualityPreflight      = "quality-preflight"
	StageTaskInit              = "task-init"
	StageGitQualityBaseline    = "git-quality-baseline"
	StageHarnessTaskResolution = "harness-task-resolution"
	StageExternalFeasibility   = "external-feasibility"
	StageRepoSearchContext     = "repo-search-context"
	StageModelCallAdmission    = "model-call-admission"
	StageRunnerSetup           = "runner-setup"
	StageClaudeProcess         = "claude-process"
)

var StartupStageSequence = []string{
	StageParentDispatch,
	StageWorkflowLock,
	StageParentAdmission,
	StageControllerActivate,
	StageQualityPreflight,
	StageTaskInit,
	StageGitQualityBaseline,
	StageHarnessTaskResolution,
	StageExternalFeasibility,
	StageRepoSearchContext,
	StageModelCallAdmission,
	StageRunnerSetup,
	StageClaudeProcess,
}

func NewStageRecorder(st *StateStore) *StageRecorder {
	attemptID := os.Getenv(ParentActionAttemptIDEnv)
	enteredAt, err := time.Parse(time.RFC3339Nano, os.Getenv(ParentActionAttemptEnterEnv))
	return &StageRecorder{
		store:           st,
		attemptID:       attemptID,
		parentEnteredAt: enteredAt,
		parentBound:     attemptID != "" && err == nil && ValidGeneratedUUID(attemptID),
		clockOrigin:     ClockOriginWorker,
	}
}

func (r *StageRecorder) SetClockOrigin(origin string) {
	if r != nil {
		r.clockOrigin = origin
	}
}

func (r *StageRecorder) ParentBound() bool {
	if r == nil {
		return false
	}
	return r.parentBound
}

func (r *StageRecorder) RecordParentDispatch(workerEnteredAt time.Time) {
	if r == nil || !r.parentBound {
		return
	}
	launch, err := time.Parse(time.RFC3339Nano, os.Getenv(WorkerProcessLaunchEnv))
	if err == nil && !launch.Before(r.parentEnteredAt) && !workerEnteredAt.Before(launch) {
		r.record(StageParentDispatch, StageOutcomeCompleted, r.parentEnteredAt, launch, "", ClockOriginParentToWorker)
		r.record("worker-bootstrap", StageOutcomeCompleted, launch, workerEnteredAt, "", ClockOriginParentToWorker)
		return
	}
	r.record(StageParentDispatch, StageOutcomeCompleted, r.parentEnteredAt, workerEnteredAt, "", ClockOriginParentToWorker)
}

func (r *StageRecorder) Span(stage string) StageSpan {
	if r == nil {
		return StageSpan{}
	}
	id, _ := NewUUID()
	span := StageSpan{recorder: r, stage: stage, startedAt: time.Now(), id: id}
	r.recordSpan(span, StageOutcomeStarted, time.Time{}, "")
	return span
}

func (r *StageRecorder) BindModelCallID(callID string) {
	if r != nil {
		r.modelCallID = callID
	}
}

func (r *StageRecorder) Measure(stage string, fn func() error) error {
	span := r.Span(stage)
	err := fn()
	span.End(err)
	return err
}

func (s StageSpan) End(err error) {
	if s.stage == "" {
		return
	}
	outcome := StageOutcomeCompleted
	errText := ""
	if err != nil {
		outcome = StageOutcomeFailed
		errText = "stage-failed"
	}
	s.recorder.recordSpan(s, outcome, time.Now(), errText)
}

func (s StageSpan) EndNotRun() {
	if s.stage == "" {
		return
	}
	s.recorder.recordSpan(s, StageOutcomeNotRun, s.startedAt, "")
}

func (r *StageRecorder) recordSpan(span StageSpan, outcome string, end time.Time, errText string) {
	entry := ModelCallLog{
		Version: ModelCallLogVersion, CallType: CallTypeStage, CallID: span.id,
		StartedAt: span.startedAt, CompletedAt: end, Phase: StagePhasePrefix + span.stage,
		Outcome: outcome, Error: errText,
		Stage: &StageTiming{Stage: span.stage, AttemptID: r.attemptID, ClockOrigin: r.clockOrigin, ModelCallID: r.modelCallID},
	}
	if !end.IsZero() {
		entry.WallDurationMS = end.Sub(span.startedAt).Milliseconds()
	}
	r.writeStage(entry)
}

func (r *StageRecorder) writeStage(entry ModelCallLog) {
	if entry.CallID == "" {
		id, err := NewUUID()
		if err != nil {
			r.writeFailures++
			warnStatsFailure("stage identityの生成", err)
			return
		}
		entry.CallID = id
	}
	if !ValidGeneratedUUID(r.taskID) {
		r.buffered = append(r.buffered, entry)
		return
	}
	entry.TaskID = r.taskID
	started := time.Now()
	err := r.store.AppendStageLog(entry)
	r.writeWallNS += time.Since(started).Nanoseconds()
	r.writeCalls++
	if err != nil {
		r.writeFailures++
		warnStatsFailure("stage telemetryの追記", err)
	}
}

func (r *StageRecorder) RebindStore(st *StateStore) {
	if r == nil {
		return
	}
	r.Flush()
	r.store = st
}

func (r *StageRecorder) BindTaskID(taskID string) {
	if r == nil || !ValidGeneratedUUID(taskID) {
		return
	}
	r.taskID = taskID
	r.Flush()
}

func (r *StageRecorder) BindCurrentTask() {
	if r == nil {
		return
	}
	r.BindTaskID(r.store.ReadOr("task.id", ""))
}

func (r *StageRecorder) Flush() {
	if r == nil || len(r.buffered) == 0 {
		return
	}
	if !ValidGeneratedUUID(r.taskID) {
		return
	}
	for index := range r.buffered {
		r.buffered[index].TaskID = r.taskID
		r.writeStage(r.buffered[index])
	}
	r.buffered = nil
}

func (r *StageRecorder) FlushOrArchive() {
	if r == nil {
		return
	}
	r.Flush()
	summary := ModelCallLog{
		Version: ModelCallLogVersion, CallType: CallTypeEvent, TaskID: r.taskID,
		Phase: "stage-observation-overhead", Outcome: StageOutcomeCompleted,
		Stage: &StageTiming{Stage: "observation-overhead", AttemptID: r.attemptID, ClockOrigin: r.clockOrigin,
			WriteWallNS: r.writeWallNS, WriteCalls: r.writeCalls, WriteFailures: r.writeFailures},
	}
	if ValidGeneratedUUID(r.taskID) {
		r.writeStage(summary)
	} else if len(r.buffered) > 0 {
		r.writeStage(summary)
	}
	if len(r.buffered) == 0 {
		return
	}
	if err := r.store.AppendStartupStageArchive(r.buffered); err != nil {
		warnStatsFailure("startup stage archiveの追記", err)
	}
	r.buffered = nil
}

func (r *StageRecorder) record(stage, outcome string, startedAt, completedAt time.Time, errText, clockOrigin string) {
	if completedAt.Before(startedAt) {
		outcome = "unknown-clock-order"
	}
	entry := ModelCallLog{
		Version:        ModelCallLogVersion,
		CallType:       CallTypeStage,
		StartedAt:      startedAt,
		CompletedAt:    completedAt,
		WallDurationMS: completedAt.Sub(startedAt).Milliseconds(),
		Phase:          StagePhasePrefix + stage,
		Outcome:        outcome,
		Stage:          &StageTiming{Stage: stage, AttemptID: r.attemptID, ClockOrigin: clockOrigin},
	}
	if errText != "" {
		entry.Error = boundedStageText(errText)
	}
	r.writeStage(entry)
}

func boundedStageText(value string) string {
	if len(value) <= stageTimingErrorMaxBytes {
		return value
	}
	prefix := "[前方を省略] "
	start := len(value) - (stageTimingErrorMaxBytes - len(prefix))
	for start < len(value) && !utf8.RuneStart(value[start]) {
		start++
	}
	return prefix + value[start:]
}

func (s *StateStore) AppendStartupStageArchive(records []ModelCallLog) error {
	for _, record := range records {
		record.Version = ModelCallLogVersion
		data, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("startup stage archiveをJSON化できません: %w", err)
		}
		if err := appendStateJSONL(s.Path(startupStageArchiveFile), data); err != nil {
			return err
		}
	}
	retained, err := s.ReadStartupStageArchive()
	if err != nil {
		return err
	}
	if len(retained) <= parentActionAttemptRecordKeep {
		return nil
	}
	var data []byte
	for _, record := range retained[len(retained)-parentActionAttemptRecordKeep:] {
		line, err := json.Marshal(record)
		if err != nil {
			return err
		}
		data = append(append(data, line...), '\n')
	}
	return writeFileAtomic(s.Path(startupStageArchiveFile), data, 0o600)
}

func (s *StateStore) ReadStartupStageArchive() ([]ModelCallLog, error) {
	return readTimingRecords(s.Path(startupStageArchiveFile), ModelCallLogVersion, func(record ModelCallLog) int { return record.Version })
}

func (s *StateStore) TaskStageLogPath(taskID string) string {
	return s.Path("execution-stages/" + taskID + ".jsonl")
}

func (s *StateStore) AppendStageLog(record ModelCallLog) error {
	if !ValidGeneratedUUID(record.TaskID) {
		return fmt.Errorf("stage logのtask identityが不正です")
	}
	record.Version = ModelCallLogVersion
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return appendStateJSONL(s.TaskStageLogPath(record.TaskID), data)
}

func (s *StateStore) ReadStageLogs(taskID string) ([]ModelCallLog, error) {
	return readTimingRecords(s.TaskStageLogPath(taskID), ModelCallLogVersion, func(record ModelCallLog) int { return record.Version })
}
