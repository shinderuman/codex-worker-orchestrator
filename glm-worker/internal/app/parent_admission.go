package app

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/workflow"
)

func admitParentCommand(cmd Command, st *state.StateStore) error {
	if cmd.Mode != ModeNewTask {
		action, parentCommand := commandParentAction(cmd.Mode)
		if !parentCommand {
			return nil
		}
		plan, err := parentActionPlan(st)
		if err != nil {
			return err
		}
		if plan.AdmitsCommand(action) {
			return nil
		}
		return parentActionDenied(string(action), plan)
	}

	plan, err := parentActionPlan(st)
	if err != nil {
		return err
	}
	if plan.RequiredAction == state.ParentActionNone {
		return nil
	}
	return parentActionDenied("new-task", plan)
}

func parentActionPlan(st *state.StateStore) (state.ParentActionPlan, error) {
	plan, err := st.ParentActionPlan()
	if err != nil {
		return state.ParentActionPlan{}, &workflow.WorkerError{Message: err.Error()}
	}
	return plan, nil
}

func commandParentAction(mode CommandMode) (state.ParentAction, bool) {
	switch mode {
	case ModeDecision:
		return state.ParentActionDecision, true
	case ModeFix:
		return state.ParentActionFix, true
	case ModeAccept:
		return state.ParentActionAccept, true
	case ModeResume:
		return state.ParentActionResume, true
	default:
		return state.ParentActionNone, false
	}
}

func parentActionDenied(requested string, plan state.ParentActionPlan) error {
	allowed := make([]string, 0, len(plan.AllowedActions))
	for _, action := range plan.AllowedActions {
		allowed = append(allowed, string(action))
	}
	return &workflow.WorkerError{Message: fmt.Sprintf(
		"parent action %s is not admitted; required_action=%s allowed_actions=%s",
		requested,
		plan.RequiredAction,
		strings.Join(allowed, ","),
	)}
}
