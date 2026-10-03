package parentactioncmd

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/failurepathadvisory"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

const publicationMachineAcceptanceGateName = "machine-acceptance"

func publicationMachineAcceptanceGate(repoRoot string, st *state.StateStore, candidate state.PublicationCandidate) publicationGateProjection {
	gate := publicationGateProjection{
		Gate:       publicationMachineAcceptanceGateName,
		Status:     publicationGatePass,
		SnapshotID: candidate.SnapshotID,
	}
	taskID, err := st.TaskID()
	if err != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, err.Error())
	}
	if candidate.TaskID != taskID {
		return machineAcceptanceGateFailure(gate, publicationGateFail,
			fmt.Sprintf("publication candidate task %s does not match current task %s", candidate.TaskID, taskID))
	}
	taskPath, err := st.CurrentTaskAuthorityPath()
	if err != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, err.Error())
	}
	content, err := publicationTaskAuthorityContent(repoRoot, st, candidate, taskID, taskPath)
	if err != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, err.Error())
	}
	contract, err := taskcontract.ParseMachineAcceptance(content)
	if err != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, err.Error())
	}
	if !contract.Present {
		return gate
	}
	gate.Required = true
	registry, err := failurepathadvisory.LoadRegistry(st.Path(failurepathadvisory.RegistryFile))
	if err != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, "machine acceptance evidence is unreadable: "+err.Error())
	}
	for _, requirement := range contract.Requirements {
		if machineAcceptanceRequirementSatisfied(registry, taskID, requirement) {
			continue
		}
		return machineAcceptanceGateFailure(gate, publicationGateMissing,
			fmt.Sprintf("requirement %s (%s) is unproven for current task %s", requirement.ID, requirement.Fact, taskID))
	}
	return gate
}

func machineAcceptanceGateFailure(gate publicationGateProjection, status, reason string) publicationGateProjection {
	gate.Required = true
	gate.Status = status
	gate.Reason = compactFinalizationDiagnostic(reason)
	return gate
}

func publicationTaskAuthorityContent(
	repoRoot string,
	st *state.StateStore,
	candidate state.PublicationCandidate,
	taskID string,
	taskPath string,
) ([]byte, error) {
	authorityHead := candidate.BaseHead
	tracked, err := publicationTaskTrackedAt(repoRoot, authorityHead, taskPath)
	if err != nil {
		return nil, err
	}
	if !tracked {
		lineage, err := st.LoadPublicationReopenLineage()
		if err != nil {
			return nil, fmt.Errorf("task %s is absent from candidate base and reopen lineage is unavailable: %w", taskPath, err)
		}
		if err := verifyCompletionReopenLineageIdentity(lineage, taskPath, taskID); err != nil {
			return nil, err
		}
		if err := verifyCompletionReopenLineageTransition(repoRoot, lineage, taskPath); err != nil {
			return nil, err
		}
		if _, err := gitFinalizationOutput(repoRoot, "merge-base", "--is-ancestor", lineage.CommitOID, candidate.BaseHead); err != nil {
			return nil, fmt.Errorf("candidate base %s does not descend from reopen lineage %s", candidate.BaseHead, lineage.CommitOID)
		}
		authorityHead = lineage.BaseHead
	}
	content, err := gitFinalizationOutput(repoRoot, "show", authorityHead+":"+taskPath)
	if err != nil {
		return nil, fmt.Errorf("read task authority %s at %s: %w", taskPath, authorityHead, err)
	}
	return []byte(content), nil
}

func publicationTaskTrackedAt(repoRoot, head, taskPath string) (bool, error) {
	entry, err := gitFinalizationOutput(repoRoot, "ls-tree", head, "--", taskPath)
	if err != nil {
		return false, fmt.Errorf("inspect task authority %s at %s: %w", taskPath, head, err)
	}
	return strings.TrimSpace(entry) != "", nil
}

func machineAcceptanceRequirementSatisfied(
	registry failurepathadvisory.Registry,
	taskID string,
	requirement taskcontract.MachineAcceptanceRequirement,
) bool {
	for _, record := range registry.Records {
		if record.TaskID != taskID || record.Outcome != failurepathadvisory.OutcomeObserved || strings.TrimSpace(record.CallID) == "" {
			continue
		}
		switch requirement.Fact {
		case taskcontract.MachineFactFailurePathAdvisoryObserved:
			return true
		case taskcontract.MachineFactFailurePathAdvisoryShown:
			if record.Advisory != nil && record.Advisory.Status == failurepathadvisory.AdvisoryShown {
				return true
			}
		}
	}
	return false
}
