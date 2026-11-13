package controller

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

func RegisteredTask(repo string) (SemanticTaskRef, error) {
	task, _, err := registeredTaskDeclaration(repo)
	return task, err
}

func registeredTaskDeclaration(repo string) (SemanticTaskRef, string, error) {
	plan, err := os.ReadFile(filepath.Join(repo, "IMPLEMENTATION_PLAN.local.md"))
	if err != nil {
		return SemanticTaskRef{}, "", err
	}
	prepared, err := repositoryproject.PrepareProjectState(string(plan))
	if err != nil {
		return SemanticTaskRef{}, "", err
	}
	entries, err := taskcontract.EnumerateTaskCorpus(repo)
	if err != nil {
		return SemanticTaskRef{}, "", err
	}
	if err := repositoryproject.ValidateClosure(prepared.Schedule, entries, "registered Task"); err != nil {
		return SemanticTaskRef{}, "", err
	}
	if len(prepared.Active) == 0 {
		return SemanticTaskRef{}, "", nil
	}
	path := prepared.Active[0]
	content, err := os.ReadFile(filepath.Join(repo, filepath.FromSlash(path)))
	if err != nil {
		return SemanticTaskRef{}, "", err
	}
	if err := repositoryproject.ValidateActiveTaskContent(content); err != nil {
		return SemanticTaskRef{}, "", err
	}
	execution, err := taskcontract.ParseExecutionAuthority(content)
	if err != nil {
		return SemanticTaskRef{}, "", err
	}
	return SemanticTaskRef{TaskPath: path, ContractDigest: digestBytes(content)}, execution.Mode, nil
}

func (s *Store) StartRegisteredTask() (Admission, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if err := s.validateRegisteredTaskStart(head); err != nil {
		return Admission{}, err
	}
	workspace, err := ResolveWorkspaceIdentity(s.identity.PrimaryRoot, s.identity)
	if err != nil {
		return Admission{}, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(workspace.Root)
	if err != nil {
		return Admission{}, err
	}
	if !IsPristine(head) && snapshot.Head != head.IntegrationTip {
		return Admission{}, fmt.Errorf("registered Task source differs from the idle integration tip")
	}
	authority, err := s.captureRegisteredTaskAuthority(snapshot)
	if err != nil {
		return Admission{}, err
	}
	if authority.ExecutionMode == ExecutionAuthorityParentOnly {
		return Admission{}, fmt.Errorf("registered parent-only Task requires parent-only materialization")
	}
	return s.bootstrapFreshExecution(head, authority, workspace, snapshot)
}

func (s *Store) captureRegisteredTaskAuthority(snapshot WorkspaceSnapshot) (TaskAuthority, error) {
	task, err := RegisteredTask(s.identity.PrimaryRoot)
	if err != nil {
		return TaskAuthority{}, err
	}
	if task.Empty() {
		return TaskAuthority{}, fmt.Errorf("new execution requires a registered ACTIVE Task")
	}
	source, authority, err := s.captureParentSourceRevision(snapshot.ID, snapshot.Head)
	if err != nil {
		return TaskAuthority{}, err
	}
	if !authority.Task.Equal(task) {
		return TaskAuthority{}, fmt.Errorf("registered Task authority changed during capture")
	}
	authority.SourceRevision = &source
	return authority, nil
}

func (s *Store) validateRegisteredTaskStart(head RepositoryControllerHead) error {
	if IsPristine(head) {
		authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
		if err != nil {
			return err
		}
		if !authority.Task.Empty() {
			return fmt.Errorf("registered Task start cannot replace committed Task authority")
		}
		return nil
	}
	if !retiredMetadataHead(head) || head.RootTaskRef != nil {
		return fmt.Errorf("registered Task start requires an idle finalized controller")
	}
	authority, err := s.retiredProjectAuthority(head)
	if err != nil {
		return err
	}
	if !authority.Task.Empty() {
		return fmt.Errorf("registered Task start cannot replace committed Task authority")
	}
	if head.PublicationPolicy != nil {
		remote, err := observePublicationRemote(s.identity.PrimaryRoot, *head.PublicationPolicy)
		if err != nil {
			return err
		}
		if remote != head.IntegrationTip {
			return fmt.Errorf("external advancement must be adopted before registered Task start")
		}
	}
	return nil
}
