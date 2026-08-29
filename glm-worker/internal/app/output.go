package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/abeval"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type statusOutput struct {
	RepoRoot            *string                   `json:"repo_root"`
	RepositoryLock      *string                   `json:"repository_lock"`
	LockPID             *string                   `json:"lock_pid"`
	TaskID              *string                   `json:"task_id"`
	ArtifactDir         *string                   `json:"artifact_dir"`
	TaskStatus          *string                   `json:"task_status"`
	TaskLiveness        *string                   `json:"task_liveness"`
	WorkerSession       *string                   `json:"worker_session"`
	ReviewerSession     *string                   `json:"reviewer_session"`
	PendingDecision     bool                      `json:"pending_decision"`
	ParentReviewOpen    *string                   `json:"parent_review_open"`
	TaskStartedAt       *time.Time                `json:"task_started_at"`
	TaskElapsedMS       *int64                    `json:"task_elapsed_ms"`
	LastEvent           *state.TaskEventRecord    `json:"last_event"`
	LastEventAgeMS      *int64                    `json:"last_event_age_ms"`
	CurrentPhase        *string                   `json:"current_phase"`
	CurrentRole         *string                   `json:"current_role"`
	CurrentModel        *string                   `json:"current_model"`
	Probes              *statusProbes             `json:"probes"`
	RateLimited         statusRateLimit           `json:"rate_limited"`
	ProviderUnavailable statusProviderUnavailable `json:"provider_unavailable"`
	ResumeAvailable     bool                      `json:"resume_available"`
	Telemetry           *string                   `json:"telemetry"`
	SessionAging        []state.SessionAging      `json:"session_aging"`

	Isolation *statusIsolation `json:"isolation,omitempty"`

	IsolationOrigin *statusIsolationOrigin `json:"isolation_origin,omitempty"`
}

type statusIsolation struct {
	IsolationID string `json:"isolation_id"`
	Worktree    string `json:"worktree"`
	Branch      string `json:"branch"`
	TaskID      string `json:"origin_task_id"`
	RepoRoot    string `json:"origin_repo_root"`
	Head        string `json:"origin_head"`
	CreatedAt   string `json:"created_at"`
}

type statusIsolationOrigin struct {
	IsolationID    string `json:"isolation_id"`
	OriginRepoRoot string `json:"origin_repo_root"`
	OriginTaskID   string `json:"origin_task_id"`
	Branch         string `json:"branch"`
	CreatedAt      string `json:"created_at"`
}

type statusProbes struct {
	Count       int        `json:"count"`
	LastAt      *time.Time `json:"last_at"`
	LastAgeMS   *int64     `json:"last_age_ms"`
	LastOutcome *string    `json:"last_outcome"`
	LastAttempt int        `json:"last_attempt"`
}

type statusRateLimit struct {
	Limited        bool    `json:"limited"`
	Phase          string  `json:"phase,omitempty"`
	ResetAtCST     string  `json:"reset_at_cst,omitempty"`
	ResetAtRFC3339 *string `json:"reset_at_rfc3339,omitempty"`
}

type statusProviderUnavailable struct {
	Unavailable    bool    `json:"unavailable"`
	Phase          string  `json:"phase,omitempty"`
	Classification *string `json:"classification,omitempty"`
	Probes         int     `json:"probes,omitempty"`
	ElapsedMS      *int64  `json:"elapsed_ms,omitempty"`
}

type currentCallView struct {
	phase string
	role  string
	model string
}

