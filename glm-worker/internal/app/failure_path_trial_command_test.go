package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathtrial"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseCommandFailurePathTrial(t *testing.T) {
	command, err := ParseCommand([]string{"--failure-path-trial"})
	if err != nil || command.Mode != ModeFailurePathTrial || command.ReferencePath != "" {
		t.Fatalf("command = %#v err = %v", command, err)
	}
	command, err = ParseCommand([]string{"--failure-path-trial", "--labels", "/tmp/labels.json"})
	if err != nil || command.Mode != ModeFailurePathTrial || command.ReferencePath != "/tmp/labels.json" {
		t.Fatalf("command = %#v err = %v", command, err)
	}
	for _, args := range [][]string{
		{"--failure-path-trial", "extra"},
		{"--failure-path-trial", "--labels"},
		{"--failure-path-trial", "--labels", ""},
		{"--failure-path-trial", "--unknown", "x"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("usage errorを期待: %#v", args)
		}
	}
}

func newFailurePathTrialRegistryState(t *testing.T) (*state.StateStore, failurepathtrial.Record) {
	t.Helper()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	record := failurepathtrial.Record{
		TaskID:  "trial-task",
		Outcome: failurepathtrial.OutcomeObserved,
		Classes: []string{failurepathtrial.ClassExternalModelInvocation},
		Findings: []failurepathtrial.Finding{
			{Target: "glm-worker/internal/runner/probe.go:90", Class: failurepathtrial.ClassExternalModelInvocation, Issue: "deadlineなし"},
		},
		AddedGLM: &failurepathtrial.AddedGLMUsage{Calls: 1, InputTokens: 200, OutputTokens: 60, WallDurationMS: 4000},
	}
	registry := failurepathtrial.Registry{}.WithRecord(record)
	if err := failurepathtrial.SaveRegistry(st.Path(failurepathtrial.RegistryFile), registry); err != nil {
		t.Fatal(err)
	}
	return st, record
}

func TestExecuteFailurePathTrialPrintsSummary(t *testing.T) {
	st, _ := newFailurePathTrialRegistryState(t)
	var output bytes.Buffer
	if err := executeFailurePathTrial(Command{Mode: ModeFailurePathTrial}, st, &output); err != nil {
		t.Fatal(err)
	}
	var summary failurepathtrial.Summary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatalf("summary JSON: %v: %s", err, output.String())
	}
	if summary.CohortSize != 1 || summary.FindingsTotal != 1 || summary.UnlabeledFindings != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.EarlyStopEligible {
		t.Fatal("未label findingがあるのに早期停止可能になりました")
	}
}

func TestExecuteFailurePathTrialAppliesLabels(t *testing.T) {
	st, record := newFailurePathTrialRegistryState(t)
	labelsPath := filepath.Join(t.TempDir(), "labels.json")
	labels := failurepathtrial.LabelInput{
		Schema: failurepathtrial.LabelsSchema,
		TaskID: record.TaskID,
		FindingDispositions: []failurepathtrial.FindingDispositionInput{
			{Index: 0, Disposition: failurepathtrial.DispositionTruePositive},
		},
		AvoidedReviewFixWaves: 1,
		Usage:                 &failurepathtrial.UsageComparison{Measured: true, CodexTokens: 5000, SolTokens: 900},
	}
	data, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(labelsPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := executeFailurePathTrial(Command{Mode: ModeFailurePathTrial, ReferencePath: labelsPath}, st, &output); err != nil {
		t.Fatal(err)
	}
	var summary failurepathtrial.Summary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.LabeledFindings != 1 || summary.TruePositiveAdversarialOnly != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.UsageComparison.MeasuredTasks != 1 || summary.UsageComparison.PendingTasks != 0 {
		t.Fatalf("usage comparison = %+v", summary.UsageComparison)
	}

	registry, err := failurepathtrial.LoadRegistry(st.Path(failurepathtrial.RegistryFile))
	if err != nil {
		t.Fatal(err)
	}
	if registry.Records[0].Findings[0].Label == nil {
		t.Fatal("labelがregistryへ保存されていません")
	}
}

func TestExecuteFailurePathTrialRejectsLabelsForUnknownTask(t *testing.T) {
	st, _ := newFailurePathTrialRegistryState(t)
	labelsPath := filepath.Join(t.TempDir(), "labels.json")
	labels := failurepathtrial.LabelInput{Schema: failurepathtrial.LabelsSchema, TaskID: "missing-task"}
	data, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(labelsPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executeFailurePathTrial(Command{Mode: ModeFailurePathTrial, ReferencePath: labelsPath}, st, &bytes.Buffer{}); err == nil {
		t.Fatal("未知taskのlabelsでerrorが返りませんでした")
	}
}

func TestExecuteFailurePathTrialEmptyRegistry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := executeFailurePathTrial(Command{Mode: ModeFailurePathTrial}, st, &output); err != nil {
		t.Fatal(err)
	}
	var summary failurepathtrial.Summary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.CohortSize != 0 || !summary.CohortOpen {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestExecuteFailurePathTrialRejectsCorruptRegistry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := st.Path(failurepathtrial.RegistryFile)
	corrupt := []byte(`{"schema":`)
	if err := os.WriteFile(registryPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := executeFailurePathTrial(Command{Mode: ModeFailurePathTrial}, st, &bytes.Buffer{}); err == nil {
		t.Fatal("corrupt registryが空summary扱いになりました")
	}
	after, err := os.ReadFile(registryPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(corrupt) {
		t.Fatalf("corrupt registryが上書きされました: %q", after)
	}
}
