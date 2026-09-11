package app

import (
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func TestTestImpactUsesStructuredValidationWhenOperationCategoryIsOther(t *testing.T) {
	cfg := newAppConfig(t)
	st, err := state.NewStateStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := st.StartNewTask()
	if err != nil {
		t.Fatal(err)
	}
	base := time.Date(2026, 9, 11, 13, 0, 0, 0, time.UTC)
	appendTestImpactEventLog(t, st, taskID,
		state.TaskEventRecord{
			CallID: "call-validation", Role: "worker", Phase: "worker-new", Seq: 1, Timestamp: base, Kind: "assistant",
			Blocks: []state.TaskBlockSummary{{
				Type: "tool_use", Name: "Bash", ToolID: "validation-1", OperationCategory: state.OperationCategoryOther,
				Validation: []state.TaskValidationObservation{{
					Form: "go-test", GateClass: state.ValidationGateClassTest, Suite: "go-test", SnapshotID: "snapshot-1", Phase: "worker-new", Attempt: state.ValidationAttemptInitial,
				}},
			}},
		},
		state.TaskEventRecord{
			CallID: "call-validation", Role: "worker", Phase: "worker-new", Seq: 2, Timestamp: base.Add(time.Second), Kind: "user",
			Blocks: []state.TaskBlockSummary{{
				Type: "tool_result", Name: "Bash", ToolID: "validation-1", OperationCategory: state.OperationCategoryOther, DurationMS: 900,
				Validation: []state.TaskValidationObservation{{
					Form: "go-test", GateClass: state.ValidationGateClassTest, Suite: "go-test", SnapshotID: "snapshot-1", Phase: "worker-new", Attempt: state.ValidationAttemptInitial, Result: state.ValidationResultUnknown,
				}},
			}},
		},
	)

	decoded := executeTestImpact(t, st)
	report := decoded["report"].(map[string]any)
	if report["evaluation"].(map[string]any)["suite_coverage"] != state.TestImpactSuiteCoveragePartial {
		t.Fatalf("evaluation = %#v", report["evaluation"])
	}
	task := findTestImpactTask(t, decoded, taskID)
	validations, _ := task["validations"].([]any)
	if len(validations) != 1 {
		t.Fatalf("validations = %#v", validations)
	}
	validation := validations[0].(map[string]any)
	if validation["gate_class"] != state.ValidationGateClassTest || validation["suite"] != "go-test" || validation["runs"].(float64) != 1 {
		t.Fatalf("structured validation = %#v", validation)
	}
	if findTestImpactOperation(task, state.OperationCategoryTest) != nil {
		t.Fatalf("legacy operation category unexpectedly became test: %#v", task)
	}
}
