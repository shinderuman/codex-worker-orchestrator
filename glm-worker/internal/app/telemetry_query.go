package app

import (
	"fmt"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type telemetryQueryView struct {
	Scope       string  `json:"scope"`
	TaskID      string  `json:"task_id,omitempty"`
	Since       *string `json:"since,omitempty"`
	Until       *string `json:"until,omitempty"`
	PeriodBasis string  `json:"period_basis,omitempty"`
}

const telemetryQueryPeriodBasisRecord = "record-started-at"

const telemetryQueryPeriodBasisTask = "task-started-at"

func telemetryQueryCommand(args []string, mode CommandMode, flag string) (Command, error) {
	query, err := parseTelemetryQueryArgs(args[1:])
	if err != nil {
		return Command{}, usageError("usage: glm-worker %s %s", flag, telemetryQueryUsage)
	}
	return Command{Mode: mode, Query: query}, nil
}

func parseTelemetryQueryArgs(args []string) (TelemetryQueryArgs, error) {
	query := TelemetryQueryArgs{Scope: state.TelemetryScopeCurrent}
	rest, err := applyTelemetryQueryScope(&query, args)
	if err != nil {
		return TelemetryQueryArgs{}, err
	}
	for index := 0; index < len(rest); index += 2 {
		if index+1 >= len(rest) {
			return TelemetryQueryArgs{}, fmt.Errorf("option %s requires a value", rest[index])
		}
		if err := applyTelemetryQueryOption(&query, rest[index], rest[index+1]); err != nil {
			return TelemetryQueryArgs{}, err
		}
	}
	if !query.Filter.Since.IsZero() && !query.Filter.Until.IsZero() && !query.Filter.Since.Before(query.Filter.Until) {
		return TelemetryQueryArgs{}, fmt.Errorf("--since must be before --until")
	}
	return query, nil
}

func applyTelemetryQueryScope(query *TelemetryQueryArgs, args []string) ([]string, error) {
	if len(args) == 0 || len(args[0]) == 0 || args[0][0] == '-' {
		return args, nil
	}
	switch args[0] {
	case state.TelemetryScopeCurrent, state.TelemetryScopeHistory:
		query.Scope = args[0]
		return args[1:], nil
	default:
		return nil, fmt.Errorf("unknown scope %q", args[0])
	}
}

func applyTelemetryQueryOption(query *TelemetryQueryArgs, name string, value string) error {
	switch name {
	case "--task":
		return applyTelemetryQueryTask(query, value)
	case "--since":
		return applyTelemetryQueryTime(&query.Filter.Since, value)
	case "--until":
		return applyTelemetryQueryTime(&query.Filter.Until, value)
	default:
		return fmt.Errorf("unknown option %q", name)
	}
}

func applyTelemetryQueryTask(query *TelemetryQueryArgs, value string) error {
	if query.Filter.TaskID != "" {
		return fmt.Errorf("--task is given twice")
	}
	if !state.ValidGeneratedUUID(value) {
		return fmt.Errorf("task IDが生成されるUUID v4形式と一致しません: %q", value)
	}
	query.Filter.TaskID = value
	return nil
}

func applyTelemetryQueryTime(target *time.Time, value string) error {
	if !target.IsZero() {
		return fmt.Errorf("period bound is given twice")
	}
	parsed, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return fmt.Errorf("期間境界がRFC3339として解析できません: %q", value)
	}
	*target = parsed
	return nil
}

func (query TelemetryQueryArgs) resolvedScope() string {
	if query.Scope == "" {
		return state.TelemetryScopeCurrent
	}
	return query.Scope
}

func (query TelemetryQueryArgs) isHistory() bool {
	return query.resolvedScope() == state.TelemetryScopeHistory
}

func (query TelemetryQueryArgs) view(periodBasis string) telemetryQueryView {
	scope := query.resolvedScope()
	view := telemetryQueryView{Scope: scope, TaskID: query.Filter.TaskID}
	if !query.Filter.Since.IsZero() {
		since := query.Filter.Since.UTC().Format(time.RFC3339)
		view.Since = &since
	}
	if !query.Filter.Until.IsZero() {
		until := query.Filter.Until.UTC().Format(time.RFC3339)
		view.Until = &until
	}
	if query.Filter.HasPeriod() {
		view.PeriodBasis = periodBasis
	}
	return view
}

func filterTaskStatsForQuery(all []state.TaskStats, filter state.TelemetryQueryFilter) []state.TaskStats {
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
