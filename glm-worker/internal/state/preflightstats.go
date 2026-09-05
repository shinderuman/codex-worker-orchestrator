package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"
)

type PreflightStats struct {
	Version           int       `json:"version"`
	SchemaRevision    int       `json:"schema_revision"`
	PassCount         int       `json:"pass_count"`
	FailureCount      int       `json:"failure_count"`
	AvoidedModelCalls int       `json:"avoided_model_calls"`
	TotalDurationMS   int64     `json:"total_duration_ms"`
	MaxDurationMS     int64     `json:"max_duration_ms"`
	LastDurationMS    int64     `json:"last_duration_ms"`
	LastOutcome       string    `json:"last_outcome"`
	LastAt            time.Time `json:"last_at"`
}

const (
	preflightStatsFile = "preflight-stats.json"

	preflightStatsVersion = 1

	preflightStatsSchemaRevision = 1

	PreflightOutcomePass = "pass"

	PreflightOutcomeFail = "fail"
)

var errUnsupportedPreflightStatsVersion = errors.New("unsupported preflight stats version")

func (s *StateStore) RecordPreflightAttempt(outcome string, at time.Time, duration time.Duration) {
	if outcome != PreflightOutcomePass && outcome != PreflightOutcomeFail {
		writeStatsWarningEvent("preflight_stats", "preflight statsの記録を無視しました（未知のoutcomeです）", fmt.Errorf("outcome %q", outcome))
		return
	}
	stats, err := s.LoadPreflightStats()
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			writeStatsWarningEvent("preflight_stats", "preflight statsの更新を中止しました（読めない既存集計を保持します）", err)
			return
		}
		stats = PreflightStats{}
	}
	stats.Version = preflightStatsVersion
	stats.SchemaRevision = preflightStatsSchemaRevision
	durationMS := duration.Milliseconds()
	if durationMS < 0 {
		durationMS = 0
	}
	if outcome == PreflightOutcomeFail {
		stats.FailureCount++
		stats.AvoidedModelCalls++
	} else {
		stats.PassCount++
	}
	stats.TotalDurationMS += durationMS
	if durationMS > stats.MaxDurationMS {
		stats.MaxDurationMS = durationMS
	}
	stats.LastDurationMS = durationMS
	stats.LastOutcome = outcome
	stats.LastAt = at
	data, err := json.MarshalIndent(stats, "", "  ")
	if err != nil {
		writeStatsWarningEvent("preflight_stats", "preflight statsのJSON化に失敗しました", err)
		return
	}
	if err := writeFileAtomic(s.Path(preflightStatsFile), append(data, '\n'), 0o600); err != nil {
		writeStatsWarningEvent("preflight_stats", "preflight statsの更新に失敗しました", err)
	}
}

func (s *StateStore) LoadPreflightStats() (PreflightStats, error) {
	data, err := os.ReadFile(s.Path(preflightStatsFile))
	if err != nil {
		return PreflightStats{}, err
	}
	var stats PreflightStats
	if err := json.Unmarshal(data, &stats); err != nil {
		return PreflightStats{}, fmt.Errorf("preflight statsを読めません: %w", err)
	}
	if stats.Version != preflightStatsVersion || stats.SchemaRevision != preflightStatsSchemaRevision {
		return PreflightStats{}, fmt.Errorf("%w: version=%d schema_revision=%d", errUnsupportedPreflightStatsVersion, stats.Version, stats.SchemaRevision)
	}
	if stats.LastOutcome != "" && stats.LastOutcome != PreflightOutcomePass && stats.LastOutcome != PreflightOutcomeFail {
		return PreflightStats{}, fmt.Errorf("preflight statsのlast_outcomeが未知です: %q", stats.LastOutcome)
	}
	return stats, nil
}
