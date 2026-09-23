package parentcontinuation

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/autoresume"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecttree"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskview"
)

type Automation struct {
	AutomationID string `json:"automation_id"`
	ParentThread string `json:"parent_thread"`
	WakeThread   string `json:"wake_thread"`
	ResumeAtUTC  string `json:"resume_at_utc"`
	WakeAtUTC    string `json:"wake_at_utc"`
}

type Request struct {
	CompletionAdmitted bool
	StopAdmitted       bool
	Continuation       repositoryproject.Continuation
	TaskAttribution    repositoryproject.TaskAttribution
	Automation         *Automation
}

type Projection struct {
	Consistent      bool
	Inconsistency   *string
	ActionPlan      *state.ParentActionPlan
	Snapshot        *state.SnapshotDigest
	SessionRotation *state.SessionRotationProjection
	ParentRequest   *Request
}

const ReasonVerifiedAutomation = "verified-automation"

func Build(cfg config.AppConfig, st *state.StateStore) Projection {
	return build(cfg, st, autoresume.ReadDBRowSqlite3)
}

func build(cfg config.AppConfig, st *state.StateStore, readDB autoresume.DBReader) Projection {
	projection := Projection{
		Consistent:      true,
		SessionRotation: &state.SessionRotationProjection{State: state.SessionRotationProjectionUnavailable},
	}
	plan, planErr := st.ParentActionPlan()
	if planErr != nil {
		markInconsistent(&projection, planErr.Error())
	} else {
		projection.ActionPlan = &plan
	}

	repoRoot := st.ReadOr("repo-root", "")
	applyRequest(repoRoot, st, plan, &projection)
	applySnapshot(repoRoot, &projection)
	applySessionRotation(st, &projection)
	applyVerifiedAutomation(cfg, st, &projection, readDB)
	return projection
}

func BuildPostCompletionRequest(repoRoot, completedTask string) (Request, error) {
	policy, err := repositoryprojecttree.BuildParentRequestCompletionProjection(repoRoot, completedTask)
	if err != nil {
		return Request{}, err
	}
	return Request{
		CompletionAdmitted: policy.CompletionAdmitted,
		StopAdmitted:       policy.StopAdmitted,
		Continuation:       policy.Continuation,
	}, nil
}

func BuildProjectContinuation(repoRoot string, st *state.StateStore, loaded repositoryprojecttree.ProjectState) (repositoryproject.Continuation, *CompletionEvidence, error) {
	project := continuationProjectView(loaded)
	evidence, err := BuildCompletionEvidence(repoRoot, st, loaded)
	if err != nil {
		return repositoryproject.Continuation{}, nil, err
	}
	if evidence != nil {
		view := evidence.View
		project.Completion = &view
	}
	return repositoryproject.DeriveContinuation(project, continuationLifecycle(st)), evidence, nil
}

func applyRequest(repoRoot string, st *state.StateStore, plan state.ParentActionPlan, projection *Projection) {
	if repoRoot == "" {
		return
	}
	active, err := repositoryharness.RuntimeActive(repoRoot, st)
	if err != nil {
		markInconsistent(projection, "repository harness activation is unavailable: "+err.Error())
		return
	}
	if !active {
		return
	}
	request, err := buildCurrentRequest(repoRoot, st)
	if err != nil {
		markInconsistent(projection, "project continuation projection is unavailable: "+err.Error())
		return
	}
	projection.ParentRequest = &request
	if request.TaskAttribution.Reason == repositoryproject.ReasonActiveTaskMismatch && !request.TaskAttribution.Handover {
		markInconsistent(projection, "canonical handoff task attribution mismatch: lifecycle="+request.TaskAttribution.LifecycleTask+", authority="+request.TaskAttribution.AuthorityTask+", active="+request.TaskAttribution.ActiveTask)
		return
	}
	if fatalActiveContinuationWithoutAction(st, plan, projection) && latestParentMaterialOutcome(st) == "error" {
		markInconsistent(projection, "active task has a terminal error while project continuation is required but no parent action is admitted")
	}
}

