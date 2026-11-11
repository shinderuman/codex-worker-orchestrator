package controller

import (
	"fmt"
	"path/filepath"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
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
		task, err = workflowBootstrapTask(cfg.RepoRoot, exists)
		if err != nil {
			return cfg, err
		}
		if task.Empty() {
			return cfg, nil
		}
	}
	identity, err := ResolveRepositoryIdentity(cfg.RepoRoot)
	if err != nil {
		return cfg, err
	}
	return WorkflowTaskConfig(cfg, identity, task), nil
}

func WorkflowTaskConfig(cfg config.AppConfig, identity RepositoryIdentity, task SemanticTaskRef) config.AppConfig {
	cfg.RepoHash = digestStrings(identity.LineageID, task.Identity())
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
	task, _, err := store.completedRequestTask(head)
	return task, err
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
	if err == nil {
		return admission.Attempt.SemanticTaskRef, nil
	}
	if task, admitted := s.parentOnlyRevisedExecutionTask(workspace, snapshot); admitted {
		return task, nil
	}
	return SemanticTaskRef{}, fmt.Errorf("resolve controller execution task: %w", err)
}

func (s *Store) parentOnlyRevisedExecutionTask(workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (SemanticTaskRef, bool) {
	live, err := s.LiveParentOnlyExecution()
	if err != nil || !live {
		return SemanticTaskRef{}, false
	}
	admission, err := s.ParentOnlyTaskAuthoritySyncAdmission(s.config)
	if err != nil || admission.Workspace.ID != workspace.ID || admission.Snapshot.ID != snapshot.ID {
		return SemanticTaskRef{}, false
	}
	return admission.Attempt.SemanticTaskRef, true
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
	if task, request, err := store.completedRequestTask(head); request || err != nil {
		return task, true, err
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
	if task, request, err := store.completedRequestTask(head); request || err != nil {
		return task, true, err
	}
	if _, err := store.retiredProjectAuthority(head); err != nil {
		return SemanticTaskRef{}, true, err
	}
	authority, err := ResolveCommittedTaskAuthority(cfg.RepoRoot)
	if err != nil {
		return SemanticTaskRef{}, true, err
	}
	descendant, err := isPublicationDescendant(cfg.RepoRoot, head.IntegrationTip, authority.Snapshot.HeadOID)
	if err != nil {
		return SemanticTaskRef{}, true, fmt.Errorf("inspect retired read authority history: %w", err)
	}
	if !descendant {
		return SemanticTaskRef{}, true, fmt.Errorf("retired read authority is outside integration history")
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
		return "", s.requestCompletionHasNoSuccessor(head)
	}
	if head.ActiveEpisodeID != "" {
		parentOnly, err := s.quiescentParentOnlyRootResume(head)
		if err != nil {
			return "", err
		}
		if parentOnly {
			return "", nil
		}
	}
	cfg := WorkflowTaskConfig(s.config, s.identity, *head.RootTaskRef)
	cfg.RepoRoot = s.identity.PrimaryRoot
	runtime := state.AttachStateStore(cfg)
	admitted, err := quiescentRuntimeReady(runtime)
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

func (s *Store) quiescentParentOnlyRootResume(head RepositoryControllerHead) (bool, error) {
	authority, err := ResolveCommittedTaskAuthority(s.identity.PrimaryRoot)
	if err != nil {
		return false, err
	}
	if authority.ExecutionMode != ExecutionAuthorityParentOnly {
		return false, nil
	}
	if head.MetadataLineageRef == nil || head.ActiveEpisodeRevision == 0 ||
		head.ExecutionTaskRef != nil || head.LiveAttemptID != "" || head.LiveLeaseID != "" {
		return false, fmt.Errorf("parent-only Root successor requires a completed child metadata target")
	}
	if !head.RootTaskRef.Equal(authority.Task) {
		return false, fmt.Errorf("parent-only Root resume successor authority differs")
	}
	if err := s.rejectBoundTaskRuntime(authority.Task); err != nil {
		return false, err
	}
	return true, nil
}

func quiescentRuntimeReady(runtime *state.StateStore) (bool, error) {
	_, admitted, err := runtime.AdmitNewTask()
	if err != nil || !admitted {
		return false, err
	}
	switch runtime.TaskStatus() {
	case state.TaskStatusNone:
		return !runtime.Exists("task.id"), nil
	case state.TaskStatusComplete, state.TaskStatusUserTerminated:
		return true, nil
	default:
		return false, nil
	}
}

func workflowBootstrapTask(root string, controllerExists bool) (SemanticTaskRef, error) {
	decision, err := repositoryharness.Evaluate(root)
	if err != nil {
		return SemanticTaskRef{}, err
	}
	if !decision.Active {
		return SemanticTaskRef{}, nil
	}
	authority, err := ResolveCommittedTaskAuthority(root)
	if err != nil {
		if controllerExists {
			return SemanticTaskRef{}, err
		}
		return SemanticTaskRef{}, nil
	}
	return authority.Task, nil
}
