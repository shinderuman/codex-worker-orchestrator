package controller

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type CommittedTaskAuthority struct {
	Task              SemanticTaskRef
	ProjectSnapshotID string
}

func ResolveCommittedTaskAuthority(repoRoot string) (CommittedTaskAuthority, error) {
	head, err := gitTrimmed(repoRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || head == "" {
		return CommittedTaskAuthority{}, fmt.Errorf("resolve committed repository HEAD: %w", err)
	}
	plan, err := readCommittedObject(repoRoot, head, "IMPLEMENTATION_PLAN.local.md")
	if err != nil {
		return CommittedTaskAuthority{}, fmt.Errorf("read committed implementation plan: %w", err)
	}
	schedule, err := taskcontract.ParsePlanSchedule(string(plan))
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	path, err := schedule.ActiveTask()
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	path = filepath.ToSlash(strings.TrimSpace(path))
	if path == "" {
		return CommittedTaskAuthority{}, fmt.Errorf("committed implementation plan has no active task")
	}
	if err := taskcontract.ValidateActiveTaskPath(path); err != nil {
		return CommittedTaskAuthority{}, err
	}
	taskContent, err := readCommittedObject(repoRoot, head, path)
	if err != nil {
		return CommittedTaskAuthority{}, fmt.Errorf("read committed semantic task %s: %w", path, err)
	}
	task := SemanticTaskRef{TaskPath: path, ContractDigest: digestBytes(taskContent)}
	return CommittedTaskAuthority{
		Task: task,
		ProjectSnapshotID: digestStrings(
			"controller-project-snapshot-v1",
			head,
			digestBytes(plan),
			path,
			task.ContractDigest,
		),
	}, nil
}

func readCommittedObject(repoRoot, revision, path string) ([]byte, error) {
	command := exec.Command("git", "-C", repoRoot, "show", revision+":"+filepath.ToSlash(path))
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}