type statsOutput struct {
	Tasks                                   int                 `json:"tasks"`
	ModelCalls                              int                 `json:"model_calls"`
	ModelCallsByAlias                       map[string]int      `json:"model_calls_by_alias"`
	ProbeCalls                              int                 `json:"probe_calls"`
	TotalAICalls                            int                 `json:"total_ai_calls"`
	TelemetryCoverage                       statsCoverage       `json:"telemetry_coverage"`
	ModelDurationMSByAlias                  map[string]int64    `json:"model_duration_ms_by_alias"`
	InputTokensByAlias                      map[string]int64    `json:"input_tokens_by_alias"`
	CacheCreationInputTokensByAlias         map[string]int64    `json:"cache_creation_input_tokens_by_alias"`
	CacheReadInputTokensByAlias             map[string]int64    `json:"cache_read_input_tokens_by_alias"`
	TotalPromptTokensByAlias                map[string]int64    `json:"total_prompt_tokens_by_alias"`
	OutputTokensByAlias                     map[string]int64    `json:"output_tokens_by_alias"`
	TopLevelTurnsByAlias                    map[string]int      `json:"top_level_turns_by_alias"`
	CallTreesByResolvedModel                map[string]int      `json:"call_trees_by_resolved_model"`
	InputTokensByResolvedModel              map[string]int64    `json:"input_tokens_by_resolved_model"`
	CacheCreationInputTokensByResolvedModel map[string]int64    `json:"cache_creation_input_tokens_by_resolved_model"`
	CacheReadInputTokensByResolvedModel     map[string]int64    `json:"cache_read_input_tokens_by_resolved_model"`
	OutputTokensByResolvedModel             map[string]int64    `json:"output_tokens_by_resolved_model"`
	WorkerCalls                             int                 `json:"worker_calls"`
	ReviewerCalls                           int                 `json:"reviewer_calls"`
	DecisionCommands                        int                 `json:"decision_commands"`
	FixCommands                             int                 `json:"fix_commands"`
	ResumeCommands                          int                 `json:"resume_commands"`
	TransientRetries                        int                 `json:"transient_retries"`
	AutoFixRounds                           int                 `json:"auto_fix_rounds"`
	NeedsSolDecisionPackets                 int                 `json:"needs_sol_decision_packets"`
	NeedsSolReviewPackets                   int                 `json:"needs_sol_review_packets"`
	PassPackets                             int                 `json:"pass_packets"`
	RateLimits                              int                 `json:"rate_limits"`
	RateLimitsByAlias                       map[string]int      `json:"rate_limits_by_alias"`
	ProviderUnavailable                     int                 `json:"provider_unavailable"`
	ProviderUnavailableByAlias              map[string]int      `json:"provider_unavailable_by_alias"`
	PacketCompactions                       int                 `json:"packet_compactions"`
	RiskFloorByCategory                     map[string]int      `json:"risk_floor_by_category"`
	SnapshotMismatches                      int                 `json:"snapshot_mismatches"`
	SnapshotMismatchByAxis                  map[string]int      `json:"snapshot_mismatch_by_axis"`
	PacketRejectByCategory                  map[string]int      `json:"packet_reject_by_category"`
	ProbeOutcome                            map[string]int      `json:"probe_outcome"`
	ParentOutcomes                          map[string]int      `json:"parent_outcomes"`
	ParentFixOrigins                        map[string]int      `json:"parent_fix_origins"`
	ParentOutcomesByModel                   map[string]int      `json:"parent_outcomes_by_model"`
	ParentOutcomesByRisk                    map[string]int      `json:"parent_outcomes_by_risk"`
	ParentFixRework                         []statsParentRework `json:"parent_fix_rework"`
	ParentFixReworkCoverage                 string              `json:"parent_fix_rework_coverage"`
	SolPacketBytes                          int                 `json:"sol_packet_bytes"`
	TelemetryDir                            string              `json:"telemetry_dir"`
	CurrentTask                             statsCurrentTask    `json:"current_task"`
}

type statsCoverage struct {
	Status        string              `json:"status"`
	StatsCalls    int                 `json:"stats_calls"`
	RawRecords    int                 `json:"raw_records"`
	MissingCalls  int                 `json:"missing_calls"`
	ExcessRecords int                 `json:"excess_records"`
	OrphanFiles   int                 `json:"orphan_files"`
	UsageKnown    bool                `json:"usage_totals_known"`
	Tasks         []statsCoverageTask `json:"tasks"`
}

type statsCoverageTask struct {
	TaskID         string `json:"task_id"`
	Classification string `json:"classification"`
	StatsCalls     int    `json:"stats_calls"`
	RawRecords     int    `json:"raw_records"`
	MissingCalls   int    `json:"missing_calls"`
	ExcessRecords  int    `json:"excess_records"`
}

type statsParentRework struct {
	Origin           string `json:"origin"`
	Calls            int    `json:"calls"`
	WorkerCalls      int    `json:"worker_calls"`
	ReviewerCalls    int    `json:"reviewer_calls"`
	Turns            int    `json:"turns"`
	TreeInputTokens  int64  `json:"tree_input_tokens"`
	TreeOutputTokens int64  `json:"tree_output_tokens"`
	WallDurationMS   int64  `json:"wall_duration_ms"`
}

