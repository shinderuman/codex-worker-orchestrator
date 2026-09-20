package parentactioncmd

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/app"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repolock"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojecthead"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

type completeOutput struct {
	Status        string                                               `json:"status"`
	Completed     bool                                                 `json:"completed"`
	RemoteSync    *completeRemoteSyncSummary                           `json:"remote_sync,omitempty"`
	ParentRequest *repositoryproject.ParentRequestCompletionProjection `json:"parent_request,omitempty"`
	NextAction    *app.PublicationActionSpec                           `json:"next_action,omitempty"`
	Failure       *finalizationFailure                                 `json:"failure,omitempty"`
}

type completeRemoteSyncSummary struct {
	Applicable       bool   `json:"applicable"`
	State            string `json:"state"`
	RemoteName       string `json:"remote_name,omitempty"`
	RemoteRef        string `json:"remote_ref,omitempty"`
	ExpectedOID      string `json:"expected_oid,omitempty"`
	RemoteOID        string `json:"remote_oid,omitempty"`
	Ahead            int    `json:"ahead,omitempty"`
	Behind           int    `json:"behind,omitempty"`
	PostconditionMet bool   `json:"postcondition_met"`
}

type completionVerification struct {
	remoteSync              *completeRemoteSyncSummary
	verifiedHead            string
	gitRepo                 bool
	repositoryHarnessActive bool
	failure                 *finalizationFailure
}

const (
	completeRemoteStateVerified                 = "verified"
	completeRemoteStateNotApplicable            = "not_applicable"
	completeStatusComplete                      = "complete"
	completeStatusAwaiting                      = "awaiting"
	completePushStatusBlocked                   = "blocked"
	completeTargetNoUpstream                    = "no_upstream"
	completeTargetDetached                      = "detached_head"
	completeTargetRemoteUnresolvable            = "remote_unresolvable"
	completeTargetHeadUnresolvable              = "head_unresolvable"
	completeFailureTreeChanged                  = "tree_changed_before_transition"
	completeFailureHeadChanged                  = "head_changed_before_transition"
	completeFailureTerminalUnrecoverable        = "completion_terminal_unrecoverable"
	completeFailureParentRequestProjectionError = "parent_request_projection_unavailable"
)

func executeComplete(cfg config.AppConfig, args []string, stdout io.Writer) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: glm-parent-action complete")
	}
	if err := persistParentCodexIdentity(cfg); err != nil {
		return err
	}
	return runComplete(cfg, stdout)
}

func runComplete(cfg config.AppConfig, stdout io.Writer) error {
	st, err := state.NewStateStore(cfg)
	if err != nil {
		return err
	}
	lock, err := repolock.Acquire(st.LockPath())
	if err != nil {
		return err
	}
	defer func() { _ = lock.Close() }()

	plan, err := st.ParentActionPlan()
	if err != nil {
		return err
	}
	if !plan.Allows(state.ParentActionComplete) {
		return fmt.Errorf("parent completion is not admitted for the current task (required action %s)", plan.RequiredAction)
	}
	verification, parentRequest := prepareCompletionVerification(cfg, st)
	if verification.failure != nil {
		return json.NewEncoder(stdout).Encode(completeOutput{
			Status:        completeStatusAwaiting,
			Completed:     false,
			RemoteSync:    verification.remoteSync,
			ParentRequest: parentRequest,
			NextAction:    app.ProjectPublicationSequence(cfg.RepoRoot, st).NextAction,
			Failure:       verification.failure,
		})
	}
	terminal, failure := completionTerminalForEvaluation(st)
	if failure != nil {
		return json.NewEncoder(stdout).Encode(completeOutput{
			Status:        completeStatusAwaiting,
			Completed:     false,
			ParentRequest: parentRequest,
			NextAction:    app.ProjectPublicationSequence(cfg.RepoRoot, st).NextAction,
			Failure:       failure,
		})
	}
	completed, err := st.CompleteParentAwaiting(func(acceptedRisk string) (*state.SessionRotationEvaluation, error) {
		return app.EvaluateCanonicalSessionRotationTerminal(cfg, st, terminal, acceptedRisk)
	})
	if err != nil {
		return err
	}
	return json.NewEncoder(stdout).Encode(completeOutput{
		Status:        completeStatusComplete,
		Completed:     completed,
		RemoteSync:    verification.remoteSync,
		ParentRequest: parentRequest,
	})
}

func completionTerminalForEvaluation(st *state.StateStore) (string, *finalizationFailure) {
	outcome, err := st.CurrentParentCompletionOutcome()
	if err != nil || outcome == nil {
		return "", &finalizationFailure{Stage: "state", Reason: completeFailureTerminalUnrecoverable}
	}
	st.UpdateTaskStats(func(stats *state.TaskStats) {
		stats.CompletionTerminal = outcome.Terminal
		stats.AcceptedRisk = outcome.Risk
	})
	return outcome.Terminal, nil
}

