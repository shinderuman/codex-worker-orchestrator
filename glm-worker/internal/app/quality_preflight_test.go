package app

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/harnesslint"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func stubQualityPreflight(t *testing.T, fn qualityPreflightFunc) {
	t.Helper()
	previous := runQualityPreflight
	runQualityPreflight = fn
	t.Cleanup(func() { runQualityPreflight = previous })
}

func newQualityContractConfig(t *testing.T) config.AppConfig {
	t.Helper()
	cfg := newAppConfig(t)
	marker := filepath.Join(cfg.RepoRoot, "glm-worker", "go.mod")
	if err := os.MkdirAll(filepath.Dir(marker), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("module github.com/shinderuman/codex-worker-orchestrator/glm-worker\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return cfg
}

func seedPreflightTaskID(t *testing.T, cfg config.AppConfig) (*state.StateStore, string) {
	t.Helper()
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := state.NewUUID()
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Write("task.id", taskID); err != nil {
		t.Fatal(err)
	}
	return st, taskID
}

func preflightStatsOrFail(t *testing.T, st *state.StateStore) state.PreflightStats {
	t.Helper()
	stats, err := st.LoadPreflightStats()
	if err != nil {
		t.Fatal(err)
	}
	return stats
}

func TestExecuteNewTaskFailsClosedBeforeModelCallOnPreflightMismatch(t *testing.T) {
	cfg := newQualityContractConfig(t)
	st, taskID := seedPreflightTaskID(t, cfg)
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
	stubQualityPreflight(t, func(string) error {
		return &harnesslint.QualityToolVersionMismatch{Tool: "golangci-lint", Observed: "2.13.1", Required: "2.7.0"}
	})

	err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, r.factory(), io.Discard, io.Discard)
	var preflightErr *QualityPreflightError
	if err == nil || !errors.As(err, &preflightErr) {
		t.Fatalf("preflight mismatchをtyped errorで返す必要があります: %v", err)
	}
	if preflightErr.Tool != "golangci-lint" || preflightErr.Observed != "2.13.1" || preflightErr.Required != "2.7.0" {
		t.Fatalf("mismatch fields = %+v", preflightErr)
	}
	if preflightErr.Classification != harnesslint.QualityToolEnvironmentFailure {
		t.Fatalf("mismatch分類 = %s", preflightErr.Classification)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("model callが実行されました: %d", len(r.prompts))
	}
	if got := st.ReadOr("task.id", ""); got != taskID {
		t.Fatalf("task.id = %q want %q", got, taskID)
	}
	if st.TaskStatus() != state.TaskStatusNone {
		t.Fatalf("task status = %q", st.TaskStatus())
	}
	if st.Exists("last-request") || st.Exists("last-review") {
		t.Fatal("preflight失敗でtask stateが変更されました")
	}
	stats := preflightStatsOrFail(t, st)
	if stats.FailureCount != 1 || stats.AvoidedModelCalls != 1 || stats.LastOutcome != state.PreflightOutcomeFail {
		t.Fatalf("preflight失敗が集計されていません: %+v", stats)
	}
	if stats.LastAt.IsZero() || stats.LastDurationMS < 0 || stats.TotalDurationMS < stats.LastDurationMS {
		t.Fatalf("preflight集計の時間軸が不正です: %+v", stats)
	}
	if _, err := os.Stat(st.ModelCallLogPath(taskID)); !os.IsNotExist(err) {
		t.Fatalf("残留task idへの誤帰属を防いでいません: %v", err)
	}
}

func TestExecuteNewTaskOnFreshStoreRecordsPreflightFailure(t *testing.T) {
	cfg := newQualityContractConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	r := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
	stubQualityPreflight(t, func(string) error {
		return &harnesslint.QualityToolVersionMismatch{Tool: "shfmt", Observed: "3.8.0", Required: "3.13.1"}
	})

	err = Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, r.factory(), io.Discard, io.Discard)
	var preflightErr *QualityPreflightError
	if err == nil || !errors.As(err, &preflightErr) {
		t.Fatalf("preflight mismatchをtyped errorで返す必要があります: %v", err)
	}
	if len(r.prompts) != 0 {
		t.Fatalf("model callが実行されました: %d", len(r.prompts))
	}
	if st.ReadOr("task.id", "") != "" {
		t.Fatal("fresh storeのpreflight失敗でlifecycle task idが生成されました")
	}
	if st.TaskStatus() != state.TaskStatusNone {
		t.Fatalf("task status = %q", st.TaskStatus())
	}
	if st.Exists("last-request") || st.Exists("last-review") {
		t.Fatal("preflight失敗でtask stateが変更されました")
	}
	stats := preflightStatsOrFail(t, st)
	if stats.FailureCount != 1 || stats.AvoidedModelCalls != 1 || stats.LastOutcome != state.PreflightOutcomeFail {
		t.Fatalf("fresh storeのpreflight失敗が集計されていません: %+v", stats)
	}
}