type statsCurrentTask struct {
	ID          *string `json:"id"`
	Status      *string `json:"status"`
	ArtifactDir *string `json:"artifact_dir"`
}

type resetOutput struct {
	Status   string  `json:"status"`
	RepoRoot *string `json:"repo_root"`
}

type acceptOutput struct {
	Accepted bool `json:"accepted"`
}

type VerificationError struct {
	Outcome autoresume.Outcome
	Reason  string
}

type verifyAutoResumeOutput struct {
	AutomationKey  string `json:"automation_key"`
	TargetThread   string `json:"target_thread"`
	ExpectedAtUTC  string `json:"expected_at_utc"`
	TOMLDTStart    string `json:"toml_dtstart"`
	DBNextRunAtUTC string `json:"db_next_run_at_utc"`
}

type checkWakeCoalesceOutput struct {
	Decision         string `json:"decision"`
	Reason           string `json:"reason"`
	ParentThread     string `json:"parent_thread"`
	ResumeAtUTC      string `json:"resume_at_utc"`
	WakeAutomationID string `json:"wake_automation_id"`
	WakeThread       string `json:"wake_thread"`
	WakeNextRunUTC   string `json:"wake_next_run_at_utc"`
	AddedWaitSeconds int64  `json:"added_wait_seconds"`
}

const statusUnreadable = "unreadable"

func writeJSON(w io.Writer, value any) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return err
	}
	_, err := w.Write(buf.Bytes())
	return err
}

