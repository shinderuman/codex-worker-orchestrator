package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
)

type statsWarningEvent struct {
	Type    string `json:"type"`
	Scope   string `json:"scope"`
	Message string `json:"message"`
	Error   string `json:"error,omitempty"`
}

type TaskStats struct {
	Version    int        `json:"version"`
	TaskID     string     `json:"task_id"`
	StartedAt  time.Time  `json:"started_at"`
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
	Status     TaskStatus `json:"status"`

	ModelCalls                              int              `json:"model_calls"`
	ModelCallsByAlias                       map[string]int   `json:"model_calls_by_alias,omitempty"`
	ModelDurationMSByAlias                  map[string]int64 `json:"model_duration_ms_by_alias,omitempty"`
	RateLimitsByAlias                       map[string]int   `json:"rate_limits_by_alias,omitempty"`
	InputTokensByAlias                      map[string]int64 `json:"input_tokens_by_alias,omitempty"`
	CacheCreationInputTokensByAlias         map[string]int64 `json:"cache_creation_input_tokens_by_alias,omitempty"`
	CacheReadInputTokensByAlias             map[string]int64 `json:"cache_read_input_tokens_by_alias,omitempty"`
	OutputTokensByAlias                     map[string]int64 `json:"output_tokens_by_alias,omitempty"`
	TopLevelTurnsByAlias                    map[string]int   `json:"top_level_turns_by_alias,omitempty"`
	CallTreesByResolvedModel                map[string]int   `json:"call_trees_by_resolved_model,omitempty"`
	InputTokensByResolvedModel              map[string]int64 `json:"input_tokens_by_resolved_model,omitempty"`
	CacheCreationInputTokensByResolvedModel map[string]int64 `json:"cache_creation_input_tokens_by_resolved_model,omitempty"`
	CacheReadInputTokensByResolvedModel     map[string]int64 `json:"cache_read_input_tokens_by_resolved_model,omitempty"`
	OutputTokensByResolvedModel             map[string]int64 `json:"output_tokens_by_resolved_model,omitempty"`
	WorkerCalls                             int              `json:"worker_calls"`
	ReviewerCalls                           int              `json:"reviewer_calls"`
	DecisionCommands                        int              `json:"decision_commands"`
	FixCommands                             int              `json:"fix_commands"`
	ResumeCommands                          int              `json:"resume_commands"`
	AutoFixRounds                           int              `json:"auto_fix_rounds"`
	NeedsSolDecisionPackets                 int              `json:"needs_sol_decision_packets"`
	NeedsSolReviewPackets                   int              `json:"needs_sol_review_packets"`
	PassPackets                             int              `json:"pass_packets"`
	RateLimits                              int              `json:"rate_limits"`

	PacketCompactions int `json:"packet_compactions"`

	SolPacketBytes int `json:"sol_packet_bytes"`

	ResultCorrections          int            `json:"result_corrections,omitempty"`
	StructuredRetryExhausted   int            `json:"structured_retry_exhausted,omitempty"`
	ProviderUnavailable        int            `json:"provider_unavailable"`
	ProviderUnavailableByAlias map[string]int `json:"provider_unavailable_by_alias,omitempty"`

	RiskFloorByCategory    map[string]int `json:"risk_floor_by_category,omitempty"`
	SnapshotMismatches     int            `json:"snapshot_mismatches,omitempty"`
	SnapshotMismatchByAxis map[string]int `json:"snapshot_mismatch_by_axis,omitempty"`
	PacketRejectByCategory map[string]int `json:"packet_reject_by_category,omitempty"`
	ProbeOutcome           map[string]int `json:"probe_outcome,omitempty"`

	RepoSearchCalls             int            `json:"repo_search_calls,omitempty"`
	RepoSearchQueriesByCategory map[string]int `json:"repo_search_queries_by_category,omitempty"`
	RepoSearchOutcomes          map[string]int `json:"repo_search_outcomes,omitempty"`
	RepoSearchResults           int            `json:"repo_search_results,omitempty"`
	RepoSearchDurationMS        int64          `json:"repo_search_duration_ms,omitempty"`

	TransientRetries int `json:"transient_retries,omitempty"`

	ParentReviewOpen      *ParentReviewOpenState `json:"parent_review_open,omitempty"`
	ParentOutcomes        map[string]int         `json:"parent_outcomes,omitempty"`
	ParentFixOrigins      map[string]int         `json:"parent_fix_origins,omitempty"`
	ParentOutcomesByModel map[string]int         `json:"parent_outcomes_by_model,omitempty"`
	ParentOutcomesByRisk  map[string]int         `json:"parent_outcomes_by_risk,omitempty"`
}