func buildCurrentRequest(repoRoot string, st *state.StateStore) (Request, error) {
	status := st.TaskStatus()
	var request Request
	if status == state.TaskStatusAwaitingParentCompletion || status == state.TaskStatusComplete {
		postCompletion, err := BuildPostCompletionRequest(repoRoot, st.ReadOr("active-task", ""))
		if err != nil {
			return Request{}, err
		}
		request = postCompletion
	} else {
		loaded, err := repositoryprojecttree.LoadProjectState(repoRoot)
		if err != nil {
			return Request{}, err
		}
		continuation, _, err := BuildProjectContinuation(repoRoot, st, loaded)
		if err != nil {
			return Request{}, err
		}
		policy := repositoryproject.ParentRequestProjection(
			continuation,
			continuation.Reason != string(state.TaskStatusRateLimited),
		)
		request = Request{
			CompletionAdmitted: policy.CompletionAdmitted,
			StopAdmitted:       policy.StopAdmitted,
			Continuation:       policy.Continuation,
		}
	}
	attribution, err := repositoryprojecttree.BuildTaskAttribution(
		repoRoot,
		st.ReadOr("active-task", ""),
		request.Continuation,
	)
	if err != nil {
		return Request{}, err
	}
	request.TaskAttribution = attribution
	if authorityTask, authorityErr := st.CurrentTaskAuthorityPath(); authorityErr == nil {
		request.TaskAttribution = repositoryproject.BindTaskAuthority(request.TaskAttribution, authorityTask)
	} else if request.TaskAttribution.Handover {
		request.TaskAttribution = repositoryproject.BindTaskAuthority(request.TaskAttribution, "")
	}
	return request, nil
}

func continuationProjectView(loaded repositoryprojecttree.ProjectState) repositoryproject.ContinuationProjectView {
	view := repositoryproject.ContinuationProjectView{PlanPresent: loaded.PlanPresent}
	if !loaded.PlanPresent {
		return view
	}
	view.ProjectReady = true
	view.GoalPresent = loaded.Plan.Goal.Present
	view.GoalCompleted = loaded.Plan.Goal.Present && loaded.Plan.Goal.Status == taskcontract.GoalStatusCompleted
	view.Active = append([]string(nil), loaded.Plan.Active...)
	view.NextRunnable = loaded.Graph.NextRunnable(loaded.Plan.Next)
	view.Blockers = loaded.Graph.Blockers(loaded.Plan.Next, loaded.Plan.Blocked)
	return view
}

func continuationLifecycle(st *state.StateStore) repositoryproject.ContinuationLifecycle {
	status := st.TaskStatus()
	pinned := st.ReadOr("active-task", "")
	lifecycle := repositoryproject.ContinuationLifecycle{
		Interrupted:  status == state.TaskStatusInterrupted,
		PinnedTask:   pinned,
		TaskAbsent:   status == state.TaskStatusNone,
		TaskComplete: status == state.TaskStatusComplete,
	}
	plan, planErr := st.ParentActionPlan()
	planKnown := planErr == nil
	if planKnown {
		lifecycle.ParentActionKnown = true
		lifecycle.RequiredAction = string(plan.RequiredAction)
		lifecycle.NoRequiredAction = plan.RequiredAction == state.ParentActionNone
	}
	lifecycle.InterruptedResumeValid = interruptedResumeValid(st, status, planKnown, plan.RequiredAction)
	lifecycle.TemporaryBlockReason = temporaryBlockReason(status)
	lifecycle.GoalTerminalCompatible = goalTerminalCompatible(status, pinned, planKnown, plan.RequiredAction)
	return lifecycle
}