func stringPtr(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func msPtr(d time.Duration) *int64 {
	if d < 0 {
		d = 0
	}
	ms := d.Milliseconds()
	return &ms
}

func printStatus(st *state.StateStore, stdout io.Writer) error {
	taskID := st.ReadOr("task.id", "")
	logs, logErr := readStatusTelemetry(st, taskID)
	output := buildStatusOutput(st, taskID, logs, logErr)
	return writeJSON(stdout, output)
}

func buildStatusOutput(st *state.StateStore, taskID string, logs []state.ModelCallLog, logErr error) statusOutput {
	probe := ProbeRepoLock(st.LockPath())
	taskStatus := st.TaskStatus()
	output := statusOutput{
		RepoRoot:        stringPtr(st.ReadOr("repo-root", "")),
		RepositoryLock:  lockStatePtr(probe.State),
		LockPID:         lockPIDPtr(probe.PID),
		TaskID:          stringPtr(taskID),
		TaskStatus:      taskStatusPtr(taskStatus),
		WorkerSession:   stringPtr(st.ReadOr("worker.id", "")),
		ReviewerSession: stringPtr(st.ReadOr("reviewer.id", "")),
		PendingDecision: st.Exists("pending-decision"),
	}
	if taskID != "" {
		output.ArtifactDir = stringPtr(st.ArtifactDir(taskID))
	}
	if taskStatus == state.TaskStatusActive {
		output.TaskLiveness = taskLiveness(probe)
	}
	if label := st.OpenParentReviewLabel(); label != statusNone {
		output.ParentReviewOpen = stringPtr(label)
	}

	fillStatusTaskDetail(st, taskID, &output)
	output.ResumeAvailable = fillStatusCheckpoint(st, &output)
	fillStatusIsolation(st, &output)
	output.Probes = statusProbesDetail(logs, time.Now())
	fillStatusTelemetry(taskID, logErr, logs, &output)
	return output
}

func fillStatusIsolation(st *state.StateStore, output *statusOutput) {
	if record, err := st.LoadIsolationRecord(); err == nil {
		output.Isolation = &statusIsolation{
			IsolationID: record.IsolationID,
			Worktree:    record.Worktree,
			Branch:      record.Branch,
			TaskID:      record.OriginTaskID,
			RepoRoot:    record.OriginRepoRoot,
			Head:        record.OriginHead,
			CreatedAt:   record.CreatedAt,
		}
	}
	if origin, err := st.LoadIsolationOrigin(); err == nil {
		output.IsolationOrigin = &statusIsolationOrigin{
			IsolationID:    origin.IsolationID,
			OriginRepoRoot: origin.OriginRepoRoot,
			OriginTaskID:   origin.OriginTaskID,
			Branch:         origin.Branch,
			CreatedAt:      origin.CreatedAt,
		}
	}
}

func lockStatePtr(lockState LockState) *string {
	if lockState == LockUnknown {
		return nil
	}
	value := string(lockState)
	return &value
}

func taskStatusPtr(status state.TaskStatus) *string {
	switch status {
	case state.TaskStatusActive,
		state.TaskStatusWaitingDecision,
		state.TaskStatusWaitingSolReview,
		state.TaskStatusComplete,
		state.TaskStatusRateLimited,
		state.TaskStatusProviderUnavailable,
		state.TaskStatusGuardRecoverable,
		state.TaskStatusInterrupted:
		value := string(status)
		return &value
	}
	return nil
}

func lockPIDPtr(pid string) *string {
	if pid == "" || pid == statusNone || pid == "unknown" {
		return nil
	}
	return &pid
}

func fillStatusTaskDetail(st *state.StateStore, taskID string, output *statusOutput) {
	if stats, err := st.CurrentTaskStats(); err == nil && !stats.StartedAt.IsZero() {
		startedAt := stats.StartedAt
		output.TaskStartedAt = &startedAt
		output.TaskElapsedMS = msPtr(time.Since(startedAt))
	}

	current := currentCallView{}
	if last, ok := lastTaskEvent(st, taskID); ok {
		current = currentCallView{phase: last.Phase, role: last.Role, model: last.ModelAlias}
		if current.model == "" {
			current.model = last.MessageModel
		}
		output.LastEvent = &last
		if !last.Timestamp.IsZero() {
			output.LastEventAgeMS = msPtr(time.Since(last.Timestamp))
		}
	} else if checkpoint, err := st.LoadResumeCheckpoint(); err == nil {
		current = currentCallView{phase: checkpoint.Phase, role: string(checkpoint.Role), model: checkpoint.Model}
	}
	output.CurrentPhase = stringPtr(current.phase)
	output.CurrentRole = stringPtr(current.role)
	output.CurrentModel = stringPtr(current.model)
}

func fillStatusCheckpoint(st *state.StateStore, output *statusOutput) bool {
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return false
	}
	if checkpoint.RateLimited {
		output.RateLimited = statusRateLimit{
			Limited:        true,
			Phase:          checkpoint.Phase,
			ResetAtCST:     checkpoint.ResetAtCST,
			ResetAtRFC3339: stringPtr(checkpoint.ResetAtRFC3339),
		}
	}
	if checkpoint.ProviderUnavailable {
		elapsed := (*int64)(nil)
		if !checkpoint.ProviderUnavailableStartedAt.IsZero() {
			elapsed = msPtr(time.Since(checkpoint.ProviderUnavailableStartedAt))
		}
		output.ProviderUnavailable = statusProviderUnavailable{
			Unavailable:    true,
			Phase:          checkpoint.Phase,
			Classification: stringPtr(checkpoint.ProviderUnavailableClassification),
			Probes:         checkpoint.ProviderUnavailableProbes,
			ElapsedMS:      elapsed,
		}
	}
	return checkpoint.RateLimited || checkpoint.ProviderUnavailable || checkpoint.UserInterrupted || checkpoint.GuardRecoverable
}

func statusProbesDetail(logs []state.ModelCallLog, now time.Time) *statusProbes {
	probes := make([]state.ModelCallLog, 0)
	for _, log := range logs {
		if log.CallType == state.CallTypeProbe {
			probes = append(probes, log)
		}
	}
	if len(probes) == 0 {
		return nil
	}
	last := probes[len(probes)-1]
	detail := statusProbes{
		Count:       len(probes),
		LastOutcome: stringPtr(last.Outcome),
		LastAttempt: last.ProbeAttempt,
	}
	if !last.CompletedAt.IsZero() {
		completedAt := last.CompletedAt
		detail.LastAt = &completedAt
		detail.LastAgeMS = msPtr(now.Sub(completedAt))
	}
	return &detail
}

func fillStatusTelemetry(taskID string, logErr error, logs []state.ModelCallLog, output *statusOutput) {
	if taskID == "" {
		return
	}
	if logErr != nil {
		unreadable := statusUnreadable
		output.Telemetry = &unreadable
		return
	}
	ok := "ok"
	output.Telemetry = &ok
	output.SessionAging = state.AgingFromModelCallLogs(logs)
}

