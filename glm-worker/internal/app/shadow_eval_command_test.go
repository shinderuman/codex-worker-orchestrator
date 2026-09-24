package app

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/shadoweval"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseCommandShadowEval(t *testing.T) {
	command, err := ParseCommand([]string{"--shadow-eval", "task-1"})
	if err != nil || command.Mode != ModeShadowEval || command.Payload != "task-1" || command.ReferencePath != "" {
		t.Fatalf("command = %#v err = %v", command, err)
	}
	command, err = ParseCommand([]string{"--shadow-eval", "task-1", "--reference", "/tmp/ref.json"})
	if err != nil || command.Payload != "task-1" || command.ReferencePath != "/tmp/ref.json" {
		t.Fatalf("command = %#v err = %v", command, err)
	}
	for _, args := range [][]string{
		{"--shadow-eval"},
		{"--shadow-eval", "task-1", "extra"},
		{"--shadow-eval", "task-1", "--reference"},
		{"--shadow-eval", "task-1", "--reference", ""},
		{"--shadow-eval", "task-1", "--unknown", "x"},
		{"--shadow-eval", "task-1", "--reference", "/tmp/a.json", "--reference", "/tmp/b.json"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("usage errorを期待: %#v", args)
		}
	}
}

func newShadowEvalState(t *testing.T) (*state.StateStore, string, []byte) {
	t.Helper()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 24, 2, 0, 0, 0, time.UTC)
	response, _ := json.Marshal(map[string]string{"summary": "worker summary"})
	st.RecordModelCallLog(state.ModelCallLog{
		Version: state.ModelCallLogVersion, CallType: state.CallTypeTask, CallID: "11111111-1111-4111-8111-111111111111",
		TaskID: taskID, Role: state.WorkerRole, Phase: "worker-new", PacketStatus: "IMPLEMENTED", Outcome: "success",
		StartedAt: base, CompletedAt: base.Add(time.Minute), WallDurationMS: 60000, Response: string(response),
	})
	st.RecordModelCallLog(state.ModelCallLog{
		Version: state.ModelCallLogVersion, CallType: state.CallTypeTask, CallID: "22222222-2222-4222-8222-222222222222",
		TaskID: taskID, Role: state.ReviewerRole, Phase: "reviewer-1-high-floor", PacketStatus: "FIX_REQUIRED", Outcome: "success",
		StartedAt: base.Add(time.Hour), CompletedAt: base.Add(time.Hour).Add(time.Minute), WallDurationMS: 30000,
	})
	telemetryPath := st.ModelCallLogPath(taskID)
	before, err := os.ReadFile(telemetryPath)
	if err != nil {
		t.Fatal(err)
	}
	return st, taskID, before
}

func fakeShadowDecision(t *testing.T, raw json.RawMessage, callErr error) {
	t.Helper()
	original := shadowDecisionCaller
	shadowDecisionCaller = func(_ *runner.ClaudeRunner, prompt string, _ string) (shadoweval.CallMetrics, json.RawMessage, []string, error) {
		metrics := shadoweval.CallMetrics{
			DurationMS: 16551, TotalCostUSD: 0.077, InputBytes: len(prompt),
			Usage: shadoweval.UsageMetrics{InputTokens: 5688, OutputTokens: 1970},
		}
		if callErr != nil {
			return metrics, json.RawMessage(`{"decisions":[{"call_id":"partial-provider-output"}]}`), nil, callErr
		}
		return metrics, raw, []string{"ANTHROPIC_AUTH_TOKEN"}, nil
	}
	t.Cleanup(func() { shadowDecisionCaller = original })
}

func runShadowEval(t *testing.T, st *state.StateStore, taskID string, referencePath string) (map[string]any, error) {
	t.Helper()
	var stdout bytes.Buffer
	err := executeShadowEval(Command{Mode: ModeShadowEval, Payload: taskID, ReferencePath: referencePath}, config.AppConfig{}, st, &stdout)
	if err != nil {
		return nil, err
	}
	return decodeSingleLineJSON(t, stdout.String()), nil
}

