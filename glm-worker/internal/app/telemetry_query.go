package app

import (
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/report"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

const telemetryQueryCompactFlag = "--compact"

func telemetryQueryCommand(args []string, mode CommandMode, flag string) (Command, error) {
	query, err := parseTelemetryQueryArgs(args[1:])
	if err != nil {
		return Command{}, machinecli.UsageErrorf("usage: glm-worker %s %s", flag, telemetryQueryUsage)
	}
	return Command{Mode: mode, Query: query}, nil
}

func parseTelemetryQueryArgs(args []string) (report.Query, error) {
	query := report.Query{Scope: state.TelemetryScopeCurrent}
	rest, err := applyTelemetryQueryScope(&query, args)
	if err != nil {
		return report.Query{}, err
	}
	options, err := takeTelemetryQueryCompactFlag(&query, rest)
	if err != nil {
		return report.Query{}, err
	}
	for index := 0; index < len(options); index += 2 {
		if index+1 >= len(options) {
			return report.Query{}, fmt.Errorf("option %s requires a value", options[index])
		}
		if err := applyTelemetryQueryOption(&query, options[index], options[index+1]); err != nil {
			return report.Query{}, err
		}
	}
	if !query.Filter.Since.IsZero() && !query.Filter.Until.IsZero() && !query.Filter.Since.Before(query.Filter.Until) {
		return report.Query{}, fmt.Errorf("--since must be before --until")
	}
	return query, nil
}

func takeTelemetryQueryCompactFlag(query *report.Query, args []string) ([]string, error) {
	options := make([]string, 0, len(args))
	for _, arg := range args {
		if arg != telemetryQueryCompactFlag {
			options = append(options, arg)
			continue
		}
		if query.Compact {
			return nil, fmt.Errorf("--compact is given twice")
		}
		query.Compact = true
	}
	return options, nil
}

func applyTelemetryQueryScope(query *report.Query, args []string) ([]string, error) {
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

func applyTelemetryQueryOption(query *report.Query, name string, value string) error {
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

func applyTelemetryQueryTask(query *report.Query, value string) error {
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
