package app

import (
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
	"io"
	"path/filepath"
	"strconv"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type statusOutput struct {
	RepoRoot            *string                   `json:"repo_root"`
	RuntimeBuild        statusRuntimeBuild        `json:"runtime_build"`
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

	Parked *statusParked `json:"parked,omitempty"`
}

type statusParked struct {
	ParkID          string `json:"park_id"`
	FromStatus      string `json:"from_status"`
	TaskID          string `json:"task_id"`
	Worktree        string `json:"worktree"`
	Branch          string `json:"branch"`
	RepoRoot        string `json:"repo_root"`
	CreatedAt       string `json:"created_at"`
	WorkerSession   string `json:"worker_session,omitempty"`
	ReviewerSession string `json:"reviewer_session,omitempty"`
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

func msPtr(d time.Duration) *int64 {
	if d < 0 {
		d = 0
	}
	ms := d.Milliseconds()
	return &ms
}

func printStatus(st *state.StateStore, stdout io.Writer) error {
	taskID := st.ReadOr("task.id", "")
	logs, logErr := taskview.ReadStatusTelemetry(st, taskID)
	output := buildStatusOutput(st, taskID, logs, logErr)
	return machinecli.WriteJSON(stdout, output)
}

func printStatusLeased(st *state.StateStore, stdout io.Writer) error {
	scope, err := captureParentEvidenceReadScope(st)
	if err != nil {
		return err
	}
	taskID := st.ReadOr("task.id", "")
	logs, logErr := taskview.ReadStatusTelemetry(st, taskID)
	output := buildStatusOutput(st, taskID, logs, logErr)
	digest := parentStatusReadDigest(st)
	return finishParentReadInScope(st, scope, state.ParentEvidenceSurfaceStatus, digest, func() (int, error) {
		return writeMeasuredJSON(stdout, output)
	})
}

func parentStatusReadDigest(st *state.StateStore) string {
	probe := ProbeRepoLock(st.LockPath())
	checkpointAvailable := false
	if checkpoint, err := st.LoadResumeCheckpoint(); err == nil {
		checkpointAvailable = checkpoint.IsStopped()
	}
	isolation := ""
	if record, err := st.LoadIsolationRecord(); err == nil {
		isolation = record.IsolationID + record.Branch + record.OriginHead
	}
	return parentEvidenceStringDigest(
		st.ReadOr("task.id", ""),
		string(st.TaskStatus()),
		st.ReadOr("worker.id", ""),
		st.ReadOr("reviewer.id", ""),
		strconv.FormatBool(st.Exists("pending-decision")),
		st.OpenParentReviewLabel(),
		strconv.FormatBool(checkpointAvailable),
		isolation,
		string(probe.State),
	)
}

func buildStatusOutput(st *state.StateStore, taskID string, logs []state.ModelCallLog, logErr error) statusOutput {
	probe := ProbeRepoLock(st.LockPath())
	taskStatus := st.TaskStatus()
	repoRoot := st.ReadOr("repo-root", "")
	output := statusOutput{
		RepoRoot:        machinecli.StringPtr(repoRoot),
		RuntimeBuild:    currentRuntimeBuild(repoRoot),
		RepositoryLock:  lockStatePtr(probe.State),
		LockPID:         lockPIDPtr(probe.PID),
		TaskID:          machinecli.StringPtr(taskID),
		TaskStatus:      machinecli.TaskStatusPtr(taskStatus),
		WorkerSession:   machinecli.StringPtr(st.ReadOr("worker.id", "")),
		ReviewerSession: machinecli.StringPtr(st.ReadOr("reviewer.id", "")),
		PendingDecision: st.Exists("pending-decision"),
	}
	if taskID != "" {
		output.ArtifactDir = machinecli.StringPtr(st.ArtifactDir(taskID))
	}
	if taskStatus == state.TaskStatusActive {
		output.TaskLiveness = taskLiveness(probe)
	}
	if label := st.OpenParentReviewLabel(); label != taskview.StatusNone {
		output.ParentReviewOpen = machinecli.StringPtr(label)
	}

	fillStatusTaskDetail(st, taskID, &output)
	output.ResumeAvailable = fillStatusCheckpoint(st, &output)
	fillStatusIsolation(st, &output)
	fillStatusParked(st, &output)
	output.Probes = statusProbesDetail(logs, time.Now())
	fillStatusTelemetry(taskID, logErr, logs, &output)
	return output
}

func fillStatusParked(st *state.StateStore, output *statusOutput) {
	record, err := st.LoadParkRecord()
	if err != nil {
		return
	}
	output.Parked = &statusParked{
		ParkID:          record.ParkID,
		FromStatus:      string(record.FromStatus),
		TaskID:          record.TaskID,
		Worktree:        record.Worktree,
		Branch:          record.Branch,
		RepoRoot:        record.RepoRoot,
		CreatedAt:       record.CreatedAt,
		WorkerSession:   record.WorkerSessionID,
		ReviewerSession: record.ReviewerSessionID,
	}
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

func lockPIDPtr(pid string) *string {
	if pid == "" || pid == taskview.StatusNone || pid == "unknown" {
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
	if last, ok := taskview.LastTaskEvent(st, taskID); ok {
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
	output.CurrentPhase = machinecli.StringPtr(current.phase)
	output.CurrentRole = machinecli.StringPtr(current.role)
	output.CurrentModel = machinecli.StringPtr(current.model)
}

func fillStatusCheckpoint(st *state.StateStore, output *statusOutput) bool {
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil {
		return false
	}
	if checkpoint.StopKind == state.ResumeStopRateLimited {
		output.RateLimited = statusRateLimit{
			Limited:        true,
			Phase:          checkpoint.Phase,
			ResetAtCST:     checkpoint.ResetAtCST,
			ResetAtRFC3339: machinecli.StringPtr(checkpoint.ResetAtRFC3339),
		}
	}
	if checkpoint.StopKind == state.ResumeStopProviderUnavailable {
		elapsed := (*int64)(nil)
		if !checkpoint.ProviderUnavailableStartedAt.IsZero() {
			elapsed = msPtr(time.Since(checkpoint.ProviderUnavailableStartedAt))
		}
		output.ProviderUnavailable = statusProviderUnavailable{
			Unavailable:    true,
			Phase:          checkpoint.Phase,
			Classification: machinecli.StringPtr(checkpoint.ProviderUnavailableClassification),
			Probes:         checkpoint.ProviderUnavailableProbes,
			ElapsedMS:      elapsed,
		}
	}
	return checkpoint.IsStopped()
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
		LastOutcome: machinecli.StringPtr(last.Outcome),
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
		unreadable := taskview.StatusUnreadable
		output.Telemetry = &unreadable
		return
	}
	ok := "ok"
	output.Telemetry = &ok
	output.SessionAging = state.AgingFromModelCallLogs(logs)
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

func resetState(st *state.StateStore, stdout io.Writer) error {
	if err := st.Reset(); err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, resetOutput{
		Status:   "reset",
		RepoRoot: machinecli.StringPtr(st.ReadOr("repo-root", "")),
	})
}

func parentAccept(st *state.StateStore, stdout io.Writer) error {
	resolved, err := st.AcceptParentReview()
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, acceptOutput{Accepted: resolved})
}

func (e *VerificationError) Error() string {
	return fmt.Sprintf("verification %s: %s", outcomeLabel(e.Outcome), e.Reason)
}

func printVerifyCodexWake(cmd Command, cfg config.AppConfig, stdout io.Writer) error {
	key := autoresume.CodexWakeAutomationKey(cmd.Verify.ThreadID)
	return printAutomationVerification(key, cmd.Verify.RFC3339, cmd.Verify.ThreadID, cfg, stdout)
}

func printAutomationVerification(automationKey, expectedRFC3339, expectedThreadID string, cfg config.AppConfig, stdout io.Writer) error {
	params := autoresume.Params{
		AutomationKey:    automationKey,
		ExpectedRFC3339:  expectedRFC3339,
		ExpectedThreadID: expectedThreadID,
		AutomationsDir:   filepath.Join(cfg.CodexConfigDir, "automations"),
		DBPath:           filepath.Join(cfg.CodexConfigDir, "sqlite", "codex-dev.db"),
	}
	result := autoresume.Verify(params, autoresume.ReadDBRowSqlite3)
	if result.Outcome != autoresume.Pass {
		return &VerificationError{Outcome: result.Outcome, Reason: result.Reason}
	}
	return machinecli.WriteJSON(stdout, verifyAutoResumeOutput{
		AutomationKey:  result.AutomationKey,
		TargetThread:   result.TargetThread,
		ExpectedAtUTC:  result.ExpectedUTC,
		TOMLDTStart:    result.TOMLDTStart,
		DBNextRunAtUTC: result.DBNextRunUTC,
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
