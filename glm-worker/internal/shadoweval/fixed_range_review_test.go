package shadoweval

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestBuildInputProjectsMachineReadableEventEvidence(t *testing.T) {
	response, err := json.Marshal(map[string]string{"summary": "bounded summary"})
	if err != nil {
		t.Fatal(err)
	}
	logs := []state.ModelCallLog{{
		Version:   state.ModelCallLogVersion,
		CallType:  state.CallTypeTask,
		CallID:    "call-a",
		TaskID:    "task-1",
		Role:      state.ReviewerRole,
		Phase:     "reviewer-1",
		Outcome:   "success",
		Response:  string(response),
		StartedAt: time.Date(2026, 9, 24, 1, 0, 0, 0, time.UTC),
	}}
	records := []state.TaskEventRecord{
		{
			Version: 1, TaskID: "task-1", CallID: "call-a", Role: "reviewer", Phase: "reviewer-1", Seq: 7,
			Kind: "assistant", Subtype: "tool-result", SearchPaths: []string{"glm-worker/internal/workflow/workflow.go"},
			Blocks: []state.TaskBlockSummary{{
				Type: "tool_result", Name: "go test", OperationCategory: state.OperationCategoryTest, IsError: true,
				Validation: []state.TaskValidationObservation{{Form: "go-test", Suite: "./...", Result: state.ValidationResultFail}},
			}},
			Validation: &state.TaskValidationEvent{
				Attribution: "reviewer", Source: "tool", Form: "go-test", Suite: "./...", Scope: "repository",
				Result: state.ValidationResultFail, Evidence: "glm-worker/internal/workflow/workflow_test.go: failure",
			},
		},
		{Version: 1, TaskID: "task-1", CallID: "unattributed-to-shadow-item", Seq: 8, Kind: "system"},
	}

	input, err := BuildInput("task-1", logs, records)
	if err != nil {
		t.Fatal(err)
	}
	if len(input.Items) != 1 || len(input.Items[0].Events) != 1 {
		t.Fatalf("event evidence = %#v", input.Items)
	}
	event := input.Items[0].Events[0]
	if event.Seq != 7 || event.Kind != "assistant" || len(event.SearchPaths) != 1 || event.SearchPaths[0] != "glm-worker/internal/workflow/workflow.go" {
		t.Fatalf("event identity = %#v", event)
	}
	if len(event.Blocks) != 1 || event.Blocks[0].OperationCategory != state.OperationCategoryTest || !event.Blocks[0].IsError {
		t.Fatalf("block evidence = %#v", event.Blocks)
	}
	if event.Validation == nil || event.Validation.Result != state.ValidationResultFail || event.Validation.Evidence == "" {
		t.Fatalf("validation evidence = %#v", event.Validation)
	}
	if input.Items[0].SourceEvidenceBytes <= 0 || input.SourceEvidenceBytes <= input.Items[0].SourceEvidenceBytes {
		t.Fatalf("source evidence bytes must include attributed and unattributed canonical records: item=%d total=%d", input.Items[0].SourceEvidenceBytes, input.SourceEvidenceBytes)
	}
}