const (
	currentStatsFile = "task-stats.json"

	taskStatsVersion = 3
)

var errUnsupportedTaskStatsVersion = errors.New("unsupported task stats version")

var statsWarnOut io.Writer = os.Stderr

func RedirectStatsWarnings(w io.Writer) func() {
	previous := statsWarnOut
	statsWarnOut = w
	return func() {
		statsWarnOut = previous
	}
}

func writeStatsWarningEvent(scope, message string, err error) {
	event := statsWarningEvent{Type: "warning", Scope: scope, Message: message}
	if err != nil {
		event.Error = err.Error()
	}
	data, marshalErr := json.Marshal(event)
	if marshalErr != nil {
		return
	}
	_, _ = statsWarnOut.Write(append(data, '\n'))
}

func warnStatsFailure(operation string, err error) {
	writeStatsWarningEvent("task_stats", fmt.Sprintf("task statsの%sに失敗しました（観測用mirrorのため続行します）", operation), err)
}

func (s *StateStore) InitializeTaskStats(taskID string) {
	stats := TaskStats{
		Version:   taskStatsVersion,
		TaskID:    taskID,
		StartedAt: time.Now().UTC(),
		Status:    TaskStatusActive,
	}
	if err := s.writeTaskStats(stats); err != nil {
		warnStatsFailure("初期化", err)
	}
}

func (s *StateStore) UpdateTaskStats(update func(*TaskStats)) {
	stats, err := s.loadTaskStats()
	if err != nil {
		stats, err = s.recoverTaskStats(err)
		if err != nil {
			return
		}
	}
	update(&stats)
	if err := s.writeTaskStats(stats); err != nil {
		warnStatsFailure("更新", err)
	}
}

func (s *StateStore) recoverTaskStats(loadErr error) (TaskStats, error) {
	if errors.Is(loadErr, os.ErrNotExist) {
		return s.bootstrapTaskStats()
	}
	warnStatsFailure("読み込み", loadErr)
	return s.bootstrapTaskStats()
}

func (s *StateStore) bootstrapTaskStats() (TaskStats, error) {
	taskID, taskErr := s.Read("task.id")
	if taskErr != nil || taskID == "" {
		return TaskStats{}, fmt.Errorf("task.idがありません")
	}
	return TaskStats{
		Version:   taskStatsVersion,
		TaskID:    taskID,
		StartedAt: time.Now().UTC(),
		Status:    s.TaskStatus(),
	}, nil
}

func (s *StateStore) ArchiveCurrentStats() {
	stats, err := s.loadTaskStats()
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		warnStatsFailure("archive読み込み", err)
		return
	}

	now := time.Now().UTC()
	stats.ArchivedAt = &now

	resolved, hadOpen, _ := stats.resolveParentOutcome(ParentOutcomeUnknown, "")
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		warnStatsFailure("archive JSON化", err)
		return
	}
	historyPath := filepath.Join(s.dir, "stats", stats.TaskID+".json")
	if err := writeFileAtomic(historyPath, append(data, '\n'), 0o600); err != nil {
		warnStatsFailure("archive書き込み", err)
		return
	}
	if hadOpen {
		s.appendParentOutcomeEvent(stats.TaskID, ParentPhaseClose, ParentOutcomeUnknown, "", resolved)
	}
	if err := s.Remove(currentStatsFile); err != nil {
		warnStatsFailure("archive後削除", err)
	}
}

func (s *StateStore) loadTaskStats() (TaskStats, error) {
	data, err := os.ReadFile(s.Path(currentStatsFile))
	if err != nil {
		return TaskStats{}, err
	}
	return decodeTaskStats(data)
}

func (s *StateStore) CurrentTaskStats() (TaskStats, error) {
	return s.loadTaskStats()
}

func decodeTaskStats(data []byte) (TaskStats, error) {
	var stats TaskStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return TaskStats{}, fmt.Errorf("task statsを読めません: %w", err)
	}
	if stats.Version != taskStatsVersion {
		return TaskStats{}, fmt.Errorf("%w: %d", errUnsupportedTaskStatsVersion, stats.Version)
	}
	return stats, nil
}

func (s *StateStore) writeTaskStats(stats TaskStats) error {
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		return fmt.Errorf("task statsをJSON化できません: %w", err)
	}
	return writeFileAtomic(s.Path(currentStatsFile), append(data, '\n'), 0o600)
}

