package parentcontinuation

import (
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

type ObservationCapabilityAdmission struct {
	Lifecycle state.ObservationExecutionAdmission
	Policy    repositoryproject.ObservationCapabilityPolicy
}

func CurrentObservationCapabilityAdmission(st *state.StateStore) (ObservationCapabilityAdmission, error) {
	lifecycle, err := st.ObservationExecuteAdmission()
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

func ValidateObservationCapabilityAdmission(st *state.StateStore, expected ObservationCapabilityAdmission) error {
	current, err := CurrentObservationCapabilityAdmission(st)
	if err != nil {
		return err
	}
	if current != expected {
		return fmt.Errorf("observation capability admission changed: task=%s round=%d status=%s", current.Lifecycle.TaskID, current.Lifecycle.Round, current.Policy.Status)
	}
	return nil
}

func BuildWithRepositoryPolicy(cfg config.AppConfig, st *state.StateStore) Projection {
	projection := Build(cfg, st)
	if !projection.Consistent || projection.ActionPlan == nil {
		return projection
	}
	admission, err := CurrentObservationCapabilityAdmission(st)
	if err != nil {
		return projection
	}
	plan := projectObservationCapabilityActions(*projection.ActionPlan, admission.Policy.Admitted)
	projection.ActionPlan = &plan
	return projection
}

func projectObservationCapabilityActions(plan state.ParentActionPlan, admitted bool) state.ParentActionPlan {
	if !admitted || plan.RequiredAction != state.ParentActionDecision {
		return plan
	}
	plan.AllowedActions = append(append([]state.ParentAction(nil), plan.AllowedActions...), state.ParentActionObservationExecute, state.ParentActionNoGo)
	return plan
}