func TestExecuteNewTaskIsAdmissibleAgainAfterPreflightRepair(t *testing.T) {
	cfg := newQualityContractConfig(t)
	st, taskID := seedPreflightTaskID(t, cfg)
	rejected := &fakeRunner{steps: []fakeStep{{structured: implementedPacketApp("done")}}}
	stubQualityPreflight(t, func(string) error {
		return &harnesslint.QualityToolVersionMismatch{Tool: "shfmt", Observed: "3.8.0", Required: "3.13.1"}
	})
	err := Execute(Command{Mode: ModeNewTask, Payload: "request"}, cfg, rejected.factory(), io.Discard, io.Discard)
	var preflightErr *QualityPreflightError
	if err == nil || !errors.As(err, &preflightErr) {
		t.Fatalf("preflight mismatchをtyped errorで返す必要があります: %v", err)
	}
	if len(rejected.prompts) != 0 {
		t.Fatalf("preflight失敗時にmodel callが実行されました: %d", len(rejected.prompts))
	}
	if st.Exists("last-review") {
		t.Fatal("preflight事前失敗がreview findingとして計上されています")
	}
	if st.ReadOr("task.id", "") != taskID {
		t.Fatalf("preflight失敗でtask identityが変わりました: %s", st.ReadOr("task.id", ""))
	}
	if err := admitParentCommand(Command{Mode: ModeNewTask}, st); err != nil {
		t.Fatalf("修復後の同じadmission再実行が拒否されました: %v", err)
	}
}

func TestPreflightQualityToolchainRecordsPassAttempt(t *testing.T) {
	cfg := newQualityContractConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stubQualityPreflight(t, func(string) error { return nil })

	if err := preflightQualityToolchain(cfg, st); err != nil {
		t.Fatal(err)
	}
	stats := preflightStatsOrFail(t, st)
	if stats.PassCount != 1 || stats.FailureCount != 0 || stats.AvoidedModelCalls != 0 {
		t.Fatalf("preflight成功が集計されていません: %+v", stats)
	}
	if stats.LastOutcome != state.PreflightOutcomePass || stats.LastAt.IsZero() {
		t.Fatalf("preflight成功の集計が不正です: %+v", stats)
	}
	if st.ReadOr("task.id", "") != "" {
		t.Fatal("preflight成功でlifecycle task idが生成されました")
	}
}

func TestPreflightQualityToolchainRecordsFailureOnFreshStore(t *testing.T) {
	cfg := newQualityContractConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stubQualityPreflight(t, func(string) error {
		return &harnesslint.QualityToolVersionMismatch{Tool: "go", Observed: "1.26.1", Required: "1.25.4"}
	})

	if err := preflightQualityToolchain(cfg, st); err == nil {
		t.Fatal("mismatchを検出できません")
	}
	stats := preflightStatsOrFail(t, st)
	if stats.FailureCount != 1 || stats.AvoidedModelCalls != 1 || stats.LastOutcome != state.PreflightOutcomeFail {
		t.Fatalf("fresh storeのpreflight失敗が集計されていません: %+v", stats)
	}
	if st.ReadOr("task.id", "") != "" {
		t.Fatal("preflight失敗でlifecycle task idが生成されました")
	}
}

