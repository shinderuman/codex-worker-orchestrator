package app

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathadvisory"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestParseCommandFailurePathAdvisory(t *testing.T) {
	command, err := ParseCommand([]string{"--failure-path-advisory"})
	if err != nil || command.Mode != ModeFailurePathAdvisory || command.ReferencePath != "" {
		t.Fatalf("command = %#v err = %v", command, err)
	}
	command, err = ParseCommand([]string{"--failure-path-advisory", "--labels", "/tmp/labels.json"})
	if err != nil || command.Mode != ModeFailurePathAdvisory || command.ReferencePath != "/tmp/labels.json" {
		t.Fatalf("command = %#v err = %v", command, err)
	}
	for _, args := range [][]string{
		{"--failure-path-advisory", "extra"},
		{"--failure-path-advisory", "--labels"},
		{"--failure-path-advisory", "--labels", ""},
		{"--failure-path-advisory", "--unknown", "x"},
	} {
		if _, err := ParseCommand(args); err == nil {
			t.Fatalf("usage errorを期待: %#v", args)
		}
	}
}

func newFailurePathAdvisoryRegistryState(t *testing.T) (*state.StateStore, failurepathadvisory.Record) {
	t.Helper()
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	record := failurepathadvisory.Record{
		TaskID:  "trial-task",
		Outcome: failurepathadvisory.OutcomeObserved,
		Classes: []string{failurepathadvisory.ClassExternalModelInvocation},
		Findings: []failurepathadvisory.Finding{
			{Target: "glm-worker/internal/runner/probe.go:90", Class: failurepathadvisory.ClassExternalModelInvocation, Issue: "deadlineなし"},
		},
		AddedGLM: &failurepathadvisory.AddedGLMUsage{Calls: 1, InputTokens: 200, OutputTokens: 60, WallDurationMS: 4000},
	}
	registry := failurepathadvisory.Registry{}.WithRecord(record)
	if err := failurepathadvisory.SaveRegistry(st.Path(failurepathadvisory.RegistryFile), registry); err != nil {
		t.Fatal(err)
	}
	return st, record
}

func TestExecuteFailurePathAdvisoryPrintsSummary(t *testing.T) {
	st, _ := newFailurePathAdvisoryRegistryState(t)
	var output bytes.Buffer
	if err := executeFailurePathAdvisory(Command{Mode: ModeFailurePathAdvisory}, st, &output); err != nil {
		t.Fatal(err)
	}
	var summary failurepathadvisory.Summary
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

func TestExecuteFailurePathAdvisoryAppliesLabels(t *testing.T) {
	st, record := newFailurePathAdvisoryRegistryState(t)
	labelsPath := filepath.Join(t.TempDir(), "labels.json")
	zero, two := 0, 2
	labels := failurepathadvisory.LabelInput{
		Schema: failurepathadvisory.LabelsSchema,
		TaskID: record.TaskID,
		FindingDispositions: []failurepathadvisory.FindingDispositionInput{
			{Index: 0, Disposition: failurepathadvisory.DispositionTruePositive},
		},
		AvoidedReviewFixWaves: 1,
		FalseNegatives:        &zero,
		HumanInterventions:    &two,
		Usage:                 &failurepathadvisory.UsageComparison{Measured: true, CodexTokens: 5000, SolTokens: 900},
	}
	data, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(labelsPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	var output bytes.Buffer
	if err := executeFailurePathAdvisory(Command{Mode: ModeFailurePathAdvisory, ReferencePath: labelsPath}, st, &output); err != nil {
		t.Fatal(err)
	}
	var summary failurepathadvisory.Summary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.LabeledFindings != 1 || summary.TruePositiveAdversarialOnly != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.UsageComparison.MeasuredTasks != 1 || summary.UsageComparison.PendingTasks != 0 {
		t.Fatalf("usage comparison = %+v", summary.UsageComparison)
	}
	if summary.FalseNegatives.MeasuredTasks != 1 || summary.FalseNegatives.Total != 0 {
		t.Fatalf("false_negatives = %+v want measured zero", summary.FalseNegatives)
	}
	if summary.HumanInterventions.MeasuredTasks != 1 || summary.HumanInterventions.Total != 2 {
		t.Fatalf("human_interventions = %+v", summary.HumanInterventions)
	}

	registry, err := failurepathadvisory.LoadRegistry(st.Path(failurepathadvisory.RegistryFile))
	if err != nil {
		t.Fatal(err)
	}
	if registry.Records[0].Findings[0].Label == nil {
		t.Fatal("labelがregistryへ保存されていません")
	}
}

func TestExecuteFailurePathAdvisoryRejectsLabelsForUnknownTask(t *testing.T) {
	st, _ := newFailurePathAdvisoryRegistryState(t)
	labelsPath := filepath.Join(t.TempDir(), "labels.json")
	labels := failurepathadvisory.LabelInput{Schema: failurepathadvisory.LabelsSchema, TaskID: "missing-task"}
	data, err := json.Marshal(labels)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(labelsPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := executeFailurePathAdvisory(Command{Mode: ModeFailurePathAdvisory, ReferencePath: labelsPath}, st, &bytes.Buffer{}); err == nil {
		t.Fatal("未知taskのlabelsでerrorが返りませんでした")
	}
}

func TestExecuteFailurePathAdvisoryEmptyRegistry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if err := executeFailurePathAdvisory(Command{Mode: ModeFailurePathAdvisory}, st, &output); err != nil {
		t.Fatal(err)
	}
	var summary failurepathadvisory.Summary
	if err := json.Unmarshal(output.Bytes(), &summary); err != nil {
		t.Fatal(err)
	}
	if summary.CohortSize != 0 || !summary.CohortOpen {
		t.Fatalf("summary = %+v", summary)
	}
}

func TestExecuteFailurePathAdvisoryRejectsCorruptRegistry(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	registryPath := st.Path(failurepathadvisory.RegistryFile)
	corrupt := []byte(`{"schema":`)
	if err := os.WriteFile(registryPath, corrupt, 0o600); err != nil {
		t.Fatal(err)
	}

	if err := executeFailurePathAdvisory(Command{Mode: ModeFailurePathAdvisory}, st, &bytes.Buffer{}); err == nil {
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