func shadowDecisionRaw(t *testing.T) json.RawMessage {
	t.Helper()
	data, err := json.Marshal(map[string]any{"decisions": []any{
		shadowDecisionJSON("11111111-1111-4111-8111-111111111111", 0.05, 0.1, "accept", "worker"),
		shadowDecisionJSON("22222222-2222-4222-8222-222222222222", 0.05, 0.6, "fix", "worker"),
	}})
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func shadowDecisionJSON(callID string, noise float64, risk float64, disposition string, owner string) map[string]any {
	return map[string]any{
		"call_id":                      callID,
		"noise_probability":            noise,
		"noise_confidence":             0.8,
		"legitimate_retry_probability": 0.1,
		"legitimate_retry_confidence":  0.7,
		"correctness_risk_probability": risk,
		"correctness_risk_confidence":  0.7,
		"owner_category":               owner,
		"owner_probability":            0.95,
		"owner_confidence":             0.9,
		"disposition_category":         disposition,
		"disposition_probability":      0.8,
		"disposition_confidence":       0.7,
		"sol_escalation_probability":   0.1,
		"sol_escalation_confidence":    0.6,
	}
}

func TestExecuteShadowEvalProducesComparisonArtifactsWithoutTouchingTelemetry(t *testing.T) {
	st, taskID, telemetryBefore := newShadowEvalState(t)
	fakeShadowDecision(t, shadowDecisionRaw(t), nil)

	output, err := runShadowEval(t, st, taskID, "")
	if err != nil {
		t.Fatal(err)
	}
	if output["task_id"] != taskID || output["input_items"] != float64(2) || output["typed_schema_valid"] != true {
		t.Fatalf("output = %#v", output)
	}
	if output["reference_known"] != float64(0) || output["reference_unknown"] != float64(2) {
		t.Fatalf("reference = %#v", output)
	}
	agreement := output["disposition_agreement_known"].(map[string]any)
	if agreement["denominator"] != float64(0) {
		t.Fatalf("agreement = %#v", agreement)
	}
	run := output["run"].(map[string]any)
	if run["control_authority"] != false || run["filter_authority"] != false || run["audit_mutated"] != false || run["user_settings_loaded_by_cli"] != false {
		t.Fatalf("run authority flags = %#v", run)
	}
	if run["duration_ms"] != float64(16551) || run["total_cost_usd"] != float64(0.077) {
		t.Fatalf("run metrics = %#v", run)
	}

	artifacts := output["artifacts"].(map[string]any)
	for _, key := range []string{"input", "decisions", "comparison"} {
		path := artifacts[key].(string)
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("artifact %s: %v", key, err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("artifact %s perm = %v", key, info.Mode().Perm())
		}
		if !strings.Contains(path, filepath.Join("artifacts", taskID, "shadow-eval")) {
			t.Fatalf("artifact path = %s", path)
		}
	}
	comparisonData, err := os.ReadFile(artifacts["comparison"].(string))
	if err != nil {
		t.Fatal(err)
	}
	var comparison shadoweval.Comparison
	if err := json.Unmarshal(comparisonData, &comparison); err != nil {
		t.Fatal(err)
	}
	if comparison.Run.SettingsSourceDisabled != true || comparison.Run.NoSessionPersistence != true || comparison.Run.ToolsDisabled != true {
		t.Fatalf("comparison run isolation = %#v", comparison.Run)
	}
	if len(comparison.DecisionsWithReference) != 2 || len(comparison.Thresholds) != 4 {
		t.Fatalf("comparison = %#v", comparison)
	}

	after, err := os.ReadFile(st.ModelCallLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(telemetryBefore, after) {
		t.Fatal("shadow評価がcanonical telemetryを変更しました")
	}
}

func TestExecuteShadowEvalComparesAgainstReference(t *testing.T) {
	st, taskID, _ := newShadowEvalState(t)
	fakeShadowDecision(t, shadowDecisionRaw(t), nil)
	referencePath := filepath.Join(t.TempDir(), "reference.json")
	reference := `{"schema":"system-one-shadow-reference/v1","bundle_task_id":"` + taskID + `","basis":"canonical Sol decision","labels":[{"call_id":"22222222-2222-4222-8222-222222222222","correctness_finding":true,"owner":"test-scenario","disposition":"fix","locator":"rows 1-5"}],"unknown_call_ids":["11111111-1111-4111-8111-111111111111"]}`
	if err := os.WriteFile(referencePath, []byte(reference), 0o600); err != nil {
		t.Fatal(err)
	}

	output, err := runShadowEval(t, st, taskID, referencePath)
	if err != nil {
		t.Fatal(err)
	}
	if output["reference_known"] != float64(1) || output["reference_unknown"] != float64(1) {
		t.Fatalf("reference = %#v", output)
	}
	agreement := output["disposition_agreement_known"].(map[string]any)
	if agreement["matching"] != float64(1) || agreement["denominator"] != float64(1) {
		t.Fatalf("agreement = %#v", agreement)
	}
}

func TestExecuteShadowEvalObservesProviderFailureAsShadowFailure(t *testing.T) {
	st, taskID, telemetryBefore := newShadowEvalState(t)
	fakeShadowDecision(t, nil, &runner.DecisionCallError{Model: "opus", Reason: "exit-status"})

	output, err := runShadowEval(t, st, taskID, "")
	if err != nil {
		t.Fatalf("provider失敗はcommand失敗にしない: %v", err)
	}
	if output["typed_schema_valid"] != false {
		t.Fatalf("output = %#v", output)
	}
	failure := output["shadow_failure"].(map[string]any)
	if failure["kind"] != "provider-failure" || !strings.Contains(failure["detail"].(string), "exit-status") {
		t.Fatalf("failure = %#v", failure)
	}
	artifacts := output["artifacts"].(map[string]any)
	if _, hasDecisions := artifacts["decisions"]; hasDecisions {
		t.Fatalf("decisions artifact = %#v", artifacts)
	}
	if _, err := os.Stat(artifacts["comparison"].(string)); err != nil {
		t.Fatalf("comparison artifact: %v", err)
	}
	after, err := os.ReadFile(st.ModelCallLogPath(taskID))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(telemetryBefore, after) {
		t.Fatal("provider失敗時もcanonical telemetryを変更してはいけません")
	}
}

func TestExecuteShadowEvalObservesSchemaViolationAsShadowFailure(t *testing.T) {
	st, taskID, _ := newShadowEvalState(t)
	decision := shadowDecisionJSON("11111111-1111-4111-8111-111111111111", 0.05, 0.1, "accept", "worker")
	decision["free_text"] = "schema外"
	raw, _ := json.Marshal(map[string]any{"decisions": []any{decision}})

	fakeShadowDecision(t, raw, nil)
	output, err := runShadowEval(t, st, taskID, "")
	if err != nil {
		t.Fatalf("schema違反はcommand失敗にしない: %v", err)
	}
	if output["typed_schema_valid"] != false {
		t.Fatalf("output = %#v", output)
	}
	failure := output["shadow_failure"].(map[string]any)
	if failure["kind"] != "schema-invalid" {
		t.Fatalf("failure = %#v", failure)
	}
	problems := output["validation_errors"].([]any)
	if len(problems) != 3 {
		t.Fatalf("validation errors = %#v", problems)
	}
}

func TestExecuteShadowEvalFailsClosedOnBadInput(t *testing.T) {
	st, taskID, _ := newShadowEvalState(t)
	fakeShadowDecision(t, shadowDecisionRaw(t), nil)

	_, missingTelemetryErr := runShadowEval(t, st, "33333333-3333-4333-8333-333333333333", "")
	if missingTelemetryErr == nil {
		t.Fatal("telemetry欠落taskはerror")
	}
	var notFound *machinecli.NotFoundError
	if !errors.As(missingTelemetryErr, &notFound) {
		t.Fatalf("NotFoundErrorを期待: %v", missingTelemetryErr)
	}

	var stdout bytes.Buffer
	if err := executeShadowEval(Command{Mode: ModeShadowEval, Payload: "../escape", ReferencePath: ""}, config.AppConfig{}, st, &stdout); err == nil {
		t.Fatal("path traversalなtask idはusage error")
	}

	referencePath := filepath.Join(t.TempDir(), "reference.json")
	if err := os.WriteFile(referencePath, []byte(`{"schema":"wrong","bundle_task_id":"`+taskID+`","labels":[]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runShadowEval(t, st, taskID, referencePath); err == nil || !strings.Contains(err.Error(), "reference schemaが不正") {
		t.Fatalf("不正referenceはfail closed: %v", err)
	}

	mismatchPath := filepath.Join(t.TempDir(), "reference.json")
	valid := `{"schema":"system-one-shadow-reference/v1","bundle_task_id":"other-task","labels":[]}`
	if err := os.WriteFile(mismatchPath, []byte(valid), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := runShadowEval(t, st, taskID, mismatchPath); err == nil || !strings.Contains(err.Error(), "bundle_task_idが対象taskと一致しません") {
		t.Fatalf("task不一致referenceはfail closed: %v", err)
	}
}
