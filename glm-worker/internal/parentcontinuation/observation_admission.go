package parentcontinuation

import (
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ObservationCapabilityAdmission struct {
	Lifecycle state.ObservationExecutionAdmission
	Policy    repositoryproject.ObservationCapabilityPolicy
}

func CurrentObservationCapabilityAdmission(st *state.StateStore) (ObservationCapabilityAdmission, error) {
	lifecycle, err := st.ObservationLifecycleAdmission()
	if err != nil {
		return ObservationCapabilityAdmission{}, err
	}
	content, err := os.ReadFile(st.TaskAuthorityContentPath(lifecycle.TaskID))
	if err != nil {
		return ObservationCapabilityAdmission{}, fmt.Errorf("task authority snapshotを読めません: %w", err)
	}
	policy, err := repositoryproject.EvaluateObservationCapability(content)
	if err != nil {
		return ObservationCapabilityAdmission{}, err
	}
	if !policy.Admitted {
		return ObservationCapabilityAdmission{}, fmt.Errorf("observation-executeはstatus %sではadmitされません(poc/observationだけが対象です)", policy.Status)
	}
	return ObservationCapabilityAdmission{Lifecycle: lifecycle, Policy: policy}, nil
}

func ProjectParentActionPlan(st *state.StateStore) (state.ParentActionPlan, error) {
	plan, err := st.ParentActionPlan()
	if err != nil {
		return state.ParentActionPlan{}, err
	}
	if plan.RequiredAction != state.ParentActionDecision {
		return plan, nil
	}
	if _, err := CurrentObservationCapabilityAdmission(st); err == nil {
		plan.AllowedActions = append(plan.AllowedActions, state.ParentActionObservationExecute, state.ParentActionNoGo)
	}
	return plan, nil
}