func interruptedResumeValid(st *state.StateStore, status state.TaskStatus, planKnown bool, action state.ParentAction) bool {
	if status != state.TaskStatusInterrupted || !planKnown || action != state.ParentActionResume {
		return false
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	return err == nil && checkpoint.StopKind == state.ResumeStopInterrupted
}

func temporaryBlockReason(status state.TaskStatus) string {
	switch status {
	case state.TaskStatusRateLimited, state.TaskStatusProviderUnavailable:
		return string(status)
	default:
		return ""
	}
}

func goalTerminalCompatible(status state.TaskStatus, pinned string, planKnown bool, action state.ParentAction) bool {
	if status != state.TaskStatusNone && status != state.TaskStatusComplete {
		return false
	}
	if status == state.TaskStatusNone && pinned != "" {
		return false
	}
	return planKnown && action == state.ParentActionNone
}

func fatalActiveContinuationWithoutAction(st *state.StateStore, plan state.ParentActionPlan, projection *Projection) bool {
	return projection.Consistent &&
		st.TaskStatus() == state.TaskStatusActive &&
		plan.RequiredAction == state.ParentActionNone &&
		len(plan.AllowedActions) == 0 &&
		projection.ParentRequest != nil &&
		projection.ParentRequest.Continuation.State == repositoryproject.ContinuationContinueNow
}

func latestParentMaterialOutcome(st *state.StateStore) string {
	taskID := st.ReadOr("task.id", "")
	logs, err := taskview.ReadStatusTelemetry(st, taskID)
	if err != nil {
		return ""
	}
	for index := len(logs) - 1; index >= 0; index-- {
		if logs[index].CallType != state.CallTypeProbe {
			return logs[index].Outcome
		}
	}
	return ""
}

func applySnapshot(repoRoot string, projection *Projection) {
	if repoRoot == "" {
		markInconsistent(projection, "repository root is unavailable")
		return
	}
	snapshot, err := state.CaptureGitSnapshot(repoRoot)
	if err != nil {
		markInconsistent(projection, "current repository snapshot is unavailable: "+err.Error())
		return
	}
	projection.Snapshot = &state.SnapshotDigest{
		Head:                          snapshot.Head,
		IndexDigest:                   snapshot.IndexDigest,
		WorktreeDigest:                snapshot.WorktreeDigest,
		WorktreeDigestExcludingParent: snapshot.WorktreeDigestExcludingParent,
	}
}

func applySessionRotation(st *state.StateStore, projection *Projection) {
	threadID := ""
	if identity, err := st.CurrentParentCodexIdentity(); err == nil {
		threadID = identity.ThreadID
	} else if !errors.Is(err, os.ErrNotExist) {
		markInconsistent(projection, "parent Codex identity is unavailable: "+err.Error())
		return
	}
	session, err := st.ProjectSessionRotation(threadID)
	if err != nil {
		markInconsistent(projection, "session rotation projection is unavailable: "+err.Error())
		return
	}
	projection.SessionRotation = &session
}

func applyVerifiedAutomation(cfg config.AppConfig, st *state.StateStore, projection *Projection, readDB autoresume.DBReader) {
	if projection.ParentRequest == nil ||
		projection.ParentRequest.Continuation.State != repositoryproject.ContinuationBlocked ||
		projection.ParentRequest.Continuation.Reason != string(state.TaskStatusRateLimited) {
		return
	}
	projection.ParentRequest.StopAdmitted = false
	proof, ok := verifiedAutomation(cfg, st, readDB)
	if !ok {
		return
	}
	projection.ParentRequest.Continuation.State = repositoryproject.ContinuationDeferredByVerifiedAutomation
	projection.ParentRequest.Continuation.Reason = ReasonVerifiedAutomation
	projection.ParentRequest.Automation = &proof
	projection.ParentRequest.StopAdmitted = true
}

func verifiedAutomation(cfg config.AppConfig, st *state.StateStore, readDB autoresume.DBReader) (Automation, bool) {
	if cfg.CodexConfigDir == "" {
		return Automation{}, false
	}
	checkpoint, err := st.LoadResumeCheckpoint()
	if err != nil || checkpoint.StopKind != state.ResumeStopRateLimited || checkpoint.ResetAtRFC3339 == "" {
		return Automation{}, false
	}
	identity, err := st.CurrentParentCodexIdentity()
	if err != nil || identity.ThreadID == "" {
		return Automation{}, false
	}
	result, err := autoresume.CheckCoalesce(autoresume.CoalesceParams{
		ParentThreadID:  identity.ThreadID,
		ResumeAtRFC3339: checkpoint.ResetAtRFC3339,
		AutomationsDir:  filepath.Join(cfg.CodexConfigDir, "automations"),
		DBPath:          filepath.Join(cfg.CodexConfigDir, "sqlite", "codex-dev.db"),
	}, readDB)
	if err != nil || result.Decision != autoresume.DecisionCoalesce {
		return Automation{}, false
	}
	return Automation{
		AutomationID: result.WakeAutomationID,
		ParentThread: result.ParentThread,
		WakeThread:   result.WakeThread,
		ResumeAtUTC:  result.ResumeAtUTC,
		WakeAtUTC:    result.WakeNextRunUTC,
	}, true
}

func markInconsistent(projection *Projection, detail string) {
	projection.Consistent = false
	if projection.Inconsistency == nil {
		projection.Inconsistency = &detail
	}
}
