package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/packet"
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
		CandidateID:        candidate.CandidateID,
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

func TestProjectedCleanupUsesParentActionStaging(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)
	reentered, err := store.ReenterAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration,
		CandidateID:        candidate.CandidateID,
	})
	if err != nil {
		t.Fatal(err)
	}
	previousSuspension := reentered.Suspension.SnapshotID
	lane, err := store.MaterializeExecution(controller.MaterializeExecutionInput{
		ExpectedGeneration: reentered.Head.ControllerGeneration,
		SuspensionID:       reentered.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	source := *lane.Admission
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          source.Attempt.AttemptID,
		TaskPath:           source.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	evidence := canonicalWorkflowEvidence(t, cfg, store, source, candidate.Policy)
	accepted, err = store.AcceptExecutionCandidate(source, controller.CandidateAcceptanceInput{
		Message: "candidate in execution lane", Policy: candidate.Policy,
		RequiredValidations: declaredNoValidations(), Evidence: evidence,
	})
	if err != nil {
		t.Fatal(err)
	}
	reentered, err = store.ReenterAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration,
		CandidateID:        accepted.CandidateRef.LogicalIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	handoff := buildParentHandoffWithConfig(cfg, runtime)
	command := projectedTransportCommand(t, handoff, "cleanup", "")
	if command.ExpectedGeneration != reentered.Head.ControllerGeneration || command.WorkspaceID != source.Workspace.ID || command.SealRef == nil {
		t.Fatalf("cleanup handoff lost the sealed workspace: %#v", command)
	}
	if _, err := store.PreviewSuspensionGC(previousSuspension); err != nil {
		t.Fatalf("GC fixture must have a durably superseded suspension: %v", err)
	}
	if _, err := projectRequestedExecutionOperation(cfg, handoff, []string{"--operation", "gc-suspension", previousSuspension}); err == nil || !strings.Contains(err.Error(), "cannot replace") {
		t.Fatalf("GC selection bypassed the required cleanup: %v", err)
	}
	exerciseProjectedExecutionTransport(t, cfg, store, handoff, "", "")
	if _, err := os.Lstat(source.Workspace.Root); !os.IsNotExist(err) {
		t.Fatalf("sealed lane was not removed: %v", err)
	}
	next := projectedTransportCommand(t, buildParentHandoffWithConfig(cfg, runtime), "materialize", "")
	if next.SuspensionID != reentered.Suspension.SnapshotID {
		t.Fatalf("cleanup lost its successor suspension: %#v", next)
	}
}

func TestRequestedSuspensionUsesParentActionStaging(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, rootTask, blockerTask, _ := newCanonicalWorkflowCorpus(t)
	rootRunner := runCanonicalWorkflow(t, cfg, ModeNewTask, "suspension fixture", rootTask, "source.txt")
	cfg = rootRunner.cfg
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	source := canonicalWorkflowAdmission(t, cfg, store)
	project, err := store.LoadProjectSnapshot(source.Head.ProjectSnapshotID)
	if err != nil {
		t.Fatal(err)
	}
	blocker, ok := taskRefPresent(project.Tasks, blockerTask)
	if !ok {
		t.Fatal("blocker is missing from project authority")
	}
	loadConfig := func() (config.AppConfig, error) { return cfg, nil }
	observed := runControllerSemanticTestCommand(t, loadConfig, controllerSemanticCommand{
		Action: controllerSemanticObserveLive, Observation: &controller.FindingObservationInput{
			Producer: "fixture reviewer", ProofClass: controller.FindingProofUnverified, ProblemKey: "staged-suspension",
		},
	})
	resolution := controllerSemanticCommand{
		Action: controllerSemanticResolve, FindingID: observed.Finding.FindingID, Decision: &controller.FindingDecision{
			Kind: controller.FindingDecisionIndependentBlocking, TargetTaskRef: &blocker, BlockingBoundary: "fixture needs its blocker",
		},
	}
	resolved := runControllerSemanticTestCommand(t, loadConfig, resolution)
	episode := resolved.Result.Episode
	if resolved.NextExecution == nil {
		t.Fatal("committed blocker did not expose a canonical suspension command")
	}
	options := []string{"--operation", "suspend", episode.EpisodeID, strconv.FormatUint(episode.Revision, 10)}
	handoff, prepare := requestedExecutionHandoff(t, cfg, options)
	if !reflect.DeepEqual(resolved.NextExecution.PrepareCommand[1:], prepare) || !reflect.DeepEqual(resolved.NextExecution.Parameters, handoff.RequiredActionParameters) {
		t.Fatal("prepare command was reconstructed instead of following the blocker projection")
	}
	command := projectedTransportCommand(t, handoff, "suspend", "")
	if command.ExpectedGeneration != source.Head.ControllerGeneration || command.EpisodeID != episode.EpisodeID || command.EpisodeRevision != episode.Revision {
		t.Fatalf("suspension projection lost exact episode source: %#v", command)
	}
	exerciseProjectedExecutionTransport(t, cfg, store, handoff, "", "", prepare...)
	after, err := store.LoadHead()
	if err != nil || after.LiveLeaseID != "" || after.ActiveEpisodeID != episode.EpisodeID {
		t.Fatalf("staged suspension did not retain the episode and release the lease: %v", err)
	}
	repeated := runControllerSemanticTestCommand(t, loadConfig, resolution)
	blocked := repeated.NextExecution
	if blocked == nil || blocked.Consistent || blocked.Inconsistency == "" || len(blocked.PrepareCommand) != 0 {
		t.Fatalf("a committed resolution lost its blocked projection reason: %#v", blocked)
	}
	report, err := store.ProjectStatus()
	if err != nil || len(report.Suspensions) != 1 || report.Suspensions[0].AttemptID != source.Attempt.AttemptID {
		t.Fatalf("staged suspension did not retain the admitted attempt: %v", err)
	}
	code, _, _ := runParentActionCLIInProcess(t, cfg, "prepare", controllerExecutionSurface, "--operation", "gc-suspension", report.Suspensions[0].SnapshotID)
	if code == 0 {
		t.Fatal("GC was prepared while the root suspension is still required by its episode")
	}
	unchanged, err := store.LoadHead()
	if err != nil || !reflect.DeepEqual(after, unchanged) {
		t.Fatalf("refused GC changed controller authority: %v", err)
	}
}

func TestUserTerminationProjectionRequiresCanonicalHandoffSelection(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, store, input, runtime, _ := userExecutionTerminationFixture(t)

	handoff := buildParentHandoffWithConfig(cfg, runtime)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerPublicationSurface {
		t.Fatalf("pending external advancement must remain the selected handoff: %#v", handoff)
	}
	var publication controllerPublicationCommand
	if err := json.Unmarshal([]byte(handoff.RequiredActionParameters["controller_command_json"]), &publication); err != nil ||
		publication.Action != controller.PublicationOperationAdoptMutable {
		t.Fatalf("canonical upstream operation was not selected: %#v %v", publication, err)
	}
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	selection := []string{"--operation", "user-terminate-execution"}
	code, output, diagnostics := runParentActionCLIInProcess(t, cfg, append([]string{"prepare", controllerExecutionSurface}, selection...)...)
	if code == 0 {
		t.Fatalf("termination was prepared before adopt-mutable: %s %s", output, diagnostics)
	}
	after, err := store.LoadHead()
	if err != nil || !reflect.DeepEqual(before, after) || runtime.TaskStatus() != state.TaskStatusAwaitingParentCompletion {
		t.Fatalf("refused termination changed source authority: %#v %v", after, err)
	}
	executeStagedParentAction(t, cfg, handoff.RequiredActionParameters["controller_command_json"])
	handoff = buildParentHandoffWithConfig(cfg, runtime)
	projectedTransportCommand(t, handoff, "cleanup", "")
	exerciseProjectedExecutionTransport(t, cfg, store, handoff, "", "")
	handoff = buildParentHandoffWithConfig(cfg, runtime)
	projectedTransportCommand(t, handoff, "materialize", "")
	exerciseProjectedExecutionTransport(t, cfg, store, handoff, "", "")
	if _, err := store.PreviewExecutionTermination(); err == nil {
		t.Fatal("execution termination was offered before reentry review acceptance")
	}
	reentered, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          reentered.LiveAttemptID,
		TaskPath:           reentered.ExecutionTaskRef.TaskPath,
		TaskContractDigest: reentered.ExecutionTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SetTaskStatus(state.TaskStatusComplete); err != nil {
		t.Fatal(err)
	}
	if err := runtime.RecordSolResult(packet.Result{Status: packet.StatusPass, Risk: packet.RiskHigh}, state.ParentReviewProducer{}); err != nil {
		t.Fatal(err)
	}
	if accepted, err := runtime.AcceptParentReview(); err != nil || !accepted {
		t.Fatalf("fixture reentry review accept: %v %v", accepted, err)
	}
	if _, err := store.PreviewExecutionTermination(); err != nil {
		t.Fatalf("reentered source is ineligible for user termination: %v", err)
	}
	handoff = buildParentHandoffWithConfig(cfg, runtime)
	if handoff.RequiredAction == nil || !terminationFollowsCompletion(*handoff.RequiredAction, handoff.RequiredActionParameters["controller_command_json"]) {
		t.Fatalf("termination preview replaced the ordinary completion handoff: %s", stringPointerValue(handoff.RequiredAction))
	}
	handoff, prepare := requestedExecutionHandoff(t, cfg, selection)
	command := projectedTransportCommand(t, handoff, "user-terminate-execution", "execution_termination.reason")
	if command.ExecutionTermination == nil || command.ExecutionTermination.SourceAttemptID != reentered.LiveAttemptID || command.ExecutionTermination.Reason != "" {
		t.Fatalf("termination projection lost the current source or invented the reason: %#v", command)
	}
	exerciseProjectedExecutionTransport(t, cfg, store, handoff, "execution_termination.reason", input.Reason, prepare...)
	after, err = store.LoadHead()
	if err != nil || after.UserTerminationRef == nil || after.LiveLeaseID != "" {
		t.Fatalf("staged termination did not retire the live lease: %v", err)
	}
	record, err := store.LoadUserTaskTermination(*after.UserTerminationRef)
	if err != nil || record.Reason != input.Reason || record.Execution == nil || record.Execution.Attempt.AttemptID != reentered.LiveAttemptID {
		t.Fatalf("staged termination did not preserve the supplied reason and live source: %v", err)
	}
}

func TestRequestedSuspensionGCUsesParentActionStaging(t *testing.T) {
	binary := typedCompletionWorkerBinary(t)
	t.Setenv("PATH", filepath.Dir(binary)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cfg, source, store, policy := retiredWorkflowCorpus(t, true)
	accept := func(source controller.Admission) controller.ExecutionOperationResult {
		t.Helper()
		result, err := store.AcceptExecutionCandidate(source, controller.CandidateAcceptanceInput{
			Message: "verified candidate for GC", Policy: policy, RequiredValidations: declaredNoValidations(),
			Evidence: canonicalWorkflowEvidence(t, cfg, store, source, policy),
		})
		if err != nil {
			t.Fatal(err)
		}
		return result
	}
	first := accept(source)
	reentered, err := store.ReenterAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: first.Head.ControllerGeneration, CandidateID: first.CandidateRef.LogicalIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	lane, err := store.MaterializeExecution(controller.MaterializeExecutionInput{
		ExpectedGeneration: reentered.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID,
	})
	if err != nil {
		t.Fatal(err)
	}
	successor := *lane.Admission
	accepted := accept(successor)
	completeAwaitedRuntimeThroughProductionAPIs(t, cfg, successor)
	promoted, err := store.PromoteAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: accepted.CandidateRef.LogicalIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	published, err := store.PublishAcceptedCandidate(controller.PublicationInput{
		ExpectedGeneration: promoted.Head.ControllerGeneration, CandidateID: accepted.CandidateRef.LogicalIdentity,
	})
	if err != nil {
		t.Fatal(err)
	}
	candidate, err := store.LoadAcceptedCandidate(*published.CandidateRef)
	if err != nil {
		t.Fatal(err)
	}
	after, err := store.CleanupExecution(controller.CleanupExecutionInput{
		ExpectedGeneration: published.Head.ControllerGeneration, WorkspaceID: successor.Workspace.ID, SealRef: candidate.SealRef,
	})
	if err != nil {
		t.Fatal(err)
	}
	suspensionID := reentered.Suspension.SnapshotID
	options := []string{"--operation", "gc-suspension", suspensionID}
	handoff, prepare := requestedExecutionHandoff(t, cfg, options)
	gc := projectedTransportCommand(t, handoff, "gc-suspension", "")
	if gc.SuspensionID != suspensionID || gc.ExpectedGeneration != after.Head.ControllerGeneration {
		t.Fatalf("GC projection lost exact suspension authority: %#v", gc)
	}
	exerciseProjectedExecutionTransport(t, cfg, store, handoff, "", "", prepare...)
	if _, err := store.LoadSuspension(cfg.RepoRoot, gc.SuspensionID); !os.IsNotExist(err) {
		t.Fatalf("collected suspension still exists: %v", err)
	}
}

func requestedExecutionHandoff(t *testing.T, cfg config.AppConfig, options []string) (parentHandoffOutput, []string) {
	t.Helper()
	args := append([]string{"--authority", "controller-execution-plan"}, options...)
	var output bytes.Buffer
	handled, err := runControllerExecutionProjection(args, func() (config.AppConfig, error) { return cfg, nil }, &output)
	if err != nil || !handled {
		t.Fatalf("explicit execution handoff was not projected: %v", err)
	}
	var projection controllerExecutionProjection
	if err := json.Unmarshal(output.Bytes(), &projection); err != nil {
		t.Fatal(err)
	}
	if want := append([]string{"glm-parent-action", "prepare", controllerExecutionSurface}, options...); !reflect.DeepEqual(projection.PrepareCommand, want) {
		t.Fatalf("projected preparation command = %v, want %v", projection.PrepareCommand, want)
	}
	return parentHandoffOutput{Consistent: projection.Consistent, RequiredAction: projection.RequiredAction, RequiredActionParameters: projection.Parameters}, projection.PrepareCommand[1:]
}

func projectedTransportCommand(t *testing.T, handoff parentHandoffOutput, action, slot string) workflow.ControllerExecutionCommand {
	t.Helper()
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
		t.Fatalf("canonical execution handoff is not admitted: required=%s allowed=%v cause=%s", stringPointerValue(handoff.RequiredAction), handoff.AllowedActions, stringPointerValue(handoff.Inconsistency))
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

func prepareProjectedExecution(t *testing.T, cfg config.AppConfig, handoff parentHandoffOutput, slot string, args ...string) (executionTransportStage, string) {
	t.Helper()
	if len(args) == 0 {
		args = []string{"prepare", controllerExecutionSurface}
	}
	code, output, diagnostics := runParentActionCLIInProcess(t, cfg, args...)
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

func exerciseProjectedExecutionTransport(t *testing.T, cfg config.AppConfig, store *controller.Store, handoff parentHandoffOutput, slot, reason string, prepare ...string) {
	t.Helper()
	stage, original := prepareProjectedExecution(t, cfg, handoff, slot, prepare...)
	staleStage, staleBody := prepareProjectedExecution(t, cfg, handoff, slot, prepare...)
	if len(prepare) > 0 {
		unselected := stage
		unselected.NextCommand = stage.NextCommand[:3]
		assertStagingRejectedWithoutStateChange(t, cfg, store, unselected)
	}
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