func verifyParentCompletion(repoRoot string, st *state.StateStore) completionVerification {
	if _, err := gitFinalizationOutput(repoRoot, "rev-parse", "--git-dir"); err != nil {
		return completionVerification{
			remoteSync: &completeRemoteSyncSummary{Applicable: false, State: completeRemoteStateNotApplicable},
		}
	}
	if !pushBindingTreeClean(repoRoot) {
		return completionVerification{failure: &finalizationFailure{Stage: "git", Reason: "tree_not_clean"}}
	}
	repositoryHarnessActive, err := workflow.RepositoryHarnessActive(repoRoot, st)
	if err != nil {
		return completionVerification{failure: &finalizationFailure{
			Stage: "metadata", Reason: "completion_transition_invalid", Detail: compactFinalizationDiagnostic(err.Error()),
		}}
	}
	if repositoryHarnessActive {
		if _, err := repositoryprojecthead.CheckParentCompletionHead(repoRoot); err != nil {
			return completionVerification{failure: &finalizationFailure{
				Stage: "metadata", Reason: "completion_transition_invalid", Detail: compactFinalizationDiagnostic(err.Error()),
			}}
		}
	}
	headOID, unborn, failure := completeHeadState(repoRoot)
	if failure != nil {
		return completionVerification{failure: failure}
	}
	if failure := verifyCompletedTaskFileRemoved(repoRoot, st, headOID); failure != nil {
		return completionVerification{failure: failure}
	}
	if unborn {
		return completionVerification{
			remoteSync:              &completeRemoteSyncSummary{Applicable: false, State: completeRemoteStateNotApplicable},
			verifiedHead:            headOID,
			gitRepo:                 true,
			repositoryHarnessActive: repositoryHarnessActive,
		}
	}
	remoteSync, failure := verifyCompletionRemoteSync(repoRoot)
	return completionVerification{
		remoteSync:              remoteSync,
		verifiedHead:            headOID,
		gitRepo:                 true,
		repositoryHarnessActive: repositoryHarnessActive,
		failure:                 failure,
	}
}

func verifyCompletionUnchanged(repoRoot string, gitRepo bool, verifiedHead string) *finalizationFailure {
	if !gitRepo {
		return nil
	}
	if !pushBindingTreeClean(repoRoot) {
		return &finalizationFailure{Stage: "git", Reason: completeFailureTreeChanged}
	}
	head, _, failure := completeHeadState(repoRoot)
	if failure != nil {
		return &finalizationFailure{Stage: "git", Reason: completeFailureHeadChanged, Detail: failure.Reason}
	}
	if head != verifiedHead {
		return &finalizationFailure{
			Stage:  "git",
			Reason: completeFailureHeadChanged,
			Detail: compactFinalizationDiagnostic("verified " + verifiedHead + " observed " + head),
		}
	}
	return nil
}

func completeHeadState(repoRoot string) (string, bool, *finalizationFailure) {
	head, err := state.ResolveGitHeadAuthority("git", repoRoot)
	if err != nil {
		return "", false, &finalizationFailure{Stage: "target", Reason: completeTargetHeadUnresolvable}
	}
	return head.Head, head.Unborn, nil
}

func verifyCompletedTaskFileRemoved(repoRoot string, st *state.StateStore, headOID string) *finalizationFailure {
	pinned := st.ReadOr("active-task", "")
	if pinned == "" || headOID == "" {
		return nil
	}
	tracked, err := gitFinalizationOutput(repoRoot, "ls-tree", "HEAD", "--", pinned)
	if err != nil {
		return &finalizationFailure{Stage: "metadata", Reason: "completed_task_file_unreadable", Detail: compactFinalizationDiagnostic(err.Error())}
	}
	if strings.TrimSpace(tracked) != "" {
		return &finalizationFailure{Stage: "metadata", Reason: "completed_task_file_still_tracked", Detail: pinned}
	}
	return nil
}

func verifyCompletionRemoteSync(repoRoot string) (*completeRemoteSyncSummary, *finalizationFailure) {
	binding := buildPushBinding(repoRoot, pushBindingOptions{})
	if binding.Status == completePushStatusBlocked {
		if binding.Failure == nil {
			return nil, &finalizationFailure{Stage: "target", Reason: completeTargetHeadUnresolvable}
		}
		switch binding.Failure.Reason {
		case completeTargetNoUpstream, completeTargetDetached:
			return &completeRemoteSyncSummary{Applicable: false, State: completeRemoteStateNotApplicable}, nil
		case completeTargetRemoteUnresolvable:
			return &completeRemoteSyncSummary{Applicable: true, State: completeTargetRemoteUnresolvable},
				&finalizationFailure{Stage: "remote_sync", Reason: completeTargetRemoteUnresolvable}
		default:
			return nil, &finalizationFailure{Stage: "target", Reason: binding.Failure.Reason}
		}
	}
	summary := &completeRemoteSyncSummary{
		Applicable:       true,
		State:            binding.Classification,
		ExpectedOID:      binding.ExpectedOID,
		PostconditionMet: binding.Postcondition != nil && binding.Postcondition.Met,
	}
	if binding.Target != nil {
		summary.RemoteName = binding.Target.RemoteName
		summary.RemoteRef = binding.Target.RemoteRef
		summary.Ahead = binding.Target.Ahead
		summary.Behind = binding.Target.Behind
	}
	if binding.RemoteProbe != nil {
		summary.RemoteOID = binding.RemoteProbe.RemoteOID
	}
	if binding.Classification == pushBindingClassificationSynced {
		summary.State = completeRemoteStateVerified
		return summary, nil
	}
	return summary, &finalizationFailure{Stage: "remote_sync", Reason: binding.Classification}
}
