package parentactioncmd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
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
		return gate
	}

	taskPath, canonicalPathErr := st.CurrentTaskAuthorityPath()
	if canonicalPathErr != nil {
		taskPath = st.ReadOr("active-task", "")
	}
	if taskPath == "" {
		declared, probeErr := savedTaskDeclaresMachineAcceptance(st, taskID)
		if probeErr != nil {
			return machineAcceptanceGateFailure(gate, publicationGateFail, probeErr.Error())
		}
		if !declared {
			return gate
		}
		return machineAcceptanceGateFailure(gate, publicationGateFail, canonicalPathErr.Error())
	}

	content, tracked, err := publicationTaskAuthorityAt(repoRoot, candidate.BaseHead, taskPath)
	if err != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, err.Error())
	}
	if !tracked {
		declared, probeErr := savedTaskDeclaresMachineAcceptance(st, taskID)
		if probeErr != nil {
			return machineAcceptanceGateFailure(gate, publicationGateFail, probeErr.Error())
		}
		if !declared {
			return gate
		}
		if canonicalPathErr != nil {
			return machineAcceptanceGateFailure(gate, publicationGateFail, canonicalPathErr.Error())
		}
		content, err = publicationTaskAuthorityContent(repoRoot, st, candidate, taskID, taskPath)
		if err != nil {
			return machineAcceptanceGateFailure(gate, publicationGateFail, err.Error())
		}
	}

	contract, err := taskcontract.ParseMachineAcceptance(content)
	if err != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, err.Error())
	}
	if !contract.Present {
		return gate
	}
	if canonicalPathErr != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, canonicalPathErr.Error())
	}
	if candidate.TaskID != taskID {
		return machineAcceptanceGateFailure(gate, publicationGateFail,
			fmt.Sprintf("publication candidate task %s does not match current task %s", candidate.TaskID, taskID))
	}

	gate.Required = true
	registry, err := failurepathadvisory.LoadRegistry(st.Path(failurepathadvisory.RegistryFile))
	if err != nil {
		return machineAcceptanceGateFailure(gate, publicationGateFail, "machine acceptance evidence is unreadable: "+err.Error())
	}
	rounds, err := st.ReadRoundRecords(taskID)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return machineAcceptanceGateFailure(gate, publicationGateFail, "machine acceptance round evidence is unreadable: "+err.Error())
	}
	for _, requirement := range contract.Requirements {
		if machineAcceptanceRequirementSatisfied(registry, rounds, taskID, candidate.SnapshotID, requirement) {
			continue
		}
		return machineAcceptanceGateFailure(gate, publicationGateMissing,
			fmt.Sprintf("requirement %s (%s) is unproven for current task %s at snapshot %s", requirement.ID, requirement.Fact, taskID, candidate.SnapshotID))
	}
	return gate
}

func machineAcceptanceGateFailure(gate publicationGateProjection, status, reason string) publicationGateProjection {
	gate.Required = true
	gate.Status = status
	gate.Reason = compactFinalizationDiagnostic(reason)
	return gate
}

func savedTaskDeclaresMachineAcceptance(st *state.StateStore, taskID string) (bool, error) {
	content, err := os.ReadFile(st.TaskAuthorityContentPath(taskID))
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read saved task authority: %w", err)
	}
	return bytes.Contains(content, []byte(taskcontract.MachineAcceptanceHeading)), nil
}

func publicationTaskAuthorityAt(repoRoot, head, taskPath string) ([]byte, bool, error) {
	tracked, err := publicationTaskTrackedAt(repoRoot, head, taskPath)
	if err != nil || !tracked {
		return nil, tracked, err
	}
	content, err := gitFinalizationOutput(repoRoot, "show", head+":"+taskPath)
	if err != nil {
		return nil, false, fmt.Errorf("read task authority %s at %s: %w", taskPath, head, err)
	}
	return []byte(content), true, nil
}

func publicationTaskAuthorityContent(
	repoRoot string,
	st *state.StateStore,
	candidate state.PublicationCandidate,
	taskID string,
	taskPath string,
) ([]byte, error) {
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
	content, tracked, err := publicationTaskAuthorityAt(repoRoot, lineage.BaseHead, taskPath)
	if err != nil {
		return nil, err
	}
	if !tracked {
		return nil, fmt.Errorf("reopen lineage base %s does not contain task authority %s", lineage.BaseHead, taskPath)
	}
	return content, nil
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
	rounds []state.RoundRecord,
	taskID string,
	snapshotID string,
	requirement taskcontract.MachineAcceptanceRequirement,
) bool {
	for _, record := range registry.Records {
		if !machineAcceptanceRecordMatchesSnapshot(record, rounds, taskID, snapshotID) {
			continue
		}
		switch requirement.Fact {
		case taskcontract.MachineFactFailurePathAdvisoryObserved:
			return true
		case taskcontract.MachineFactFailurePathAdvisoryShown:
			if record.Advisory != nil && record.Advisory.Status == failurepathadvisory.AdvisoryShown && record.Advisory.FindingsShown > 0 {
				return true
			}
		}
	}
	return false
}

func machineAcceptanceRecordMatchesSnapshot(
	record failurepathadvisory.Record,
	rounds []state.RoundRecord,
	taskID string,
	snapshotID string,
) bool {
	if record.TaskID != taskID || record.ReviewNumber <= 0 || record.Outcome != failurepathadvisory.OutcomeObserved || strings.TrimSpace(record.CallID) == "" {
		return false
	}
	for _, round := range rounds {
		if round.TaskID != taskID || round.ReviewNumber != record.ReviewNumber || round.CaptureError != "" {
			continue
		}
		roundSnapshotID := state.ValidationSnapshotID(round.Snapshot.Head, round.Snapshot.IndexDigest, round.Snapshot.WorktreeDigest)
		if roundSnapshotID != "" && roundSnapshotID == snapshotID {
			return true
		}
	}
	return false
}
