package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controller"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionmilestone"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/executionunit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/runner"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type taskAuthoritySyncFixture struct {
	cfg     config.AppConfig
	store   *controller.Store
	source  controller.Admission
	runtime *state.StateStore
}

func taskAuthoritySyncDigest(t *testing.T, data []byte) string {
	t.Helper()
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func (f taskAuthoritySyncFixture) reviseActiveTask(t *testing.T, amendment string) controller.SemanticTaskRef {
	t.Helper()
	taskPath := f.source.Attempt.SemanticTaskRef.TaskPath
	task, err := os.ReadFile(filepath.Join(f.cfg.RepoRoot, taskPath))
	if err != nil {
		t.Fatal(err)
	}
	revised := append(append([]byte(nil), task...), []byte(amendment)...)
	writeAppTestFile(t, f.cfg.RepoRoot, taskPath, string(revised))
	return controller.SemanticTaskRef{TaskPath: taskPath, ContractDigest: taskAuthoritySyncDigest(t, revised)}
}

func taskAuthoritySyncLaneFixture(t *testing.T) taskAuthoritySyncFixture {
	t.Helper()
	cfg, store, accepted, candidate, runtime := sealedCandidateReentryFixture(t)
	reentered, err := store.ReenterAcceptedCandidate(controller.PublicationInput{ExpectedGeneration: accepted.Head.ControllerGeneration, CandidateID: candidate.CandidateID})
	if err != nil {
		t.Fatal(err)
	}
	lane, err := workflow.ExecuteControllerExecution(cfg, store, workflow.ControllerExecutionCommand{Action: "materialize", ExpectedGeneration: reentered.Head.ControllerGeneration, SuspensionID: reentered.Suspension.SnapshotID})
	if err != nil {
		t.Fatal(err)
	}
	source := *lane.Admission
	bound := cfg
	bound.RepoRoot = source.Workspace.Root
	if err := state.CaptureGitBaseline(bound, runtime); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          source.Attempt.AttemptID,
		TaskPath:           source.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: source.Attempt.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("repo-root", source.Workspace.Root); err != nil {
		t.Fatal(err)
	}
	if err := runtime.Write("last-request", "task authority sync original request"); err != nil {
		t.Fatal(err)
	}
	if err := runtime.SaveCurrentTaskAuthority(source.Attempt.SemanticTaskRef.TaskPath, []byte("# active\n")); err != nil {
		t.Fatal(err)
	}
	writeAppTestFile(t, source.Workspace.Root, "lane-implementation.txt", "uncommitted worker implementation\n")
	after, err := controller.CaptureWorkspaceSnapshot(source.Workspace.Root)
	if err != nil {
		t.Fatal(err)
	}
	source, err = store.RecordAdmittedMutation(source, "task authority sync fixture work", "success", after)
	if err != nil {
		t.Fatal(err)
	}
	taskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	definitions := []executionunit.MilestoneDefinition{
		{ID: "m1", Scope: "first scope", Acceptance: "first acceptance"},
		{ID: "m2", Scope: "second scope", Acceptance: "second acceptance"},
	}
	plan := executionmilestone.NewPlan(taskID, source.Attempt.SemanticTaskRef.TaskPath, source.Attempt.SemanticTaskRef.ContractDigest, definitions, time.Now().UTC())
	plan.CurrentIndex = 1
	plan.Milestones[0].Status = executionmilestone.StatusComplete
	plan.Milestones[0].Completion = &executionmilestone.Completion{
		CompletedAt:        time.Now().UTC(),
		Summary:            "first milestone",
		TaskContractSHA256: source.Attempt.SemanticTaskRef.ContractDigest,
	}
	if err := executionmilestone.Save(runtime, plan); err != nil {
		t.Fatal(err)
	}
	return taskAuthoritySyncFixture{cfg: cfg, store: store, source: source, runtime: runtime}
}

func taskAuthoritySyncHandoff(t *testing.T, f taskAuthoritySyncFixture) parentHandoffOutput {
	t.Helper()
	return buildParentHandoffWithConfig(f.cfg, f.runtime)
}

func TestTaskAuthoritySyncHandoffGuidesAndRevisesLiveLaneContract(t *testing.T) {
	f := taskAuthoritySyncLaneFixture(t)
	revised := f.reviseActiveTask(t, "\n## Amendments\n\n親Codexの追加要求。\n")
	headBefore, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}

	handoff := taskAuthoritySyncHandoff(t, f)
	if !handoff.Consistent || handoff.RequiredAction == nil || *handoff.RequiredAction != controllerExecutionSurface {
		t.Fatalf("revised Task handoff did not demand the sync operation: %#v", handoff)
	}
	if containsParentAction(handoff.AllowedActions, "start") {
		t.Fatalf("revised Task handoff admitted start before the sync: %v", handoff.AllowedActions)
	}
	payload := handoff.RequiredActionParameters["controller_command_json"]
	var command workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != "sync-task-authority" || command.ExpectedGeneration != headBefore.ControllerGeneration ||
		command.SourceSnapshotID == "" || command.RevisedTask == nil || !command.RevisedTask.Equal(revised) {
		t.Fatalf("sync guidance command: %#v", command)
	}
	if handoff.RequiredActionParameters["previous_task_contract_sha256"] != f.source.Attempt.SemanticTaskRef.ContractDigest ||
		handoff.RequiredActionParameters["revised_task_contract_sha256"] != revised.ContractDigest {
		t.Fatalf("sync guidance lost its digest boundary: %#v", handoff.RequiredActionParameters)
	}

	primaryHead, err := controller.CaptureWorkspaceSnapshot(f.cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if primaryHead.ID != command.SourceSnapshotID {
		t.Fatal("sync guidance did not observe the current primary snapshot")
	}
	executeStagedControllerAction(t, f.cfg, controllerExecutionSurface, payload, true)

	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if head.RootTaskRef == nil || !head.RootTaskRef.Equal(revised) || head.ExecutionTaskRef == nil || !head.ExecutionTaskRef.Equal(revised) {
		t.Fatalf("sync did not rebind the revised Task authority: %#v", head)
	}
	if head.LiveAttemptID != f.source.Attempt.AttemptID || head.LiveLeaseID == f.source.Lease.LeaseID || head.ActiveEpisodeID != "" || head.AcceptedCandidateRef != nil {
		t.Fatalf("sync disturbed live execution boundaries: %#v", head)
	}
	laneTask, err := os.ReadFile(filepath.Join(f.source.Workspace.Root, revised.TaskPath))
	if err != nil || !bytes.Contains(laneTask, []byte("親Codexの追加要求。")) {
		t.Fatalf("revised contract was not applied to the lane: %v", err)
	}
	implementation, err := os.ReadFile(filepath.Join(f.source.Workspace.Root, "lane-implementation.txt"))
	if err != nil || string(implementation) != "uncommitted worker implementation\n" {
		t.Fatalf("sync lost the lane implementation diff: %v", err)
	}
	primaryTask, err := os.ReadFile(filepath.Join(f.cfg.RepoRoot, revised.TaskPath))
	if err != nil || !bytes.Equal(primaryTask, laneTask) {
		t.Fatal("sync diverged the primary revision from the lane contract")
	}
	afterPrimary, err := controller.CaptureWorkspaceSnapshot(f.cfg.RepoRoot)
	if err != nil || afterPrimary.Head != primaryHead.Head || afterPrimary.IndexDigest != primaryHead.IndexDigest || afterPrimary.RefDigest != primaryHead.RefDigest {
		t.Fatal("sync mutated protected primary Git authority")
	}
	binding, err := f.runtime.LoadControllerRuntimeBinding()
	if err != nil || binding.AttemptID != f.source.Attempt.AttemptID || binding.TaskContractDigest != revised.ContractDigest {
		t.Fatalf("sync did not rebind the runtime contract: %#v %v", binding, err)
	}
	plan, err := executionmilestone.Load(f.runtime)
	if err != nil || plan == nil {
		t.Fatalf("sync lost the milestone plan: %#v %v", plan, err)
	}
	if plan.TaskContractSHA256 != revised.ContractDigest || plan.CurrentIndex != 1 ||
		plan.Milestones[0].Status != executionmilestone.StatusComplete ||
		plan.Milestones[0].Completion == nil || plan.Milestones[0].Completion.TaskContractSHA256 != f.source.Attempt.SemanticTaskRef.ContractDigest {
		t.Fatalf("sync did not rebind the milestone authority with history: %#v", plan)
	}
	stored, err := os.ReadFile(f.runtime.TaskAuthorityContentPath(mustTaskAuthorityID(t, f.runtime)))
	if err != nil || !bytes.Equal(stored, laneTask) {
		t.Fatalf("sync did not refresh the task authority snapshot: %v", err)
	}

	after := taskAuthoritySyncHandoff(t, f)
	if after.RequiredAction != nil && *after.RequiredAction == controllerExecutionSurface &&
		strings.Contains(after.RequiredActionParameters["controller_command_json"], "sync-task-authority") {
		t.Fatalf("sync completed but the handoff still demands another sync: %#v", after)
	}
}

func mustTaskAuthorityID(t *testing.T, runtime *state.StateStore) string {
	t.Helper()
	taskID, err := runtime.TaskID()
	if err != nil {
		t.Fatal(err)
	}
	return taskID
}

func TestTaskAuthoritySyncRejectsUnexplainedOrStaleRevisions(t *testing.T) {
	f := taskAuthoritySyncLaneFixture(t)
	revised := f.reviseActiveTask(t, "\n## Amendments\n\n却下される追加分。\n")
	headBefore, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	assertSyncRejected := func(command workflow.ControllerExecutionCommand) {
		t.Helper()
		before, err := f.store.LoadHead()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := workflow.ExecuteControllerExecution(f.cfg, f.store, command); err == nil {
			t.Fatalf("unexplained revision was synced: %#v", command)
		}
		after, err := f.store.LoadHead()
		if err != nil || after.ControllerGeneration != before.ControllerGeneration || after.PendingTransitionID != "" {
			t.Fatalf("rejected sync disturbed controller authority: %#v", after)
		}
	}

	stale := workflow.ControllerExecutionCommand{Action: "sync-task-authority", ExpectedGeneration: headBefore.ControllerGeneration, SourceSnapshotID: "stale-parent-snapshot", RevisedTask: &revised}
	assertSyncRejected(stale)
	foreign := revised
	foreign.TaskPath = "IMPLEMENTATION_TASKS/successor.md"
	assertSyncRejected(workflow.ControllerExecutionCommand{Action: "sync-task-authority", ExpectedGeneration: headBefore.ControllerGeneration, SourceSnapshotID: currentPrimarySnapshotID(t, f), RevisedTask: &foreign})
	unchanged := f.source.Attempt.SemanticTaskRef
	assertSyncRejected(workflow.ControllerExecutionCommand{Action: "sync-task-authority", ExpectedGeneration: headBefore.ControllerGeneration, SourceSnapshotID: currentPrimarySnapshotID(t, f), RevisedTask: &unchanged})
	assertSyncRejected(workflow.ControllerExecutionCommand{Action: "sync-task-authority", ExpectedGeneration: headBefore.ControllerGeneration + 1, SourceSnapshotID: currentPrimarySnapshotID(t, f), RevisedTask: &revised})

	writeAppTestFile(t, f.cfg.RepoRoot, "parent-extra.txt", "another parent edit\n")
	snapshot := currentPrimarySnapshotID(t, f)
	_, err = workflow.ExecuteControllerExecution(f.cfg, f.store, workflow.ControllerExecutionCommand{Action: "sync-task-authority", ExpectedGeneration: headBefore.ControllerGeneration, SourceSnapshotID: snapshot, RevisedTask: &revised})
	if err == nil || !strings.Contains(err.Error(), "active Task file") {
		t.Fatalf("extra primary change was accepted: %v", err)
	}
	handoff := taskAuthoritySyncHandoff(t, f)
	if handoff.RequiredAction != nil && *handoff.RequiredAction == controllerExecutionSurface &&
		strings.Contains(handoff.RequiredActionParameters["controller_command_json"], "sync-task-authority") {
		t.Fatalf("handoff guided a sync outside its Task-only boundary: %#v", handoff)
	}
	if handoff.Consistent || handoff.Inconsistency == nil || !strings.Contains(*handoff.Inconsistency, "not limited") {
		t.Fatalf("handoff did not keep source import closed for unpreserved Lane changes: %#v", handoff)
	}
	if _, _, err := f.store.PreviewParentSourceImport(f.source.Workspace.Root); err == nil {
		t.Fatal("full source import would discard the Lane implementation")
	}
}

func TestTaskAuthoritySyncRequiresStoppedRuntimeAndLiveAttemptBinding(t *testing.T) {
	f := taskAuthoritySyncLaneFixture(t)
	revised := f.reviseActiveTask(t, "\n## Amendments\n\nruntime境界の追加分。\n")
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	command := workflow.ControllerExecutionCommand{Action: "sync-task-authority", ExpectedGeneration: head.ControllerGeneration, SourceSnapshotID: currentPrimarySnapshotID(t, f), RevisedTask: &revised}
	if err := f.runtime.SetTaskStatus(state.TaskStatusActive); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ExecuteControllerExecution(f.cfg, f.store, command); err == nil || !strings.Contains(err.Error(), "stopped runtime") {
		t.Fatalf("active runtime accepted a Task sync: %v", err)
	}
	if err := f.runtime.SetTaskStatus(state.TaskStatusWaitingSolReview); err != nil {
		t.Fatal(err)
	}
	foreignRuntime := f.runtime
	if err := foreignRuntime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          f.source.Attempt.AttemptID,
		TaskPath:           "IMPLEMENTATION_TASKS/successor.md",
		TaskContractDigest: f.source.Attempt.SemanticTaskRef.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ExecuteControllerExecution(f.cfg, f.store, command); err == nil || !strings.Contains(err.Error(), "source binding") {
		t.Fatalf("foreign runtime binding accepted a Task sync: %v", err)
	}
	if err := f.runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID:          f.source.Attempt.AttemptID,
		TaskPath:           f.source.Attempt.SemanticTaskRef.TaskPath,
		TaskContractDigest: "foreign-digest",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ExecuteControllerExecution(f.cfg, f.store, command); err == nil || !strings.Contains(err.Error(), "source binding") {
		t.Fatalf("foreign runtime contract digest accepted a Task sync: %v", err)
	}
	if err := f.runtime.ClearControllerRuntimeBinding(); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ExecuteControllerExecution(f.cfg, f.store, command); err == nil || !strings.Contains(err.Error(), "intact runtime binding") {
		t.Fatalf("corrupted runtime state was treated as unstarted: %v", err)
	}
}

func currentPrimarySnapshotID(t *testing.T, f taskAuthoritySyncFixture) string {
	t.Helper()
	snapshot, err := controller.CaptureWorkspaceSnapshot(f.cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot.ID
}

func runTaskAuthoritySyncLaneDispatch(t *testing.T, laneCfg config.AppConfig, mode CommandMode, payload string) (*canonicalWorkflowRunner, error) {
	t.Helper()
	scripted := &canonicalWorkflowRunner{t: t, task: "IMPLEMENTATION_TASKS/root.md"}
	scripted.cfg = laneCfg
	scripted.cfg.ClaudeConfigDir = t.TempDir()
	wf := workflow.NewWorkflow(laneCfg, state.AttachStateStore(laneCfg), scripted, io.Discard)
	var err error
	switch mode {
	case ModeFix:
		err = wf.ExecuteExplicitFixWithExecutionMilestones(payload, "", "", "")
	case ModeResume:
		err = wf.ExecuteResumeWithExecutionMilestones()
	default:
		t.Fatalf("unsupported lane dispatch mode: %d", mode)
	}
	return scripted, err
}

func syncRevisedTaskThroughRegularOwner(t *testing.T, f taskAuthoritySyncFixture, revised controller.SemanticTaskRef) controller.ExecutionOperationResult {
	t.Helper()
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	result, err := workflow.ExecuteControllerExecution(f.cfg, f.store, workflow.ControllerExecutionCommand{
		Action: "sync-task-authority", ExpectedGeneration: head.ControllerGeneration,
		SourceSnapshotID: currentPrimarySnapshotID(t, f), RevisedTask: &revised,
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func TestTaskAuthoritySyncGuardBlocksDispatchUntilRevisedContract(t *testing.T) {
	f := taskAuthoritySyncLaneFixture(t)
	laneCfg := f.cfg
	laneCfg.RepoRoot = f.source.Workspace.Root
	revised := f.reviseActiveTask(t, "\n## Amendments\n\ndispatch前の追加分。\n")

	scripted, err := runTaskAuthoritySyncLaneDispatch(t, laneCfg, ModeFix, "fix round request")
	if err == nil || !strings.Contains(err.Error(), "model call blocked until the revised Task contract is ingested") {
		t.Fatalf("stale contract fix dispatch was not blocked with sync guidance: %v", err)
	}
	if len(scripted.prompts) != 0 {
		t.Fatalf("stale contract dispatch reached the provider: %d calls", len(scripted.prompts))
	}

	syncRevisedTaskThroughRegularOwner(t, f, revised)
	scripted, err = runTaskAuthoritySyncLaneDispatch(t, laneCfg, ModeFix, "fix round request")
	var interrupted *runner.InterruptedCallError
	if !errors.As(err, &interrupted) || len(scripted.prompts) != 1 {
		t.Fatalf("synced contract dispatch did not reach the provider exactly once: %v calls=%d", err, len(scripted.prompts))
	}

	second := f.reviseActiveTask(t, "\n## Amendments\n\n同一commit内の2回目の追加分。\n")
	head, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if required, err := f.store.RequiredTaskAuthoritySync(head); err != nil || required == nil || !required.Admittable {
		t.Fatalf("second parent revision was not classified as sync-required: %#v %v", required, err)
	}

	_, err = runTaskAuthoritySyncLaneDispatch(t, laneCfg, ModeResume, "")
	if err == nil || !strings.Contains(err.Error(), "model call blocked until the revised Task contract is ingested") {
		t.Fatalf("second stale contract resume was not blocked: %v", err)
	}
	if len(scripted.prompts) != 1 {
		t.Fatalf("second stale contract dispatch reached the provider: %d calls", len(scripted.prompts))
	}

	syncRevisedTaskThroughRegularOwner(t, f, second)
	head, err = f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if required, err := f.store.RequiredTaskAuthoritySync(head); err != nil || required != nil {
		t.Fatalf("synced lane still reports a required sync: %#v err=%v", required, err)
	}
	implementation, err := os.ReadFile(filepath.Join(f.source.Workspace.Root, "lane-implementation.txt"))
	if err != nil || string(implementation) != "uncommitted worker implementation\n" {
		t.Fatalf("second sync lost the lane implementation diff: %q %v", implementation, err)
	}
	laneTask, err := os.ReadFile(filepath.Join(f.source.Workspace.Root, second.TaskPath))
	if err != nil || !bytes.Contains(laneTask, []byte("同一commit内の2回目の追加分。")) {
		t.Fatalf("second revision was not applied to the lane: %v", err)
	}
	binding, err := f.runtime.LoadControllerRuntimeBinding()
	if err != nil || binding.TaskContractDigest != second.ContractDigest {
		t.Fatalf("second sync did not rebind the runtime contract: %#v %v", binding, err)
	}
	plan, err := executionmilestone.Load(f.runtime)
	if err != nil || plan == nil || plan.TaskContractSHA256 != second.ContractDigest ||
		plan.Milestones[0].Completion == nil || plan.Milestones[0].Completion.TaskContractSHA256 != f.source.Attempt.SemanticTaskRef.ContractDigest {
		t.Fatalf("second sync did not rebind the milestone authority with history: %#v", plan)
	}
	handoff := taskAuthoritySyncHandoff(t, f)
	if handoff.RequiredAction != nil && *handoff.RequiredAction == controllerExecutionSurface &&
		strings.Contains(handoff.RequiredActionParameters["controller_command_json"], "sync-task-authority") {
		t.Fatalf("handoff still demands a sync after the second round: %#v", handoff)
	}

	scripted, err = runTaskAuthoritySyncLaneDispatch(t, laneCfg, ModeResume, "")
	if !errors.As(err, &interrupted) || len(scripted.prompts) != 1 {
		t.Fatalf("resume after the second sync did not reach the provider: %v calls=%d", err, len(scripted.prompts))
	}
}

func TestTaskAuthoritySyncGuardBlocksFreshStartUntilSynced(t *testing.T) {
	cfg, rootTask, _, _ := newCanonicalWorkflowCorpus(t)
	original, err := os.ReadFile(filepath.Join(cfg.RepoRoot, rootTask))
	if err != nil {
		t.Fatal(err)
	}
	revisedContent := append(append([]byte(nil), original...), []byte("\n## Amendments\n\n初回dispatch前の追加分。\n")...)
	writeAppTestFile(t, cfg.RepoRoot, rootTask, string(revisedContent))
	revisedDigest := taskAuthoritySyncDigest(t, revisedContent)

	dispatchFreshStart := func() (*canonicalWorkflowRunner, error) {
		scripted := &canonicalWorkflowRunner{t: t, task: rootTask}
		factory := func(actual config.AppConfig, _ *state.StateStore, _ *runner.StopController) workflow.ModelRunner {
			scripted.cfg = actual
			return scripted
		}
		runCfg := cfg
		runCfg.ClaudeConfigDir = t.TempDir()
		return scripted, Execute(Command{Mode: ModeNewTask, Payload: "fresh start request"}, runCfg, factory, io.Discard, io.Discard)
	}

	scripted, err := dispatchFreshStart()
	if err == nil || !strings.Contains(err.Error(), "model call blocked until the revised Task contract is ingested") {
		t.Fatalf("stale fresh start was not blocked with sync guidance: %v", err)
	}
	if len(scripted.prompts) != 0 {
		t.Fatalf("stale fresh start reached the provider: %d calls", len(scripted.prompts))
	}
	store, err := controller.Open(cfg)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if loaded.LiveAttemptID == "" || loaded.ExecutionTaskRef == nil || loaded.ExecutionTaskRef.ContractDigest == revisedDigest {
		t.Fatalf("blocked fresh start did not bootstrap the regular lane authority: %#v", loaded)
	}
	snapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ExecuteControllerExecution(cfg, store, workflow.ControllerExecutionCommand{
		Action: "sync-task-authority", ExpectedGeneration: loaded.ControllerGeneration,
		SourceSnapshotID: snapshot.ID,
		RevisedTask:      &controller.SemanticTaskRef{TaskPath: rootTask, ContractDigest: revisedDigest},
	}); err != nil {
		t.Fatal(err)
	}
	if head, err := store.LoadHead(); err != nil || head.ExecutionTaskRef.ContractDigest != revisedDigest {
		t.Fatalf("fresh lane sync did not rebind the revised contract: %#v err=%v", head, err)
	}
	handoff := buildParentHandoffWithConfig(cfg, state.AttachStateStore(cfg))
	if !handoff.Consistent || handoff.Controller == nil ||
		handoff.RequiredAction == nil || *handoff.RequiredAction != canonicalStartAction {
		t.Fatalf("unstarted runtime after Task sync has no legal start handoff: %#v", handoff)
	}
	if handoff.Controller.Head.ExecutionTaskRef == nil ||
		handoff.Controller.Head.ExecutionTaskRef.ContractDigest != revisedDigest {
		t.Fatalf("synchronized handoff lost its Task revision: %#v", handoff.Controller.Head)
	}
	scripted, err = dispatchFreshStart()
	var interrupted *runner.InterruptedCallError
	if !errors.As(err, &interrupted) || len(scripted.prompts) != 1 {
		t.Fatalf("synced fresh start did not reach the provider exactly once: %v calls=%d", err, len(scripted.prompts))
	}
}

func prepareTaskAuthorityInterruptedSync(t *testing.T) (taskAuthoritySyncFixture, controller.SemanticTaskRef, controller.ExecutionOperationResult, controller.Admission) {
	t.Helper()
	f := taskAuthoritySyncLaneFixture(t)
	revised := f.reviseActiveTask(t, "\n## Amendments\n\nrecovery境界の追加分。\n")

	runtimeCfg, err := controller.ParentContinuationConfig(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	wf := workflow.NewWorkflow(runtimeCfg, state.AttachStateStore(runtimeCfg), nil, io.Discard)
	before, err := wf.CaptureTaskAuthoritySyncEvidence()
	if err != nil {
		t.Fatal(err)
	}
	admission, err := controller.CurrentSemanticAdmission(f.cfg, f.store)
	if err != nil {
		t.Fatal(err)
	}
	ref, err := f.store.PutEvidenceObject("runtime-artifact", "application/json",
		admission.Attempt.AttemptID+":"+controller.TaskAuthoritySyncProducer+":"+currentPrimarySnapshotID(t, f), true, before)
	if err != nil {
		t.Fatal(err)
	}
	committed, err := f.store.SyncTaskAuthority(admission, currentPrimarySnapshotID(t, f), ref, revised)
	if err != nil {
		t.Fatal(err)
	}
	return f, revised, committed, admission
}

func TestTaskAuthoritySyncRecoversInterruptedRuntimeCompletion(t *testing.T) {
	f, revised, committed, admission := prepareTaskAuthorityInterruptedSync(t)

	laneCfg := f.cfg
	laneCfg.RepoRoot = f.source.Workspace.Root
	boundLane, err := controller.WorkflowConfig(laneCfg)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := executionmilestone.Load(f.runtime)
	if err != nil {
		t.Fatal(err)
	}
	if err := executionmilestone.ValidateAuthority(boundLane, f.runtime, plan); err == nil ||
		!strings.Contains(err.Error(), "task contract changed") {
		t.Fatalf("stale milestone plan was still admitted under the revised file: %v", err)
	}
	if handoff := buildParentHandoffWithConfig(laneCfg, f.runtime); handoff.Consistent {
		t.Fatalf("stale runtime binding handoff stayed consistent mid-recovery: %#v", handoff)
	}

	recovered := runControllerRecoveryCLI(t, f.cfg, committed.TransitionID)
	if recovered.TransitionID != committed.TransitionID || recovered.Head.RootTaskRef == nil || !recovered.Head.RootTaskRef.Equal(revised) {
		t.Fatalf("CLI recovery lost the revised transition authority: %#v", recovered)
	}
	binding, err := f.runtime.LoadControllerRuntimeBinding()
	if err != nil || binding.TaskContractDigest != revised.ContractDigest {
		t.Fatalf("recovery did not complete the runtime binding: %#v %v", binding, err)
	}
	plan, err = executionmilestone.Load(f.runtime)
	if err != nil || plan.TaskContractSHA256 != revised.ContractDigest ||
		plan.Milestones[0].Completion == nil || plan.Milestones[0].Completion.TaskContractSHA256 != f.source.Attempt.SemanticTaskRef.ContractDigest {
		t.Fatalf("recovery did not complete the milestone rebind with history: %#v", plan)
	}
	if err := executionmilestone.ValidateAuthority(boundLane, f.runtime, plan); err != nil {
		t.Fatalf("recovered milestone plan is not admitted: %v", err)
	}

	if _, err := workflow.ExecuteControllerExecution(f.cfg, f.store, workflow.ControllerExecutionCommand{Action: "recover", TransitionID: committed.TransitionID}); err != nil {
		t.Fatalf("completed sync recovery is not idempotent: %v", err)
	}

	foreign := revised
	foreign.ContractDigest = "foreign-recovery-digest"
	if err := f.runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID: admission.Attempt.AttemptID, TaskPath: revised.TaskPath, TaskContractDigest: foreign.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ExecuteControllerExecution(f.cfg, f.store, workflow.ControllerExecutionCommand{Action: "recover", TransitionID: committed.TransitionID}); err == nil ||
		!strings.Contains(err.Error(), "recovery lineage") {
		t.Fatalf("foreign runtime binding was recovered: %v", err)
	}
	if err := f.runtime.SaveControllerRuntimeBinding(state.ControllerRuntimeBinding{
		AttemptID: admission.Attempt.AttemptID, TaskPath: revised.TaskPath, TaskContractDigest: revised.ContractDigest,
	}); err != nil {
		t.Fatal(err)
	}
	if err := f.runtime.SaveCurrentTaskAuthority(revised.TaskPath, []byte("# stale authority\n")); err != nil {
		t.Fatal(err)
	}
	if _, err := workflow.ExecuteControllerExecution(f.cfg, f.store, workflow.ControllerExecutionCommand{Action: "recover", TransitionID: committed.TransitionID}); err != nil {
		t.Fatalf("mid-boundary crash was not recovered from the target binding: %v", err)
	}
	stored, err := os.ReadFile(f.runtime.TaskAuthorityContentPath(mustTaskAuthorityID(t, f.runtime)))
	if err != nil || !bytes.Contains(stored, []byte("recovery境界の追加分。")) {
		t.Fatalf("mid-boundary recovery did not refresh the task authority snapshot: %v", err)
	}
}

func TestTaskAuthoritySyncRejectsMissingIdentityWithRetainedRuntime(t *testing.T) {
	f := taskAuthoritySyncLaneFixture(t)
	revised := f.reviseActiveTask(t, "\n追加要求。\n")
	before, err := f.store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if err := f.runtime.Remove("task.id"); err != nil {
		t.Fatal(err)
	}
	_, err = workflow.ExecuteControllerExecution(f.cfg, f.store, workflow.ControllerExecutionCommand{Action: "sync-task-authority", ExpectedGeneration: before.ControllerGeneration, SourceSnapshotID: currentPrimarySnapshotID(t, f), RevisedTask: &revised})
	if err == nil || !strings.Contains(err.Error(), "retained runtime") {
		t.Fatalf("missing identity was treated as unstarted: %v", err)
	}
	after, err := f.store.LoadHead()
	if err != nil || before.ControllerGeneration != after.ControllerGeneration || after.PendingTransitionID != "" {
		t.Fatal("rejected runtime mutated controller")
	}
}

func TestTaskAuthoritySyncRejectsForeignMilestoneLineage(t *testing.T) {
	for _, phase := range []string{"before", "recovery"} {
		t.Run(phase, func(t *testing.T) {
			f := taskAuthoritySyncLaneFixture(t)
			revised := f.reviseActiveTask(t, "\n追加要求\n")
			cfg, err := controller.ParentContinuationConfig(f.cfg)
			if err != nil {
				t.Fatal(err)
			}
			wf := workflow.NewWorkflow(cfg, state.AttachStateStore(cfg), nil, io.Discard)
			before, err := wf.CaptureTaskAuthoritySyncEvidence()
			if err != nil {
				t.Fatal(err)
			}
			headBefore, err := f.store.LoadHead()
			if err != nil {
				t.Fatal(err)
			}
			if phase == "recovery" {
				admission, err := controller.CurrentSemanticAdmission(f.cfg, f.store)
				if err != nil {
					t.Fatal(err)
				}
				ref, err := f.store.PutEvidenceObject("runtime-artifact", "application/json", admission.Attempt.AttemptID+":"+controller.TaskAuthoritySyncProducer+":"+currentPrimarySnapshotID(t, f), true, before)
				if err != nil {
					t.Fatal(err)
				}
				if _, err = f.store.SyncTaskAuthority(admission, currentPrimarySnapshotID(t, f), ref, revised); err != nil {
					t.Fatal(err)
				}
			}
			plan, err := executionmilestone.Load(f.runtime)
			if err != nil {
				t.Fatal(err)
			}
			plan.TaskContractSHA256 = "foreign-contract"
			if err = executionmilestone.Save(f.runtime, plan); err != nil {
				t.Fatal(err)
			}
			if phase == "before" {
				if _, err = wf.CaptureTaskAuthoritySyncEvidence(); err == nil {
					t.Fatal("foreign milestone source admitted")
				}
				head, err := f.store.LoadHead()
				if err != nil || head.ControllerGeneration != headBefore.ControllerGeneration {
					t.Fatal("rejected source mutated controller")
				}
			} else if err = wf.CompleteTaskAuthoritySync(before); err == nil {
				t.Fatal("foreign recovery milestone overwritten")
			}
			after, err := executionmilestone.Load(f.runtime)
			if err != nil || after.TaskContractSHA256 != "foreign-contract" {
				t.Fatal("foreign milestone changed")
			}
			binding, err := f.runtime.LoadControllerRuntimeBinding()
			if err != nil || binding.TaskContractDigest != f.source.Attempt.SemanticTaskRef.ContractDigest {
				t.Fatal("rejected milestone changed runtime binding")
			}
		})
	}
}

func TestParentOnlyTaskAmendmentSyncCompletesWithoutModelRuntime(t *testing.T) {
	cfg, store, policy, tasks, goShim := parentOnlyCompletionFixture(t, false)
	// Amendment admission must observe a Task-only primary worktree revision.
	// The completion fixture also seeds an untracked product; remove it before sync.
	if err := os.Remove(filepath.Join(cfg.RepoRoot, "evaluation-result.md")); err != nil {
		t.Fatal(err)
	}
	start := executeParentHandoff(t, cfg)
	if !start.Consistent || start.RequiredAction == nil || *start.RequiredAction != controllerExecutionSurface {
		t.Fatalf("parent-only start handoff: %#v", start)
	}
	runParentOnlyExecutionStep(t, cfg, start.RequiredActionParameters, true)
	before, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	bound, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	runtime := state.AttachStateStore(bound)
	taskID, err := runtime.TaskID()
	if err != nil || taskID != before.LiveAttemptID {
		t.Fatalf("parent-only smoke identity: %q %v", taskID, err)
	}
	if _, err := runtime.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent-only execution unexpectedly attached a model Runtime: %v", err)
	}

	// A same-path amendment must never promote a parent-only attempt into GLM execution.
	glmContent := parentOnlyAppTaskContent(controller.ExecutionAuthorityGLM)
	writeAppTestFile(t, cfg.RepoRoot, tasks.parent, glmContent)
	glmSnapshot, err := controller.CaptureWorkspaceSnapshot(cfg.RepoRoot)
	if err != nil {
		t.Fatal(err)
	}
	glmTask := controller.SemanticTaskRef{TaskPath: tasks.parent, ContractDigest: taskAuthoritySyncDigest(t, []byte(glmContent))}
	if _, err := workflow.ExecuteControllerExecution(cfg, store, workflow.ControllerExecutionCommand{
		Action: "sync-task-authority", ExpectedGeneration: before.ControllerGeneration,
		SourceSnapshotID: glmSnapshot.ID, RevisedTask: &glmTask,
	}); err == nil || !strings.Contains(err.Error(), "designation") {
		t.Fatalf("parent-only Amendment illegally switched to GLM authority: %v", err)
	}
	unchanged, err := store.LoadHead()
	if err != nil || unchanged.ControllerGeneration != before.ControllerGeneration || unchanged.PendingTransitionID != "" {
		t.Fatalf("rejected GLM promotion changed controller authority: %#v %v", unchanged, err)
	}

	revisedContent := parentOnlyAppTaskContent(controller.ExecutionAuthorityParentOnly) +
		"\n## Amendments\n\n1. Preserve the parent-only attempt when accepting this amendment.\n"
	writeAppTestFile(t, cfg.RepoRoot, tasks.parent, revisedContent)
	revised := controller.SemanticTaskRef{TaskPath: tasks.parent, ContractDigest: taskAuthoritySyncDigest(t, []byte(revisedContent))}
	writeAppTestFile(t, cfg.RepoRoot, "unrelated-change.txt", "must not join Task-only sync\n")
	if _, err := store.ParentOnlyTaskAuthoritySyncAdmission(cfg); err == nil {
		t.Fatal("parent-only Task sync admitted unrelated primary source edits")
	}
	if err := os.Remove(filepath.Join(cfg.RepoRoot, "unrelated-change.txt")); err != nil {
		t.Fatal(err)
	}
	// Matching IDs alone are insufficient: the admitted attempt baseline must
	// remain byte-for-byte bound to Controller's stored source authority.
	source, err := store.ParentOnlyTaskAuthoritySyncAdmission(cfg)
	if err != nil {
		t.Fatal(err)
	}
	tampered := source
	tampered.Attempt.BaselineTrees.WorktreeTree = "forged-baseline"
	if _, err := store.SyncTaskAuthority(tampered, source.Snapshot.ID, controller.EvidenceObjectRef{}, revised); err == nil || !strings.Contains(err.Error(), "source authority changed") {
		t.Fatalf("forged parent-only admission baseline was accepted: %v", err)
	}
	proof, err := store.PutEvidenceObject("runtime-artifact", "application/json",
		before.LiveAttemptID+":"+controller.TaskAuthoritySyncProducer+":"+source.Snapshot.ID,
		true, []byte(`{"runtime":"parent-only","task_id":"forged-attempt"}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.SyncTaskAuthority(source, source.Snapshot.ID, proof, revised); err == nil || !strings.Contains(err.Error(), "smoke identity") {
		t.Fatalf("forged parent-only smoke evidence was accepted: %v", err)
	}
	unchanged, err = store.LoadHead()
	if err != nil || unchanged.ControllerGeneration != before.ControllerGeneration || unchanged.PendingTransitionID != "" {
		t.Fatalf("forged smoke proof prepared a transition: %#v %v", unchanged, err)
	}
	sync := executeParentHandoff(t, cfg)
	if !sync.Consistent || sync.RequiredAction == nil || *sync.RequiredAction != controllerExecutionSurface {
		t.Fatalf("parent-only Amendment did not produce an executable handoff: %#v", sync)
	}
	payload := sync.RequiredActionParameters["controller_command_json"]
	var command workflow.ControllerExecutionCommand
	if err := json.Unmarshal([]byte(payload), &command); err != nil {
		t.Fatal(err)
	}
	if command.Action != "sync-task-authority" || command.ExpectedGeneration != before.ControllerGeneration ||
		command.RevisedTask == nil || !command.RevisedTask.Equal(revised) {
		t.Fatalf("parent-only Amendment command: %#v", command)
	}

	stale := command
	stale.ExpectedGeneration++
	if _, err := workflow.ExecuteControllerExecution(cfg, store, stale); err == nil {
		t.Fatal("stale parent-only Amendment generation was accepted")
	}
	foreign := command
	foreign.RevisedTask = &controller.SemanticTaskRef{TaskPath: tasks.successor, ContractDigest: revised.ContractDigest}
	if _, err := workflow.ExecuteControllerExecution(cfg, store, foreign); err == nil {
		t.Fatal("foreign parent-only Amendment Task was accepted")
	}
	executeStagedControllerAction(t, cfg, controllerExecutionSurface, payload, true)

	after, err := store.LoadHead()
	if err != nil {
		t.Fatal(err)
	}
	if after.LiveAttemptID != before.LiveAttemptID || after.LiveLeaseID == before.LiveLeaseID ||
		after.RootTaskRef == nil || !after.RootTaskRef.Equal(revised) ||
		after.ExecutionTaskRef == nil || !after.ExecutionTaskRef.Equal(revised) {
		t.Fatalf("parent-only Amendment did not retain same-attempt revised Task authority: %#v", after)
	}
	if _, err := runtime.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("parent-only Amendment manufactured a Runtime: %v", err)
	}
	if actual, err := runtime.TaskID(); err != nil || actual != taskID {
		t.Fatalf("parent-only Amendment lost old smoke attempt identity: %q %v", actual, err)
	}
	// Task digest changes the state-store namespace. The amended Task must also
	// carry the same smoke attempt identity with no model Runtime binding.
	revisedConfig, err := controller.WorkflowConfig(cfg)
	if err != nil {
		t.Fatal(err)
	}
	revisedRuntime := state.AttachStateStore(revisedConfig)
	if current, err := revisedRuntime.TaskID(); err != nil || current != taskID {
		t.Fatalf("amended Task state lacks the same smoke attempt identity: %q %v", current, err)
	}
	if _, err := revisedRuntime.LoadControllerRuntimeBinding(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("amended Task state created a GLM Runtime binding: %v", err)
	}
	// Completion must use the exact post-sync snapshot. Introducing another
	// unrecorded product here would invalidate the live lease.
	next := executeParentHandoff(t, cfg)
	if !next.Consistent || next.RequiredAction == nil || *next.RequiredAction != controllerPublicationSurface {
		t.Fatalf("parent-only Amendment did not proceed to completion: %#v", next)
	}
	driveParentOnlyCompletion(t, cfg, goShim, false, false, true)
	assertParentOnlyTerminalState(t, cfg, store, policy, tasks, false)
}

func TestParentOnlyTaskAuthoritySyncEvidenceRejectsRuntimeSpoofing(t *testing.T) {
	parent := controller.Admission{Attempt: controller.AttemptRecord{AttemptID: "parent-attempt", ExecutionAuthority: controller.ExecutionAuthorityParentOnly}}
	ordinary := controller.Admission{Attempt: controller.AttemptRecord{AttemptID: "worker-attempt"}}
	for _, scenario := range []struct {
		name      string
		admission controller.Admission
		source    workflow.TaskAuthoritySyncEvidence
	}{
		{"missing-smoke-id", parent, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeParentOnly}},
		{"wrong-smoke-id", parent, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeParentOnly, TaskID: "wrong-attempt"}},
		{"unstarted-parent-only", parent, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeUnstarted}},
		{"ordinary-runtime-parent-only", parent, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeAttached}},
		{"spoofed-parent-only-on-GLM", ordinary, workflow.TaskAuthoritySyncEvidence{Runtime: workflow.TaskAuthorityRuntimeParentOnly, TaskID: "worker-attempt"}},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			raw, err := json.Marshal(scenario.source)
			if err != nil {
				t.Fatal(err)
			}
			if err := workflow.VerifyTaskAuthoritySyncRuntime(raw, scenario.admission); err == nil {
				t.Fatal("incorrect Runtime evidence accepted")
			}
		})
	}
}
