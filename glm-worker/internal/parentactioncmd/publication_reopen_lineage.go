package parentactioncmd

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func verifyCompletionReopenLineageIdentity(lineage state.PublicationReopenLineage, lifecycleTask, taskID string) error {
	if lineage.TaskID != taskID {
		return fmt.Errorf("publication reopen lineage task identity %s does not match current task identity %s", lineage.TaskID, taskID)
	}
	if lineage.TaskPath != lifecycleTask {
		return fmt.Errorf("publication reopen lineage task path %s does not match lifecycle task %s", lineage.TaskPath, lifecycleTask)
	}
	return nil
}

func verifyCompletionReopenLineageTransition(repoRoot string, lineage state.PublicationReopenLineage, lifecycleTask string) error {
	parents, err := gitFinalizationOutput(repoRoot, "rev-list", "--parents", "-n", "1", lineage.CommitOID)
	if err != nil {
		return fmt.Errorf("publication reopen lineage commit %s cannot be read: %w", lineage.CommitOID, err)
	}
	parentFields := strings.Fields(parents)
	if len(parentFields) != 2 || parentFields[0] != lineage.CommitOID || parentFields[1] != lineage.BaseHead {
		return fmt.Errorf("publication reopen lineage commit %s is not directly based on %s", lineage.CommitOID, lineage.BaseHead)
	}
	lineageBaseEntry, err := gitFinalizationOutput(repoRoot, "ls-tree", lineage.BaseHead, "--", lifecycleTask)
	if err != nil {
		return fmt.Errorf("lifecycle task %s cannot be read from reopen lineage base: %w", lifecycleTask, err)
	}
	if strings.TrimSpace(lineageBaseEntry) == "" {
		return fmt.Errorf("lifecycle task %s was not tracked at reopen lineage base", lifecycleTask)
	}
	lineageCommitEntry, err := gitFinalizationOutput(repoRoot, "ls-tree", lineage.CommitOID, "--", lifecycleTask)
	if err != nil {
		return fmt.Errorf("lifecycle task %s cannot be read from reopen lineage commit: %w", lifecycleTask, err)
	}
	if strings.TrimSpace(lineageCommitEntry) != "" {
		return fmt.Errorf("lifecycle task %s remains tracked in reopen lineage commit", lifecycleTask)
	}
	return nil
}
