package repositoryprojecthead

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojectcommit"
)

func validatePreparedPlan(root, head string, prepared repositoryproject.FinalHeadPlan) error {
	start := 0
	if prepared.ActiveTask != "" {
		if err := validateActiveTask(root, head, prepared.ActiveTask); err != nil {
			return err
		}
		start = 1
	}
	for _, path := range prepared.Tasks[start:] {
		if err := validateTask(root, head, path); err != nil {
			return err
		}
	}
	entries, err := repositoryprojectcommit.TaskCorpusEntries(root, head)
	if err != nil {
		return err
	}
	return repositoryproject.ValidateClosure(prepared.Schedule, entries, "HEADのPlanとIMPLEMENTATION_TASKS corpusのclosureが成立しません")
}

func validateActiveTask(root, head, path string) error {
	if err := validateTask(root, head, path); err != nil {
		return err
	}
	content, err := gitOutput(root, "show", head+":"+path)
	if err != nil {
		return fmt.Errorf("HEADのACTIVE task contract %sを読めません: %w", path, err)
	}
	if err := repositoryproject.ValidateActiveTaskContent([]byte(content)); err != nil {
		return fmt.Errorf("HEADのACTIVE task contract %sを受理できません: %w", path, err)
	}
	return nil
}

func validateTask(root, head, path string) error {
	entry, err := gitOutput(root, "ls-tree", head, "--", path)
	if err != nil {
		return fmt.Errorf("HEADのtask file %sを確認できません: %w", path, err)
	}
	fields := strings.Fields(entry)
	if len(fields) < 2 || (fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" {
		return fmt.Errorf("HEADのplanが参照するtask file %s がHEAD treeへregular fileとして存在しません", path)
	}
	return nil
}