func readStatusTelemetry(st *state.StateStore, taskID string) ([]state.ModelCallLog, error) {
	if taskID == "" {
		return nil, nil
	}
	logs, err := st.ReadModelCallLogs(taskID)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return logs, nil
}

func lastTaskEvent(st *state.StateStore, taskID string) (state.TaskEventRecord, bool) {
	if taskID == "" {
		return state.TaskEventRecord{}, false
	}
	return readLastTaskEvent(st.TaskEventLogPath(taskID))
}

func taskLiveness(probe LockProbe) *string {
	switch probe.State {
	case LockHeld:
		running := "running"
		return &running
	case LockFree:
		stale := "stale"
		return &stale
	default:
		return nil
	}
}

func printStats(st *state.StateStore, stdout io.Writer) error {
	all, err := st.AllTaskStats()
	if err != nil {
		return err
	}
	output := buildStatsOutput(st, all)
	return writeJSON(stdout, output)
}

func newAggregateTaskStats() state.TaskStats {
	return state.TaskStats{
		ModelCallsByAlias:                       map[string]int{},
		ModelDurationMSByAlias:                  map[string]int64{},
		RateLimitsByAlias:                       map[string]int{},
		InputTokensByAlias:                      map[string]int64{},
		CacheCreationInputTokensByAlias:         map[string]int64{},
		CacheReadInputTokensByAlias:             map[string]int64{},
		OutputTokensByAlias:                     map[string]int64{},
		TopLevelTurnsByAlias:                    map[string]int{},
		CallTreesByResolvedModel:                map[string]int{},
		InputTokensByResolvedModel:              map[string]int64{},
		CacheCreationInputTokensByResolvedModel: map[string]int64{},
		CacheReadInputTokensByResolvedModel:     map[string]int64{},
		OutputTokensByResolvedModel:             map[string]int64{},
		ProviderUnavailableByAlias:              map[string]int{},
		RiskFloorByCategory:                     map[string]int{},
		SnapshotMismatchByAxis:                  map[string]int{},
		PacketRejectByCategory:                  map[string]int{},
		ProbeOutcome:                            map[string]int{},
		ParentOutcomes:                          map[string]int{},
		ParentFixOrigins:                        map[string]int{},
		ParentOutcomesByModel:                   map[string]int{},
		ParentOutcomesByRisk:                    map[string]int{},
	}
}

func mergeTaskStats(aggregate *state.TaskStats, stats state.TaskStats) {
	aggregate.ModelCalls += stats.ModelCalls
	mergeIntMap(&aggregate.ModelCallsByAlias, stats.ModelCallsByAlias)
	mergeInt64Map(&aggregate.ModelDurationMSByAlias, stats.ModelDurationMSByAlias)
	mergeIntMap(&aggregate.RateLimitsByAlias, stats.RateLimitsByAlias)
	mergeInt64Map(&aggregate.InputTokensByAlias, stats.InputTokensByAlias)
	mergeInt64Map(&aggregate.CacheCreationInputTokensByAlias, stats.CacheCreationInputTokensByAlias)
	mergeInt64Map(&aggregate.CacheReadInputTokensByAlias, stats.CacheReadInputTokensByAlias)
	mergeInt64Map(&aggregate.OutputTokensByAlias, stats.OutputTokensByAlias)
	mergeIntMap(&aggregate.TopLevelTurnsByAlias, stats.TopLevelTurnsByAlias)
	mergeIntMap(&aggregate.CallTreesByResolvedModel, stats.CallTreesByResolvedModel)
	mergeInt64Map(&aggregate.InputTokensByResolvedModel, stats.InputTokensByResolvedModel)
	mergeInt64Map(&aggregate.CacheCreationInputTokensByResolvedModel, stats.CacheCreationInputTokensByResolvedModel)
	mergeInt64Map(&aggregate.CacheReadInputTokensByResolvedModel, stats.CacheReadInputTokensByResolvedModel)
	mergeInt64Map(&aggregate.OutputTokensByResolvedModel, stats.OutputTokensByResolvedModel)
	aggregate.WorkerCalls += stats.WorkerCalls
	aggregate.ReviewerCalls += stats.ReviewerCalls
	aggregate.DecisionCommands += stats.DecisionCommands
	aggregate.FixCommands += stats.FixCommands
	aggregate.ResumeCommands += stats.ResumeCommands
	aggregate.AutoFixRounds += stats.AutoFixRounds
	aggregate.NeedsSolDecisionPackets += stats.NeedsSolDecisionPackets
	aggregate.NeedsSolReviewPackets += stats.NeedsSolReviewPackets
	aggregate.PassPackets += stats.PassPackets
	aggregate.RateLimits += stats.RateLimits
	aggregate.PacketCompactions += stats.PacketCompactions
	aggregate.SolPacketBytes += stats.SolPacketBytes
	aggregate.ProviderUnavailable += stats.ProviderUnavailable
	mergeIntMap(&aggregate.ProviderUnavailableByAlias, stats.ProviderUnavailableByAlias)
	mergeIntMap(&aggregate.RiskFloorByCategory, stats.RiskFloorByCategory)
	aggregate.SnapshotMismatches += stats.SnapshotMismatches
	mergeIntMap(&aggregate.SnapshotMismatchByAxis, stats.SnapshotMismatchByAxis)
	mergeIntMap(&aggregate.PacketRejectByCategory, stats.PacketRejectByCategory)
	mergeIntMap(&aggregate.ProbeOutcome, stats.ProbeOutcome)
	aggregate.TransientRetries += stats.TransientRetries
	mergeIntMap(&aggregate.ParentOutcomes, stats.ParentOutcomes)
	mergeIntMap(&aggregate.ParentFixOrigins, stats.ParentFixOrigins)
	mergeIntMap(&aggregate.ParentOutcomesByModel, stats.ParentOutcomesByModel)
	mergeIntMap(&aggregate.ParentOutcomesByRisk, stats.ParentOutcomesByRisk)
}

