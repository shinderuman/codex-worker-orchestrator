package controller

import (
	"fmt"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
)

func WorkflowConfig(cfg config.AppConfig) (config.AppConfig, error) {
	present, err := RepositoryPresent(cfg.RepoRoot)
	if err != nil || !present {
		return cfg, err
	}
	cfg.RepositoryLockPath, err = WorkflowLockPath(cfg)
	if err != nil {
		return cfg, err
	}
	exists, err := Exists(cfg)
	if err != nil {
		return cfg, err
	}
	task, err := workflowTaskRef(cfg, exists)
	if err != nil {
		return cfg, err
	}
	if task.Empty() {
		authority, err := ResolveCommittedTaskAuthority(cfg.RepoRoot)
		if err != nil {
			if exists {
				return cfg, err
			}
			return cfg, nil
		}
		task = authority.Task
	}
	identity, err := ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return cfg, err
	}
	cfg.RepoHash = digestStrings(identity.LineageID, task.TaskPath)
	cfg.RepoShort = cfg.RepoHash[:12]
	return cfg, nil
}

func workflowTaskRef(cfg config.AppConfig, exists bool) (SemanticTaskRef, error) {
	if !exists {
		return SemanticTaskRef{}, nil
	}
	store, err := Open(cfg)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return SemanticTaskRef{}, err
	}
	if head.ExecutionTaskRef != nil {
		return *head.ExecutionTaskRef, nil
	}
	if head.RootTaskRef != nil {
		return *head.RootTaskRef, nil
	}
	return SemanticTaskRef{}, nil
}

func WorkflowLockPath(cfg config.AppConfig) (string, error) {
	identity, err := ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return "", err
	}
	return filepath.Clean(controllerStoreDir(cfg, identity)) + ".workflow.lock", nil
}

func (s *Store) ExecutionTaskForWorkspace(root string) (SemanticTaskRef, error) {
	head, err := s.LoadHead()
	if err != nil {
		return SemanticTaskRef{}, err
	}
	workspace, err := ResolveWorkspaceIdentity(root, s.identity)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(root)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	authority, err := MutationAuthorityFromHead(head)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	admission, err := s.AdmitMutation(authority, workspace, snapshot)
	if err != nil {
		return SemanticTaskRef{}, fmt.Errorf("resolve controller execution task: %w", err)
	}
	return admission.Attempt.SemanticTaskRef, nil
}

func WorkflowExecutionTask(cfg config.AppConfig) (SemanticTaskRef, bool, error) {
	present, err := RepositoryPresent(cfg.RepoRoot)
	if err != nil || !present {
		return SemanticTaskRef{}, false, err
	}
	exists, err := Exists(cfg)
	if err != nil || !exists {
		return SemanticTaskRef{}, false, err
	}
	store, err := Open(cfg)
	if err != nil {
		return SemanticTaskRef{}, true, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return SemanticTaskRef{}, true, err
	}
	if IsPristine(head) {
		return SemanticTaskRef{}, false, nil
	}
	if head.ExecutionTaskRef == nil {
		return SemanticTaskRef{}, true, fmt.Errorf("canonical controller has no execution task")
	}
	task, err := store.ExecutionTaskForWorkspace(cfg.RepoRoot)
	return task, true, err
}
