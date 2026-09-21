package state

import (
	"errors"
	"fmt"
	"os"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/controlprovenance"
)

const parentActionAdmissionControlID = "parent-action-staging-admission"

func (s *StateStore) AdmitParentActionByAuthority(action ParentAction) (ParentActionPlan, bool, error) {
	plan, err := s.ParentActionPlan()
	if err != nil {
		return ParentActionPlan{}, false, err
	}

	admitted, err := s.parentActionAuthorityAdmits(plan, action)
	if err != nil {
		return ParentActionPlan{}, false, err
	}
	if action == ParentActionResume && admitted {
		if err := s.rejectResumeBeforeRateLimitReset(); err != nil {
			return ParentActionPlan{}, false, err
		}
	}
	return plan, admitted, nil
}

func (s *StateStore) parentActionAuthorityAdmits(plan ParentActionPlan, action ParentAction) (bool, error) {
	if plan.Allows(action) {
		return true, nil
	}

	authority, err := s.ParentActionResultAuthority()
	if err != nil {
		return false, err
	}
	if authority == nil {
		return false, nil
	}
	if !authority.ParentMayPromoteNegativeResult() {
		return false, nil
	}
	return plan.AdmitsCommand(action), nil
}

func (s *StateStore) ParentActionResultAuthority() (*controlprovenance.ResultAuthority, error) {
	repoRoot := s.ReadOr("repo-root", "")
	if repoRoot == "" {
		return nil, nil
	}
	registry, err := controlprovenance.Load(repoRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("load parent-action control authority: %w", err)
	}
	authority, err := registry.ResultAuthority(parentActionAdmissionControlID)
	if err != nil {
		return nil, fmt.Errorf("resolve parent-action control authority: %w", err)
	}
	return &authority, nil
}