func buildStatsOutput(st *state.StateStore, all []state.TaskStats) statsOutput {
	aggregate := newAggregateTaskStats()
	for _, stats := range all {
		mergeTaskStats(&aggregate, stats)
	}
	output := statsOutputFromAggregate(st, len(all), aggregate, probeCallCount(aggregate.ProbeOutcome))
	output.TelemetryCoverage = statsCoverageDetail(st.ComputeTelemetryCoverage(all))
	fillStatsParentReview(st, all, aggregate, &output)
	output.CurrentTask = statsCurrentTaskDetail(st)
	return output
}

func probeCallCount(outcomes map[string]int) int {
	total := 0
	for _, count := range outcomes {
		total += count
	}
	return total
}

func statsOutputFromAggregate(st *state.StateStore, tasks int, aggregate state.TaskStats, probeCalls int) statsOutput {
	return statsOutput{
		Tasks:                           tasks,
		ModelCalls:                      aggregate.ModelCalls,
		ModelCallsByAlias:               aggregate.ModelCallsByAlias,
		ProbeCalls:                      probeCalls,
		TotalAICalls:                    aggregate.ModelCalls + probeCalls,
		ModelDurationMSByAlias:          aggregate.ModelDurationMSByAlias,
		InputTokensByAlias:              aggregate.InputTokensByAlias,
		CacheCreationInputTokensByAlias: aggregate.CacheCreationInputTokensByAlias,
		CacheReadInputTokensByAlias:     aggregate.CacheReadInputTokensByAlias,
		TotalPromptTokensByAlias: sumInt64Maps(
			aggregate.InputTokensByAlias,
			aggregate.CacheCreationInputTokensByAlias,
			aggregate.CacheReadInputTokensByAlias,
		),
		OutputTokensByAlias:                     aggregate.OutputTokensByAlias,
		TopLevelTurnsByAlias:                    aggregate.TopLevelTurnsByAlias,
		CallTreesByResolvedModel:                aggregate.CallTreesByResolvedModel,
		InputTokensByResolvedModel:              aggregate.InputTokensByResolvedModel,
		CacheCreationInputTokensByResolvedModel: aggregate.CacheCreationInputTokensByResolvedModel,
		CacheReadInputTokensByResolvedModel:     aggregate.CacheReadInputTokensByResolvedModel,
		OutputTokensByResolvedModel:             aggregate.OutputTokensByResolvedModel,
		WorkerCalls:                             aggregate.WorkerCalls,
		ReviewerCalls:                           aggregate.ReviewerCalls,
		DecisionCommands:                        aggregate.DecisionCommands,
		FixCommands:                             aggregate.FixCommands,
		ResumeCommands:                          aggregate.ResumeCommands,
		TransientRetries:                        aggregate.TransientRetries,
		AutoFixRounds:                           aggregate.AutoFixRounds,
		NeedsSolDecisionPackets:                 aggregate.NeedsSolDecisionPackets,
		NeedsSolReviewPackets:                   aggregate.NeedsSolReviewPackets,
		PassPackets:                             aggregate.PassPackets,
		RateLimits:                              aggregate.RateLimits,
		RateLimitsByAlias:                       aggregate.RateLimitsByAlias,
		ProviderUnavailable:                     aggregate.ProviderUnavailable,
		ProviderUnavailableByAlias:              aggregate.ProviderUnavailableByAlias,
		PacketCompactions:                       aggregate.PacketCompactions,
		RiskFloorByCategory:                     aggregate.RiskFloorByCategory,
		SnapshotMismatches:                      aggregate.SnapshotMismatches,
		SnapshotMismatchByAxis:                  aggregate.SnapshotMismatchByAxis,
		PacketRejectByCategory:                  aggregate.PacketRejectByCategory,
		ProbeOutcome:                            aggregate.ProbeOutcome,
		ParentOutcomes:                          aggregate.ParentOutcomes,
		ParentFixOrigins:                        aggregate.ParentFixOrigins,
		ParentOutcomesByModel:                   aggregate.ParentOutcomesByModel,
		ParentOutcomesByRisk:                    aggregate.ParentOutcomesByRisk,
		SolPacketBytes:                          aggregate.SolPacketBytes,
		TelemetryDir:                            st.Path("telemetry"),
	}
}