func TestPreflightQualityToolchainSkipsRepoWithoutQualitySurface(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stubQualityPreflight(t, func(string) error {
		return &harnesslint.QualityToolVersionMismatch{Tool: "go", Observed: "1.26.1", Required: "1.25.4"}
	})

	if err := preflightQualityToolchain(cfg, st); err != nil {
		t.Fatalf("quality対象外repositoryでpreflightが実行されました: %v", err)
	}
	if _, err := st.LoadPreflightStats(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("skip時にpreflight集計が書かれました: %v", err)
	}
}

func TestPreflightQualityToolchainSkipsEmptyRepoRoot(t *testing.T) {
	stubQualityPreflight(t, func(string) error {
		t.Fatal("repo rootが空の場合にpreflightが実行されました")
		return nil
	})
	if err := preflightQualityToolchain(config.AppConfig{}, nil); err != nil {
		t.Fatal(err)
	}
}

func TestStatsOutputIncludesPreflightAggregate(t *testing.T) {
	cfg := newQualityContractConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	stubQualityPreflight(t, func(string) error { return nil })
	if err := preflightQualityToolchain(cfg, st); err != nil {
		t.Fatal(err)
	}
	stubQualityPreflight(t, func(string) error {
		return &harnesslint.QualityToolVersionMismatch{Tool: "shellcheck", Observed: "0.10.0", Required: "0.11.0"}
	})
	if err := preflightQualityToolchain(cfg, st); err == nil {
		t.Fatal("mismatchを検出できません")
	}

	output := executeStatsOutput(t, st)
	if output.Preflight.Status != "ok" {
		t.Fatalf("preflight status = %q: %+v", output.Preflight.Status, output.Preflight)
	}
	if output.Preflight.PassCount != 1 || output.Preflight.FailureCount != 1 || output.Preflight.AvoidedModelCalls != 1 {
		t.Fatalf("preflight = %+v", output.Preflight)
	}
	if output.Preflight.LastOutcome != state.PreflightOutcomeFail || output.Preflight.LastAt == nil {
		t.Fatalf("preflight last = %+v", output.Preflight)
	}
	if output.Preflight.TotalDurationMS < output.Preflight.LastDurationMS ||
		output.Preflight.MaxDurationMS < output.Preflight.LastDurationMS {
		t.Fatalf("preflight durations = %+v", output.Preflight)
	}
}

func TestStatsOutputPreflightStatusUnknownOnMalformedAggregate(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(st.Path("preflight-stats.json"), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}

	output := executeStatsOutput(t, st)
	if output.Preflight.Status != "unknown" || output.Preflight.Error == "" {
		t.Fatalf("preflight = %+v", output.Preflight)
	}
	if output.Preflight.PassCount != 0 || output.Preflight.FailureCount != 0 {
		t.Fatalf("unknown時に集計値を出力しています: %+v", output.Preflight)
	}
}

func TestStatsOutputPreflightStatusNoneWithoutAttempts(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}

	output := executeStatsOutput(t, st)
	if output.Preflight.Status != statusNone {
		t.Fatalf("preflight = %+v", output.Preflight)
	}
}

