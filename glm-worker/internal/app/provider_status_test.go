package app

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func executeStatsOutput(t *testing.T, st *state.StateStore) statsOutput {
	t.Helper()
	var out bytes.Buffer
	if err := printStats(st, &out); err != nil {
		t.Fatal(err)
	}
	var output statsOutput
	if err := json.Unmarshal([]byte(strings.TrimSpace(out.String())), &output); err != nil {
		t.Fatalf("--stats出力がmachine JSONではありません: %v: %q", err, out.String())
	}
	return output
}

func TestExecuteStatusShowsProviderUnavailable(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	startedAt := time.Now().Add(-51 * time.Minute).UTC()
	if err := st.SaveResumeCheckpoint(state.ResumeCheckpoint{
		Stage:                             state.ResumeStageWorker,
		Phase:                             "worker-new",
		Role:                              state.WorkerRole,
		Model:                             "opus",
		Effort:                            "high",
		Prompt:                            "p",
		Request:                           "req",
		StopKind:                          state.ResumeStopProviderUnavailable,
		ProviderUnavailableClassification: "http-503",
		ProviderUnavailableProbes:         4,
		ProviderUnavailableStartedAt:      startedAt,
	}); err != nil {
		t.Fatal(err)
	}
	if err := st.SetTaskStatus(state.TaskStatusProviderUnavailable); err != nil {
		t.Fatal(err)
	}

	output := executeStatusOutput(t, cfg)
	statusString(t, "task_status", output.TaskStatus, string(state.TaskStatusProviderUnavailable))
	if !output.ProviderUnavailable.Unavailable {
		t.Fatalf("provider_unavailable = %#v", output.ProviderUnavailable)
	}
	statusString(t, "provider_unavailable.phase", &output.ProviderUnavailable.Phase, "worker-new")
	if output.ProviderUnavailable.Classification == nil || *output.ProviderUnavailable.Classification != "http-503" {
		t.Fatalf("provider_unavailable.classification = %#v", output.ProviderUnavailable.Classification)
	}
	if output.ProviderUnavailable.Probes != 4 {
		t.Fatalf("provider_unavailable.probes = %d", output.ProviderUnavailable.Probes)
	}
	if !output.ResumeAvailable {
		t.Fatal("provider停止中はresume_availableが必要です")
	}
	if output.RateLimited.Limited {
		t.Fatalf("provider-unavailable時にrate_limited.limited = true: %#v", output.RateLimited)
	}
}

func TestExecuteStatusReportsProviderUnavailableNoWhenClean(t *testing.T) {
	cfg := newAppConfig(t)
	output := executeStatusOutput(t, cfg)
	if output.ProviderUnavailable.Unavailable {
		t.Fatalf("空状態のprovider_unavailable = %#v", output.ProviderUnavailable)
	}
	if output.ResumeAvailable {
		t.Fatal("空状態でresume_availableです")
	}
}

func TestPrintStatsAggregatesProviderUnavailable(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	st.RecordProviderUnavailable("opus")
	st.RecordProviderUnavailable("opus")
	st.RecordProviderUnavailable("haiku")

	output := executeStatsOutput(t, st)
	if output.ProviderUnavailable != 3 {
		t.Fatalf("provider_unavailable = %d", output.ProviderUnavailable)
	}
	if len(output.ProviderUnavailableByAlias) != 2 || output.ProviderUnavailableByAlias["haiku"] != 1 || output.ProviderUnavailableByAlias["opus"] != 2 {
		t.Fatalf("provider_unavailable_by_alias = %#v", output.ProviderUnavailableByAlias)
	}
}

func TestPrintStatsReportsDiagnosticAggregates(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	st.RecordRiskFloor("worker-declared")
	st.RecordRiskFloor("worker-declared")
	st.RecordRiskFloor("self-protection")
	st.RecordSnapshotMismatch("head,index")
	st.RecordPacketReject("size")
	st.RecordPacketReject("malformed")
	st.RecordProbeOutcome("probe_failure")
	st.RecordProbeOutcome("probe_success")
	st.RecordTransientRetry()

	output := executeStatsOutput(t, st)
	if len(output.RiskFloorByCategory) != 2 || output.RiskFloorByCategory["self-protection"] != 1 || output.RiskFloorByCategory["worker-declared"] != 2 {
		t.Fatalf("risk_floor_by_category = %#v", output.RiskFloorByCategory)
	}
	if output.SnapshotMismatches != 1 {
		t.Fatalf("snapshot_mismatches = %d", output.SnapshotMismatches)
	}
	if len(output.SnapshotMismatchByAxis) != 2 || output.SnapshotMismatchByAxis["head"] != 1 || output.SnapshotMismatchByAxis["index"] != 1 {
		t.Fatalf("snapshot_mismatch_by_axis = %#v", output.SnapshotMismatchByAxis)
	}
	if len(output.PacketRejectByCategory) != 2 || output.PacketRejectByCategory["malformed"] != 1 || output.PacketRejectByCategory["size"] != 1 {
		t.Fatalf("packet_reject_by_category = %#v", output.PacketRejectByCategory)
	}
	if len(output.ProbeOutcome) != 2 || output.ProbeOutcome["probe_failure"] != 1 || output.ProbeOutcome["probe_success"] != 1 {
		t.Fatalf("probe_outcome = %#v", output.ProbeOutcome)
	}
	if output.ProbeCalls != 2 {
		t.Fatalf("probe_calls = %d", output.ProbeCalls)
	}
	if output.TotalAICalls != 2 {
		t.Fatalf("total_ai_calls = %d", output.TotalAICalls)
	}
	if output.TransientRetries != 1 {
		t.Fatalf("transient_retries = %d", output.TransientRetries)
	}
}

func TestPrintStatsReportsDiagnosticAggregatesEmpty(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.StartNewTask(); err != nil {
		t.Fatal(err)
	}
	output := executeStatsOutput(t, st)
	if len(output.RiskFloorByCategory) != 0 {
		t.Fatalf("risk_floor_by_category = %#v", output.RiskFloorByCategory)
	}
	if output.SnapshotMismatches != 0 || len(output.SnapshotMismatchByAxis) != 0 {
		t.Fatalf("snapshot_mismatch = %d %#v", output.SnapshotMismatches, output.SnapshotMismatchByAxis)
	}
	if len(output.PacketRejectByCategory) != 0 {
		t.Fatalf("packet_reject_by_category = %#v", output.PacketRejectByCategory)
	}
	if len(output.ProbeOutcome) != 0 {
		t.Fatalf("probe_outcome = %#v", output.ProbeOutcome)
	}
	if output.ProbeCalls != 0 || output.TotalAICalls != 0 || output.TransientRetries != 0 {
		t.Fatalf("空状態の計数 = probe=%d total=%d transient=%d", output.ProbeCalls, output.TotalAICalls, output.TransientRetries)
	}
}