func TestBuildInputRetainsLateHighSignalEvidenceWithinBound(t *testing.T) {
	logs := []state.ModelCallLog{{
		Version:   state.ModelCallLogVersion,
		CallType:  state.CallTypeTask,
		CallID:    "call-a",
		TaskID:    "task-1",
		Role:      state.ReviewerRole,
		Phase:     "reviewer-1",
		Outcome:   "success",
		StartedAt: time.Date(2026, 9, 24, 1, 0, 0, 0, time.UTC),
	}}
	records := make([]state.TaskEventRecord, 0, 42)
	for seq := 1; seq <= 40; seq++ {
		records = append(records, state.TaskEventRecord{
			Version: 1, TaskID: "task-1", CallID: "call-a", Role: "reviewer", Phase: "reviewer-1",
			Seq: seq, Kind: "assistant", Subtype: "message",
		})
	}
	records = append(records,
		state.TaskEventRecord{
			Version: 1, TaskID: "task-1", CallID: "call-a", Role: "reviewer", Phase: "reviewer-1",
			Seq: 41, Kind: "assistant", Subtype: "tool-result",
			Validation: &state.TaskValidationEvent{
				Attribution: "reviewer", Source: "tool", Form: "go-test", Suite: "./...", Scope: "repository",
				Result: state.ValidationResultFail, Evidence: "late validation failure",
			},
		},
		state.TaskEventRecord{
			Version: 1, TaskID: "task-1", CallID: "call-a", Role: "reviewer", Phase: "reviewer-1",
			Seq: 42, Kind: "assistant", Subtype: "tool-result", IsError: true,
			SearchPaths: []string{"late.go"},
			Blocks: []state.TaskBlockSummary{{
				Type: "tool_result", Name: "go test", OperationCategory: state.OperationCategoryTest, IsError: true,
			}},
		},
	)

	first, err := BuildInput("task-1", logs, records)
	if err != nil {
		t.Fatal(err)
	}
	second, err := BuildInput("task-1", logs, records)
	if err != nil {
		t.Fatal(err)
	}
	if first.ItemsSHA256 != second.ItemsSHA256 {
		t.Fatalf("bounded selection is not deterministic: %s != %s", first.ItemsSHA256, second.ItemsSHA256)
	}
	if len(first.Items) != 1 || len(first.Items[0].Events) != eventEvidenceMaxItems {
		t.Fatalf("bounded event count = %#v", first.Items)
	}
	var validationSeen, errorSeen bool
	previousSeq := 0
	for _, event := range first.Items[0].Events {
		if event.Seq <= previousSeq {
			t.Fatalf("selected events lost chronological order: previous=%d current=%d", previousSeq, event.Seq)
		}
		previousSeq = event.Seq
		if event.Seq == 41 && event.Validation != nil && event.Validation.Result == state.ValidationResultFail {
			validationSeen = true
		}
		if event.Seq == 42 && event.IsError && len(event.Blocks) == 1 && event.Blocks[0].IsError && len(event.SearchPaths) == 1 {
			errorSeen = true
		}
	}
	if !validationSeen || !errorSeen {
		t.Fatalf("late correctness evidence was dropped: validation=%v error=%v events=%#v", validationSeen, errorSeen, first.Items[0].Events)
	}
}

func TestReductionEstimateUsesCanonicalSourceEvidenceBytes(t *testing.T) {
	input := ShadowInput{
		Schema:              InputSchema,
		TaskID:              "task-1",
		SourceEvidenceBytes: 1000,
		Items: []InputItem{
			{CallID: "noise", SourceEvidenceBytes: 100, Summary: "this text size must not drive the estimate"},
			{CallID: "keep", SourceEvidenceBytes: 400, Summary: "short"},
		},
	}
	decisions := []Decision{
		{
			CallID: "noise", DispositionCategory: "noise", NoiseProbability: 0.95, NoiseConfidence: 0.95,
			CorrectnessRiskProbability: 0.05, SolEscalationProbability: 0.05, OwnerCategory: "worker",
		},
		{
			CallID: "keep", DispositionCategory: "accept", NoiseProbability: 0.01, NoiseConfidence: 0.95,
			CorrectnessRiskProbability: 0.05, SolEscalationProbability: 0.05, OwnerCategory: "worker",
		},
	}
	comparison := BuildComparison(input, decisions, nil, nil, Reference{}, NewRun(CallMetrics{}, nil))
	if len(comparison.Thresholds) == 0 {
		t.Fatal("thresholds are missing")
	}
	for _, row := range comparison.Thresholds {
		if row.Candidates != 1 || row.EstimatedSolVisibleInputReduction != 0.1 {
			t.Fatalf("threshold %v source-evidence reduction = %#v", row.Threshold, row)
		}
	}
}