func statsCoverageDetail(coverage state.TelemetryCoverage) statsCoverage {
	detail := statsCoverage{
		Status:        coverage.Status,
		StatsCalls:    coverage.StatsCalls,
		RawRecords:    coverage.RawRecords,
		MissingCalls:  coverage.MissingCalls,
		ExcessRecords: coverage.ExcessRecords,
		OrphanFiles:   coverage.OrphanFiles,
		UsageKnown:    coverage.UsageKnown,
		Tasks:         make([]statsCoverageTask, 0, len(coverage.Tasks)),
	}
	for _, entry := range coverage.Tasks {
		classification := entry.Classification()
		if classification == state.CoverageComplete {
			continue
		}
		detail.Tasks = append(detail.Tasks, statsCoverageTask{
			TaskID:         entry.TaskID,
			Classification: classification,
			StatsCalls:     entry.StatsCalls,
			RawRecords:     entry.RawRecords,
			MissingCalls:   entry.MissingCalls(),
			ExcessRecords:  entry.ExcessRecords(),
		})
	}
	return detail
}

func fillStatsParentReview(st *state.StateStore, all []state.TaskStats, _ state.TaskStats, output *statsOutput) {
	output.ParentFixRework = make([]statsParentRework, 0)
	rework := st.ComputeParentRework(all)
	origins := make([]string, 0, len(rework.ByOrigin))
	for origin := range rework.ByOrigin {
		origins = append(origins, origin)
	}
	sort.Strings(origins)
	for _, origin := range origins {
		entry := rework.ByOrigin[origin]
		output.ParentFixRework = append(output.ParentFixRework, statsParentRework{
			Origin:           origin,
			Calls:            entry.Calls,
			WorkerCalls:      entry.WorkerCalls,
			ReviewerCalls:    entry.ReviewerCalls,
			Turns:            entry.Turns,
			TreeInputTokens:  entry.TreeInputTokens,
			TreeOutputTokens: entry.TreeOutputTokens,
			WallDurationMS:   entry.WallDurationMS,
		})
	}
	output.ParentFixReworkCoverage = rework.Coverage
}

func statsCurrentTaskDetail(st *state.StateStore) statsCurrentTask {
	current := statsCurrentTask{Status: taskStatusPtr(st.TaskStatus())}
	if id := st.ReadOr("task.id", ""); id != "" {
		current.ID = stringPtr(id)
		current.ArtifactDir = stringPtr(st.ArtifactDir(id))
	}
	return current
}

