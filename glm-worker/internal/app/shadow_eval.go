package app

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/machinecli"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/report"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/shadoweval"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type shadowEvalArtifacts struct {
	Input      string `json:"input"`
	Decisions  string `json:"decisions,omitempty"`
	Comparison string `json:"comparison"`
}

type shadowEvalOutput struct {
	TaskID                    string                    `json:"task_id"`
	InputItems                int                       `json:"input_items"`
	ItemsSHA256               string                    `json:"items_sha256"`
	TypedSchemaValid          bool                      `json:"typed_schema_valid"`
	ValidationErrors          []string                  `json:"validation_errors"`
	ShadowFailure             *shadoweval.ShadowFailure `json:"shadow_failure,omitempty"`
	ReferenceKnown            int                       `json:"reference_known"`
	ReferenceUnknown          int                       `json:"reference_unknown"`
	DispositionAgreementKnown shadoweval.AgreementCount `json:"disposition_agreement_known"`
	Run                       shadoweval.ComparisonRun  `json:"run"`
	Artifacts                 shadowEvalArtifacts       `json:"artifacts"`
}

var shadowDecisionCaller = func(r *runner.ClaudeRunner, prompt string, schema string) (shadoweval.CallMetrics, json.RawMessage, []string, error) {
	result, err := r.Decide(shadoweval.ModelAlias, shadoweval.ModelEffort, schema, prompt)
	metrics := shadoweval.CallMetrics{
		DurationMS:    result.DurationMS,
		DurationAPIMS: result.DurationAPIMS,
		TotalCostUSD:  result.TotalCostUSD,
		InputBytes:    len(prompt),
		Usage: shadoweval.UsageMetrics{
			InputTokens:              result.Usage.InputTokens,
			CacheCreationInputTokens: result.Usage.CacheCreationInputTokens,
			CacheReadInputTokens:     result.Usage.CacheReadInputTokens,
			OutputTokens:             result.Usage.OutputTokens,
		},
	}
	return metrics, result.StructuredOutput, result.SettingEnvKeys, err
}

func executeShadowEval(cmd Command, cfg config.AppConfig, st *state.StateStore, stdout io.Writer) error {
	if err := validateShadowEvalTaskID(cmd.Payload); err != nil {
		return err
	}
	logs, err := st.ReadModelCallLogs(cmd.Payload)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &machinecli.NotFoundError{Message: fmt.Sprintf("task %sのtelemetryがありません", cmd.Payload)}
		}
		return err
	}
	records, _, err := report.ReadTaskEventRecords(st, cmd.Payload)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	input, err := shadoweval.BuildInput(cmd.Payload, logs, records)
	if err != nil {
		return &machinecli.NotFoundError{Message: err.Error()}
	}
	reference, err := loadShadowEvalReference(cmd.ReferencePath, input)
	if err != nil {
		return err
	}
	marshaled, err := shadoweval.MarshalInput(input)
	if err != nil {
		return err
	}
	prompt := shadoweval.ClassificationPrompt(input, marshaled)
	schema := shadoweval.DecisionsJSONSchema(len(input.Items))

	metrics, rawDecisions, settingEnvKeys, callErr := shadowDecisionCaller(runner.NewClaudeRunner(cfg, st), prompt, schema)
	failure, decisions, validationErrors := evaluateShadowDecisions(rawDecisions, callErr, input)
	comparison := shadoweval.BuildComparison(input, decisions, validationErrors, failure, reference, shadoweval.NewRun(metrics, settingEnvKeys))
	if callErr != nil {
		rawDecisions = nil
	}

	artifacts, err := writeShadowEvalArtifacts(st, cmd.Payload, marshaled, rawDecisions, comparison)
	if err != nil {
		return err
	}
	return machinecli.WriteJSON(stdout, shadowEvalOutput{
		TaskID:                    comparison.TaskID,
		InputItems:                comparison.InputItems,
		ItemsSHA256:               comparison.ItemsSHA256,
		TypedSchemaValid:          comparison.TypedSchemaValid,
		ValidationErrors:          comparison.ValidationErrors,
		ShadowFailure:             comparison.ShadowFailure,
		ReferenceKnown:            comparison.ReferenceKnown,
		ReferenceUnknown:          comparison.ReferenceUnknown,
		DispositionAgreementKnown: comparison.DispositionAgreementKnown,
		Run:                       comparison.Run,
		Artifacts:                 artifacts,
	})
}

