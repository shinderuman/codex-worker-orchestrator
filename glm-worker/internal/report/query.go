package report

import (
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type Query struct {
	Scope   string
	Filter  state.TelemetryQueryFilter
	Compact bool
}

type queryView struct {
	Scope       string  `json:"scope"`
	TaskID      string  `json:"task_id,omitempty"`
	Since       *string `json:"since,omitempty"`
	Until       *string `json:"until,omitempty"`
	PeriodBasis string  `json:"period_basis,omitempty"`
}

const QueryPeriodBasisRecord = "record-started-at"

const QueryPeriodBasisTask = "task-started-at"

func (query Query) ResolvedScope() string {
	if query.Scope == "" {
		return state.TelemetryScopeCurrent
	}
	return query.Scope
}

func (query Query) IsHistory() bool {
	return query.ResolvedScope() == state.TelemetryScopeHistory
}

func (query Query) view(periodBasis string) queryView {
	scope := query.ResolvedScope()
	view := queryView{Scope: scope, TaskID: query.Filter.TaskID}
	if !query.Filter.Since.IsZero() {
		since := query.Filter.Since.UTC().Format(time.RFC3339Nano)
		view.Since = &since
	}
	if !query.Filter.Until.IsZero() {
		until := query.Filter.Until.UTC().Format(time.RFC3339Nano)
		view.Until = &until
	}
	if query.Filter.HasPeriod() {
		view.PeriodBasis = periodBasis
	}
	return view
}

func FilterTaskStatsForQuery(all []state.TaskStats, filter state.TelemetryQueryFilter) []state.TaskStats {
	filtered := make([]state.TaskStats, 0, len(all))
	for _, stats := range all {
		if !filter.MatchesTask(stats.TaskID) {
			continue
		}
		if !filter.CoversTime(stats.StartedAt) {
			continue
		}
		filtered = append(filtered, stats)
	}
	return filtered
}
