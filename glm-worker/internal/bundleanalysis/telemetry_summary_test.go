package bundleanalysis

import (
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/report"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestCompactStatsAttributesOnlyCurrentSchemaCohort(t *testing.T) {
	t.Parallel()
	filtered := []state.TaskStats{
		{
			TaskID: "current", WorkerCalls: 2, ReviewerCalls: 1,
			InputTokensByAlias:          map[string]int64{"opus": 100},
			CacheReadInputTokensByAlias: map[string]int64{"opus": 30},
			OutputTokensByAlias:         map[string]int64{"opus": 20},
			ParentOutcomes:              map[string]int{"implemented": 1},
		},
		{
			TaskID: "historical", WorkerCalls: 900,
			InputTokensByAlias: map[string]int64{"opus": 9900},
		},
	}
	got := buildTelemetryCompactStats(filtered, map[string]bool{"current": true})
	if got.PeriodBasis != report.QueryPeriodBasisTask ||
		got.TasksConsidered != 2 || got.CurrentSchemaTasks != 1 ||
		got.TasksWithoutCurrentSchemaRecords != 1 ||
		got.WorkerCalls != 2 || got.ReviewerCalls != 1 {
		t.Fatalf("current schema cohort attribution = %#v", got)
	}
	if got.TotalPromptTokensByAlias["opus"] != 130 ||
		got.OutputTokensByAlias["opus"] != 20 ||
		got.ParentOutcomes["implemented"] != 1 {
		t.Fatalf("current schema usage aggregation = %#v", got)
	}
}

func TestCompactOutlierNonEvaluatedBoundaryKeepsUnknown(t *testing.T) {
	t.Parallel()
	unknown := telemetryCompactOutliersSection(state.CallOutlierReport{}, false)
	if unknown.Status != telemetryCompactOutliersNotEvaluated ||
		unknown.TopCalls == nil || unknown.TopTasks == nil ||
		len(unknown.TopCalls) != 0 || len(unknown.TopTasks) != 0 {
		t.Fatalf("unknown outlier cohort was interpreted as evaluated: %#v", unknown)
	}
	empty := telemetryCompactOutliersSection(state.CallOutlierReport{}, true)
	if empty.Status != telemetryCompactOutliersEvaluated ||
		empty.OutlierCalls != 0 || empty.TruncatedCalls != 0 {
		t.Fatalf("empty current cohort lost evaluated boundary: %#v", empty)
	}
}
