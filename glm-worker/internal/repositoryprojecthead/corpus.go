package repositoryprojecthead

import (
	"fmt"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
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
	entries, err := taskCorpusEntries(root, head)
	if err != nil {
		return err
	}
	return repositoryproject.ValidateClosure(prepared.Schedule, entries, "HEADのPlanとIMPLEMENTATION_TASKS corpusのclosureが成立しません")
}

func taskCorpusEntries(root, head string) ([]taskcontract.TaskCorpusEntry, error) {
	output, err := gitOutput(root, "ls-tree", "-r", "-t", "-z", head, "--", taskcontract.TasksDir)
	if err != nil {
		return nil, fmt.Errorf("HEADのtask corpusを列挙できません: %w", err)
	}
	var entries []taskcontract.TaskCorpusEntry
	for _, record := range strings.Split(output, "\x00") {
		if record == "" {
			continue
		}
		entry, ok, err := parseTaskCorpusRecord(record)
		if err != nil {
			return nil, err
		}
		if ok {
			entries = append(entries, entry)
		}
	}
	return entries, nil
}

func parseTaskCorpusRecord(record string) (taskcontract.TaskCorpusEntry, bool, error) {
	metadata, path, found := strings.Cut(record, "\t")
	if !found {
		return taskcontract.TaskCorpusEntry{}, false, fmt.Errorf("HEADのtask corpus entry %qを読み込めません", record)
	}
	if !strings.HasSuffix(path, ".md") {
		return taskcontract.TaskCorpusEntry{}, false, nil
	}
	fields := strings.Fields(metadata)
	if len(fields) < 2 {
		return taskcontract.TaskCorpusEntry{}, false, fmt.Errorf("HEADのtask corpus entry %qを読み込めません", record)
	}
	regularBlob := (fields[0] == "100644" || fields[0] == "100755") && fields[1] == "blob"
	return taskcontract.TaskCorpusEntry{Path: path, Regular: regularBlob}, true, nil
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