func (s *StateStore) AllTaskStats() ([]TaskStats, error) {
	result := make([]TaskStats, 0)
	historyPaths, err := filepath.Glob(filepath.Join(s.dir, "stats", "*.json"))
	if err != nil {
		return nil, err
	}
	sort.Strings(historyPaths)
	for _, path := range historyPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		stats, err := decodeTaskStats(data)
		if errors.Is(err, errUnsupportedTaskStatsVersion) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("task stats historyを読めません: %w", err)
		}
		result = append(result, stats)
	}
	current, err := s.loadTaskStats()
	switch {
	case err == nil:
		result = append(result, current)
	case errors.Is(err, errUnsupportedTaskStatsVersion):
		return result, nil
	case !errors.Is(err, os.ErrNotExist):
		return nil, err
	}
	return result, nil
}

func (s *StateStore) RecordModelCall(role SessionRole, model string) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ModelCalls++
		if stats.ModelCallsByAlias == nil {
			stats.ModelCallsByAlias = make(map[string]int)
		}
		stats.ModelCallsByAlias[model]++
		if role == ReviewerRole {
			stats.ReviewerCalls++
		} else {
			stats.WorkerCalls++
		}
	})
}

func (s *StateStore) RecordTransientRetry() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.TransientRetries++
	})
}

func (s *StateStore) RecordModelDuration(model string, duration time.Duration) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		if stats.ModelDurationMSByAlias == nil {
			stats.ModelDurationMSByAlias = make(map[string]int64)
		}
		stats.ModelDurationMSByAlias[model] += duration.Milliseconds()
	})
}

func (s *StateStore) RecordDecision() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.DecisionCommands++
	})
}

func (s *StateStore) RecordFix() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.FixCommands++
	})
}

func (s *StateStore) RecordResume() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ResumeCommands++
	})
}

func (s *StateStore) RecordAutoFix() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.AutoFixRounds++
	})
}

func (s *StateStore) RecordRateLimit(model string) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.RateLimits++
		if stats.RateLimitsByAlias == nil {
			stats.RateLimitsByAlias = make(map[string]int)
		}
		stats.RateLimitsByAlias[model]++
	})
}

func (s *StateStore) RecordProviderUnavailable(model string) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ProviderUnavailable++
		if stats.ProviderUnavailableByAlias == nil {
			stats.ProviderUnavailableByAlias = make(map[string]int)
		}
		stats.ProviderUnavailableByAlias[model]++
	})
}

func (s *StateStore) RecordResultCorrection() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.ResultCorrections++
	})
}

func (s *StateStore) RecordStructuredRetryExhausted() {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.StructuredRetryExhausted++
	})
}

func (s *StateStore) RecordRiskFloor(category string) {
	if category == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		addInt(&stats.RiskFloorByCategory, category, 1)
	})
}

func (s *StateStore) RecordSnapshotMismatch(axis string) {
	if axis == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.SnapshotMismatches++
		for _, a := range strings.Split(axis, ",") {
			addInt(&stats.SnapshotMismatchByAxis, a, 1)
		}
	})
}

func (s *StateStore) RecordPacketReject(category string) {
	if category == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		addInt(&stats.PacketRejectByCategory, category, 1)
	})
}

func (s *StateStore) RecordProbeOutcome(outcome string) {
	if outcome == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		addInt(&stats.ProbeOutcome, outcome, 1)
	})
}

func (s *StateStore) RecordRepoSearchOutcome(category string, outcome string, resultCount int, duration time.Duration) {
	if category == "" || outcome == "" {
		return
	}
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.RepoSearchCalls++
		addInt(&stats.RepoSearchQueriesByCategory, category, 1)
		addInt(&stats.RepoSearchOutcomes, outcome, 1)
		stats.RepoSearchResults += resultCount
		stats.RepoSearchDurationMS += duration.Milliseconds()
	})
}

func (s *StateStore) RecordSolResult(value packet.Result, producer ParentReviewProducer) {
	s.UpdateTaskStats(func(stats *TaskStats) {
		stats.SolPacketBytes += value.ByteSize()
		switch value.Status {
		case packet.StatusNeedsSolDecision:
			stats.NeedsSolDecisionPackets++
		case packet.StatusNeedsSolReview:
			stats.NeedsSolReviewPackets++
		case packet.StatusPass:
			stats.PassPackets++
		}
		stats.openParentReview(string(value.Status), string(value.Risk), producer)
	})
}

func (s *StateStore) Reset() error {
	s.ArchiveCurrentStats()
	return s.Remove(taskStateFileNames()...)
}