func loadShadowEvalReference(path string, input shadoweval.ShadowInput) (shadoweval.Reference, error) {
	if path == "" {
		return shadoweval.Reference{}, nil
	}
	reference, err := shadoweval.LoadReference(path)
	if err != nil {
		return shadoweval.Reference{}, err
	}
	if err := shadoweval.ValidateReferenceAgainstInput(reference, input); err != nil {
		return shadoweval.Reference{}, err
	}
	return reference, nil
}

func evaluateShadowDecisions(rawDecisions json.RawMessage, callErr error, input shadoweval.ShadowInput) (*shadoweval.ShadowFailure, []shadoweval.Decision, []string) {
	if callErr != nil {
		return &shadoweval.ShadowFailure{
			Kind:   shadoweval.ShadowFailureProvider,
			Detail: boundedDecisionFailureDetail(callErr),
		}, nil, nil
	}
	decisions, validationErrors := shadoweval.ParseDecisions(rawDecisions, input)
	if len(validationErrors) > 0 {
		return &shadoweval.ShadowFailure{Kind: shadoweval.ShadowFailureSchemaInvalid}, decisions, validationErrors
	}
	return nil, decisions, validationErrors
}

func boundedDecisionFailureDetail(callErr error) string {
	var callFailure *runner.DecisionCallError
	if errors.As(callErr, &callFailure) {
		return callFailure.Error()
	}
	if runner.IsStructuredOutputError(callErr) {
		return "decision呼出がstructured outputを返しませんでした"
	}
	return "decision呼出が失敗しました"
}

func writeShadowEvalArtifacts(st *state.StateStore, taskID string, marshaledInput []byte, rawDecisions json.RawMessage, comparison shadoweval.Comparison) (shadowEvalArtifacts, error) {
	runDir := filepath.Join(st.ArtifactDir(taskID), "shadow-eval", time.Now().UTC().Format("20060102T150405.000000000Z"))
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		return shadowEvalArtifacts{}, fmt.Errorf("shadow評価artifact dirを作成できません: %w", err)
	}
	artifacts := shadowEvalArtifacts{
		Input:      filepath.Join(runDir, "shadow-input.json"),
		Comparison: filepath.Join(runDir, "shadow-comparison.json"),
	}
	if len(rawDecisions) > 0 {
		artifacts.Decisions = filepath.Join(runDir, "shadow-decisions.json")
	}
	comparisonJSON, err := json.Marshal(comparison)
	if err != nil {
		return shadowEvalArtifacts{}, fmt.Errorf("shadow評価comparisonをJSON化できません: %w", err)
	}
	if err := os.WriteFile(artifacts.Input, marshaledInput, 0o600); err != nil {
		return shadowEvalArtifacts{}, fmt.Errorf("shadow評価input artifactを書けません: %w", err)
	}
	if err := os.WriteFile(artifacts.Comparison, comparisonJSON, 0o600); err != nil {
		return shadowEvalArtifacts{}, fmt.Errorf("shadow評価comparison artifactを書けません: %w", err)
	}
	if artifacts.Decisions != "" {
		if err := os.WriteFile(artifacts.Decisions, rawDecisions, 0o600); err != nil {
			return shadowEvalArtifacts{}, fmt.Errorf("shadow評価decisions artifactを書けません: %w", err)
		}
	}
	return artifacts, nil
}

func validateShadowEvalTaskID(taskID string) error {
	if taskID == "" || taskID == "." || taskID == ".." || filepath.IsAbs(taskID) || strings.ContainsAny(taskID, `/\`) {
		return machinecli.UsageErrorf("%s", shadowEvalUsage)
	}
	return nil
}