func TestWriteProcessErrorQualityPreflightMismatchDetail(t *testing.T) {
	err := &QualityPreflightError{
		Tool:           "golangci-lint",
		Observed:       "2.13.1",
		Required:       "2.7.0",
		Classification: harnesslint.QualityToolEnvironmentFailure,
		Cause:          &harnesslint.QualityToolVersionMismatch{Tool: "golangci-lint", Observed: "2.13.1", Required: "2.7.0"},
		DurationMS:     42,
	}
	envelope, raw := writeProcessErrorJSON(t, err)
	if envelope.Error.Kind != "quality_preflight_failed" {
		t.Fatalf("kind = %q: %s", envelope.Error.Kind, raw)
	}
	wantMessage := "quality toolchain preflight failed before any model call (golangci-lint=2.13.1, required=2.7.0); run ./install-quality-tools.sh and rerun the same command"
	if envelope.Error.Message != wantMessage {
		t.Fatalf("message = %q: %s", envelope.Error.Message, raw)
	}
	for key, want := range map[string]any{
		"tool": "golangci-lint", "observed": "2.13.1", "required": "2.7.0",
		"repair": "./install-quality-tools.sh", "version_authority": "quality-tools.yml",
		"classification": "environment",
		"duration_ms":    float64(42), "model_calls": float64(0),
		"cause": "quality tool version mismatch: golangci-lint=2.13.1, required=2.7.0",
	} {
		if envelope.Error.Detail[key] != want {
			t.Fatalf("detail[%s] = %#v want %#v: %s", key, envelope.Error.Detail[key], want, raw)
		}
	}
	if len(envelope.Error.Detail) != 9 {
		t.Fatalf("detail = %#v: %s", envelope.Error.Detail, raw)
	}
}

func TestWriteProcessErrorQualityPreflightCauseOnlyDetail(t *testing.T) {
	err := &QualityPreflightError{
		Classification: harnesslint.QualityToolInternalFailure,
		Cause:          &harnesslint.QualityToolContractError{Cause: errors.New("read quality tool contract: missing")},
		DurationMS:     7,
	}
	envelope, raw := writeProcessErrorJSON(t, err)
	if envelope.Error.Kind != "quality_preflight_failed" {
		t.Fatalf("kind = %q: %s", envelope.Error.Kind, raw)
	}
	wantMessage := "quality toolchain preflight failed before any model call; run ./install-quality-tools.sh and rerun the same command: read quality tool contract: missing"
	if envelope.Error.Message != wantMessage {
		t.Fatalf("message = %q: %s", envelope.Error.Message, raw)
	}
	for key, want := range map[string]any{
		"repair": "./install-quality-tools.sh", "version_authority": "quality-tools.yml",
		"classification": "internal",
		"duration_ms":    float64(7), "model_calls": float64(0),
		"cause": "read quality tool contract: missing",
	} {
		if envelope.Error.Detail[key] != want {
			t.Fatalf("detail[%s] = %#v want %#v: %s", key, envelope.Error.Detail[key], want, raw)
		}
	}
	if len(envelope.Error.Detail) != 6 {
		t.Fatalf("detail = %#v: %s", envelope.Error.Detail, raw)
	}
}

func TestNewQualityPreflightErrorClassifiesTypedFailures(t *testing.T) {
	mismatchErr := newQualityPreflightError(
		&harnesslint.QualityToolVersionMismatch{Tool: "shfmt", Observed: "3.8.0", Required: "3.13.1"},
		3*time.Millisecond,
	)
	if mismatchErr.Classification != harnesslint.QualityToolEnvironmentFailure || mismatchErr.Tool != "shfmt" {
		t.Fatalf("mismatch分類 = %+v", mismatchErr)
	}

	contractErr := newQualityPreflightError(
		&harnesslint.QualityToolContractError{Cause: errors.New("quality tool contract is incomplete")},
		time.Millisecond,
	)
	if contractErr.Classification != harnesslint.QualityToolInternalFailure {
		t.Fatalf("contract分類 = %+v", contractErr)
	}

	unclassifiedErr := newQualityPreflightError(errors.New("unclassified harness fault"), time.Millisecond)
	if unclassifiedErr.Classification != harnesslint.QualityToolInternalFailure {
		t.Fatalf("未分類faultのfallback分類 = %+v", unclassifiedErr)
	}
}