func mergeIntMap(target *map[string]int, source map[string]int) {
	if *target == nil {
		*target = make(map[string]int)
	}
	for key, value := range source {
		(*target)[key] += value
	}
}

func mergeInt64Map(target *map[string]int64, source map[string]int64) {
	if *target == nil {
		*target = make(map[string]int64)
	}
	for key, value := range source {
		(*target)[key] += value
	}
}

func sumInt64Maps(values ...map[string]int64) map[string]int64 {
	result := make(map[string]int64)
	for _, items := range values {
		for key, value := range items {
			result[key] += value
		}
	}
	return result
}

func printEvalAB(st *state.StateStore, dir string, stdout io.Writer) error {
	spec, direct, orchestrated, err := abeval.LoadPair(dir)
	if err != nil {
		return err
	}
	if orchestrated.GLMUsage.Source == abeval.GLMUsageSourceTaskStats {
		all, err := st.AllTaskStats()
		if err != nil {
			return err
		}
		orchestrated, err = abeval.ResolveFromTaskStats(orchestrated, all)
		if err != nil {
			return err
		}
	}
	if err := abeval.ValidatePair(spec, direct, orchestrated); err != nil {
		return err
	}
	return writeJSON(stdout, abeval.BuildReport(abeval.Compare(spec, direct, orchestrated)))
}

func resetState(st *state.StateStore, stdout io.Writer) error {
	if err := st.Reset(); err != nil {
		return err
	}
	return writeJSON(stdout, resetOutput{
		Status:   "reset",
		RepoRoot: stringPtr(st.ReadOr("repo-root", "")),
	})
}

func parentAccept(st *state.StateStore, stdout io.Writer) error {
	resolved, err := st.AcceptParentReview()
	if err != nil {
		return err
	}
	return writeJSON(stdout, acceptOutput{Accepted: resolved})
}

func (e *VerificationError) Error() string {
	return fmt.Sprintf("verification %s: %s", outcomeLabel(e.Outcome), e.Reason)
}

func printVerifyAutoResume(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	params := autoresume.Params{
		AutomationKey:    cmd.Verify.Key,
		ExpectedRFC3339:  cmd.Verify.RFC3339,
		ExpectedThreadID: cmd.Verify.ThreadID,
		AutomationsDir:   filepath.Join(cfg.CodexConfigDir, "automations"),
		DBPath:           filepath.Join(cfg.CodexConfigDir, "sqlite", "codex-dev.db"),
	}
	result := autoresume.Verify(params, autoresume.ReadDBRowSqlite3)
	if result.Outcome != autoresume.Pass {
		return &VerificationError{Outcome: result.Outcome, Reason: result.Reason}
	}
	return writeJSON(stdout, verifyAutoResumeOutput{
		AutomationKey:  result.AutomationKey,
		TargetThread:   result.TargetThread,
		ExpectedAtUTC:  result.ExpectedUTC,
		TOMLDTStart:    result.TOMLDTStart,
		DBNextRunAtUTC: result.DBNextRunUTC,
	})
}

func printCheckWakeCoalesce(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	result, err := autoresume.CheckCoalesce(autoresume.CoalesceParams{
		ParentThreadID:  cmd.Coalesce.ParentThreadID,
		ResumeAtRFC3339: cmd.Coalesce.ResumeAtRFC3339,
		AutomationsDir:  filepath.Join(cfg.CodexConfigDir, "automations"),
		DBPath:          filepath.Join(cfg.CodexConfigDir, "sqlite", "codex-dev.db"),
	}, autoresume.ReadDBRowSqlite3)
	if err != nil {
		return usageError("%s", err.Error())
	}
	return writeJSON(stdout, checkWakeCoalesceOutput{
		Decision:         result.Decision,
		Reason:           result.Reason,
		ParentThread:     result.ParentThread,
		ResumeAtUTC:      result.ResumeAtUTC,
		WakeAutomationID: result.WakeAutomationID,
		WakeThread:       result.WakeThread,
		WakeNextRunUTC:   result.WakeNextRunUTC,
		AddedWaitSeconds: result.AddedWaitSeconds,
	})
}

func outcomeLabel(o autoresume.Outcome) string {
	switch o {
	case autoresume.Pass:
		return "pass"
	case autoresume.Fail:
		return "fail"
	default:
		return "unavailable"
	}
}
