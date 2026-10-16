package controller

import (
	"fmt"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
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
	return WorkflowTaskConfig(cfg, identity, task), nil
}

func WorkflowTaskConfig(cfg config.AppConfig, identity RepositoryIdentity, task SemanticTaskRef) config.AppConfig {
	cfg.RepoHash = digestStrings(identity.LineageID, task.TaskPath)
	cfg.RepoShort = cfg.RepoHash[:12]
	return cfg
}

func ParentContinuationConfig(cfg config.AppConfig) (config.AppConfig, error) {
	present, err := RepositoryPresent(cfg.RepoRoot)
	if err != nil || !present {
		return cfg, err
	}
	exists, err := Exists(cfg)
	if err != nil || !exists {
		return cfg, err
	}
	store, err := Open(cfg)
	if err != nil {
		return cfg, err
	}
	root, err := canonicalPath(cfg.RepoRoot)
	if err != nil {
		return cfg, err
	}
	if root != store.Identity().PrimaryRoot {
		return WorkflowConfig(cfg)
	}
	report, err := store.ProjectStatus()
	if err != nil {
		return cfg, err
	}
	if report.Head.LiveAttemptID == "" && report.Head.LiveLeaseID == "" {
		return cfg, nil
	}
	return store.liveParentContinuationConfig(cfg, report)
}

func (s *Store) liveParentContinuationConfig(cfg config.AppConfig, report ControllerStatusReport) (config.AppConfig, error) {
	if report.Live == nil || report.Live.WorkspaceRoot == "" || report.LiveError != "" {
		return cfg, fmt.Errorf("parent continuation live workspace is unavailable: %s", report.LiveError)
	}
	task, err := s.ExecutionTaskForWorkspace(report.Live.WorkspaceRoot)
	if err != nil {
		return cfg, err
	}
	if !task.Equal(report.Live.ExecutionTask) {
		return cfg, fmt.Errorf("parent continuation execution Task authority changed")
	}
	cfg.RepoRoot = report.Live.WorkspaceRoot
	return WorkflowConfig(cfg)
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
	admission, err := s.AdmitMutationSettlingQualityIntent(authority, workspace, snapshot)
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

func WorkflowAuthorityTask(cfg config.AppConfig) (SemanticTaskRef, bool, error) {
	task, canonical, executionErr := WorkflowExecutionTask(cfg)
	if executionErr == nil || !canonical {
		return task, canonical, executionErr
	}
	store, err := Open(cfg)
	if err != nil {
		return SemanticTaskRef{}, true, err
	}
	head, err := store.LoadHead()
	if err != nil {
		return SemanticTaskRef{}, true, err
	}
	return store.quiescentWorkflowAuthority(cfg, head, executionErr)
}

func (store *Store) quiescentWorkflowAuthority(cfg config.AppConfig, head RepositoryControllerHead, executionErr error) (SemanticTaskRef, bool, error) {
	if task, pending, err := store.PreparedCompletionAuthority(head); pending || err != nil {
		return task, true, err
	}
	if head.UserTerminationRef != nil && head.MetadataLineageRef == nil && head.ExecutionTaskRef == nil {
		authority, err := store.UserTerminatedSuccessorAuthority(head)
		return authority.Task, true, err
	}
	if !retiredMetadataHead(head) {
		if task, sealed := sealedAcceptedReadAuthority(store, head); sealed {
			return task, true, nil
		}
		return SemanticTaskRef{}, true, executionErr
	}
	return store.retiredWorkflowAuthority(cfg, head)
}

func (store *Store) retiredWorkflowAuthority(cfg config.AppConfig, head RepositoryControllerHead) (SemanticTaskRef, bool, error) {
	if _, err := store.retiredProjectAuthority(head); err != nil {
		return SemanticTaskRef{}, true, err
	}
	authority, err := ResolveCommittedTaskAuthority(cfg.RepoRoot)
	if err != nil {
		return SemanticTaskRef{}, true, err
	}
	if authority.Task.TaskPath != head.RootTaskRef.TaskPath {
		return SemanticTaskRef{}, true, fmt.Errorf("retired metadata advancement changes selected Task authority")
	}
	if err := validateParentMetadataHistory(cfg.RepoRoot, head.IntegrationTip, authority.Snapshot.HeadOID); err != nil {
		return SemanticTaskRef{}, true, err
	}
	return authority.Task, true, nil
}

func sealedAcceptedReadAuthority(store *Store, head RepositoryControllerHead) (SemanticTaskRef, bool) {
	if head.Status != ControllerStatusActive || head.LiveAttemptID != "" || head.ExecutionTaskRef == nil {
		return SemanticTaskRef{}, false
	}
	if _, err := store.QuiescentCandidateAwaitMaterialization(head); err != nil {
		return SemanticTaskRef{}, false
	}
	return *head.ExecutionTaskRef, true
}

func (s *Store) QuiescentRuntimeAdmission(head RepositoryControllerHead) (string, error) {
	if head.RootTaskRef == nil {
		return "", fmt.Errorf("quiescent Runtime requires selected Task authority")
	}
	cfg := WorkflowTaskConfig(s.config, s.identity, *head.RootTaskRef)
	cfg.RepoRoot = s.identity.PrimaryRoot
	runtime := state.AttachStateStore(cfg)
	_, admitted, err := runtime.AdmitNewTask()
	if err != nil {
		return "", err
	}
	if head.ActiveEpisodeID == "" && !admitted {
		return "", fmt.Errorf("successor Runtime has unfinished parent actions")
	}
	if !runtime.Exists("task.id") {
		return "", nil
	}
	return runtime.TaskID()
}
