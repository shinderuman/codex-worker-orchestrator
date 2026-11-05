package app

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type executionTransportStage struct {
	Path        string                               `json:"path"`
	Slots       []struct{ Name, Placeholder string } `json:"slots"`
	NextCommand []string                             `json:"next_command"`
}

func TestProjectedMaterializationUsesParentActionStaging(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)
	reentered, err := store.ReenterAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration,
		CandidateID: candidate.CandidateID,
	})
	if err != nil {
		t.Fatal(err)
	}
	handoff := buildParentHandoffWithConfig(cfg, runtime)
	command := projectedTransportCommand(t, handoff, "materialize", "")
	if command.ExpectedGeneration != reentered.Head.ControllerGeneration || command.SuspensionID != reentered.Suspension.SnapshotID {
		t.Fatalf("materialization handoff lost machine-owned identity: %#v", command)
	}
	exerciseProjectedExecutionTransport(t, cfg, store, handoff, "", "")
	after, err := store.LoadHead()
	if err != nil || after.LiveAttemptID == "" || after.LiveLeaseID == "" || after.ControllerGeneration <= reentered.Head.ControllerGeneration {
		t.Fatalf("typed materialization did not commit a new live lease: %#v %v", after, err)
	}
}

func TestProjectedUserTerminationUsesDeclaredReasonStaging(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, store, input, runtime, _ := userExecutionTerminationFixture(t)
	handoff := buildParentHandoffWithConfig(cfg, runtime)
	command := projectedTransportCommand(t, handoff, "user-terminate-execution", "execution_termination.reason")
	if command.ExecutionTermination == nil || command.ExecutionTermination.ExpectedGeneration != input.ExpectedGeneration ||
		command.ExecutionTermination.SourceAttemptID != input.SourceAttemptID || command.ExecutionTermination.Reason != "" {
		t.Fatalf("termination handoff lost exact user decision boundary: %#v", command)
	}
	exerciseProjectedExecutionTransport(t, cfg, store, handoff, "execution_termination.reason", "explicitly terminate this unfinished execution")
	after, err := store.LoadHead()
	if err != nil || after.UserTerminationRef == nil || after.ExecutionTaskRef != nil || after.LiveLeaseID != "" ||
		runtime.TaskStatus() != state.TaskStatusUserTerminated {
		t.Fatalf("typed user termination did not preserve terminated authority: %#v %v", after, err)
	}
}

func projectedTransportCommand(t *testing.T, handoff parentHandoffOutput, action, slot string) workflow.ControllerExecutionCommand {
	t.Helper()
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
		t.Fatalf("canonical execution handoff is not admitted: %#v", handoff)
	}
	if handoff.RequiredActionParameters["semantic_slot"] != slot {
		t.Fatalf("canonical semantic slot differs: %#v", handoff.RequiredActionParameters)
	}
	var command workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &command); err != nil || command.Action != action {
		t.Fatalf("canonical typed operation differs: %#v %v", command, err)
	}
	return command
}

func prepareProjectedExecution(t *testing.T, cfg config.AppConfig, handoff parentHandoffOutput, slot string) (executionTransportStage, string) {
	t.Helper()
	code, output, diagnostics := runParentActionCLIInProcess(t, cfg, "prepare", controllerExecutionSurface)
	if code != 0 {
		t.Fatalf("parent prepare rejected canonical handoff: %s %s", output, diagnostics)
	}
	var prepared executionTransportStage
	if err := json.Unmarshal([]byte(output), &prepared); err != nil {
		t.Fatal(err)
	}
	command := handoff.RequiredActionParameters["controller_command_json"]
	template, err := os.ReadFile(prepared.Path)
	if err != nil {
		t.Fatal(err)
	}
	if len(prepared.NextCommand) < 3 || !strings.Contains(string(template), "COMMAND_TEMPLATE: "+command+"\n") {
		t.Fatalf("prepared command differs from real handoff: %s", template)
	}
	if slot == "" && len(prepared.Slots) != 0 || slot != "" && (len(prepared.Slots) != 1 || prepared.Slots[0].Name != slot) {
		t.Fatalf("machine semantic slots differ: %#v", prepared.Slots)
	}
	return prepared, string(template)
}

func assertStagingRejectedWithoutStateChange(t *testing.T, cfg config.AppConfig, store *controller.Store, stage executionTransportStage) {
	t.Helper()
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	code, output, diagnostics := runParentActionCLIInProcess(t, cfg, stage.NextCommand[1:]...)
	if code == 0 {
		t.Fatalf("invalid execution staging was consumed: %s %s", output, diagnostics)
	}
	after, err := store.LoadHead()
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("rejected execution staging mutated controller head: %v", err)
	}
}

func exerciseProjectedExecutionTransport(t *testing.T, cfg config.AppConfig, store *controller.Store, handoff parentHandoffOutput, slot, reason string) {
	t.Helper()
	stage, original := prepareProjectedExecution(t, cfg, handoff, slot)
	staleStage, staleBody := prepareProjectedExecution(t, cfg, handoff, slot)
	if err := os.WriteFile(staleStage.Path, []byte(staleBody), 0600); err != nil {
		t.Fatal(err)
	}

	assertStagedMutationRejected(t, cfg, store, stage, "UNDECLARED_REASON: unknown\n"+original)

	var headCommand workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &headCommand); err != nil {
		t.Fatal(err)
	}
	oldGeneration := fmt.Sprintf("\"expected_generation\":%d", headCommand.ExpectedGeneration)
	newGeneration := fmt.Sprintf("\"expected_generation\":%d", headCommand.ExpectedGeneration+1)
	tampered := strings.Replace(original, oldGeneration, newGeneration, 1)
	if tampered == original {
		t.Fatal("canonical generation is absent from stage")
	}
	assertStagedMutationRejected(t, cfg, store, stage, tampered)

	filled := original
	if slot != "" {
		if reason == "" {
			t.Fatal("machine-declared reason cannot be implicitly chosen")
		}
		filled = strings.Replace(filled, stage.Slots[0].Placeholder, reason, 1)
	}
	if err := os.WriteFile(stage.Path, []byte(filled), 0600); err != nil {
		t.Fatal(err)
	}
	code, output, diagnostics := runParentActionCLIInProcess(t, cfg, stage.NextCommand[1:]...)
	if code != 0 {
		t.Fatalf("canonical execution did not consume: %s %s", output, diagnostics)
	}
	assertStagingRejectedWithoutStateChange(t, cfg, store, staleStage)
	assertStagingRejectedWithoutStateChange(t, cfg, store, stage)
}

func assertStagedMutationRejected(t *testing.T, cfg config.AppConfig, store *controller.Store, stage executionTransportStage, payload string) {
	t.Helper()
	if err := os.WriteFile(stage.Path, []byte(payload), 0600); err != nil {
		t.Fatal(err)
	}
	assertStagingRejectedWithoutStateChange(t, cfg, store, stage)
}
