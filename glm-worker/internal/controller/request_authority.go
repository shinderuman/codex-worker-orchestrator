package controller

import (
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/config"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryharness"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/state"
)

func ActivateRequest(cfg config.AppConfig, request string) (Admission, error) {
	decision, err := repositoryharness.Evaluate(cfg.RepoRoot)
	if err != nil {
		return Admission{}, err
	}
	if decision.Active {
		return Activate(cfg)
	}
	if err := RequireRequestRepository(cfg.RepoRoot); err != nil {
		return Admission{}, err
	}
	if strings.TrimSpace(request) == "" || !utf8.ValidString(request) {
		return Admission{}, fmt.Errorf("request authority requires a nonempty request")
	}
	store, err := Open(cfg)
	if err != nil {
		return Admission{}, err
	}
	return store.activateRequest(request)
}

func (s *Store) activateRequest(request string) (Admission, error) {
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if head.LiveLeaseID != "" {
		if head.ExecutionTaskRef == nil || head.ExecutionTaskRef.RequestID == "" {
			return Admission{}, fmt.Errorf("existing project authority cannot become request authority")
		}
		if err := s.ValidateRequestTask(*head.ExecutionTaskRef, request); err != nil {
			return Admission{}, err
		}
		return Activate(s.config)
	}
	workspace, err := ResolveWorkspaceIdentity(s.config.RepoRoot, s.Identity())
	if err != nil {
		return Admission{}, err
	}
	snapshot, err := CaptureWorkspaceSnapshot(s.config.RepoRoot)
	if err != nil {
		return Admission{}, err
	}
	return s.bootstrapRequestExecution(request, workspace, snapshot)
}

func RequireRequestRepository(root string) error {
	decision, err := repositoryharness.Evaluate(root)
	if err != nil {
		return err
	}
	if decision.Active || decision.Reason != repositoryharness.ReasonAbsent {
		return fmt.Errorf("request authority requires an absent repository harness marker: %s", decision.Reason)
	}
	tracked, err := repositoryharness.MarkerTracked(root)
	if err != nil {
		return err
	}
	if tracked {
		return fmt.Errorf("request authority refuses a missing tracked repository harness marker")
	}
	return nil
}

func (s *Store) bootstrapRequestExecution(request string, workspace WorkspaceIdentity, snapshot WorkspaceSnapshot) (Admission, error) {
	lock, err := s.acquireMutationLock()
	if err != nil {
		return Admission{}, err
	}
	defer func() { _ = lock.Close() }()
	head, err := s.LoadHead()
	if err != nil {
		return Admission{}, err
	}
	if !IsPristine(head) {
		if err := s.requireCompletedRequest(head); err != nil {
			return Admission{}, err
		}
	}
	if err := s.validateBootstrapWorkspace(workspace); err != nil {
		return Admission{}, err
	}
	if err := RequireRequestRepository(workspace.Root); err != nil {
		return Admission{}, err
	}
	id, err := state.NewUUID()
	if err != nil {
		return Admission{}, err
	}
	task := SemanticTaskRef{RequestID: id, ContractDigest: digestBytes([]byte(request))}
	project := ProjectSnapshot{SchemaVersion: controllerSchemaVersion, RepositoryIdentity: s.identity.LineageID, HeadOID: snapshot.Head, Tasks: []SemanticTaskRef{task}, UserRequest: &RequestTaskAuthority{RequestID: id, Content: request}}
	project.TaskCorpusDigest = digestTaskRefs(project.Tasks)
	project.SnapshotID = projectSnapshotID(project)
	authority := TaskAuthority{Task: task, ProjectSnapshotID: project.SnapshotID, Snapshot: project}
	return s.bootstrapFreshExecution(head, authority, workspace, snapshot)
}

func validateRequestTaskSnapshot(snapshot ProjectSnapshot) error {
	if snapshot.UserRequest != nil && snapshot.CompletedRequest != nil {
		return fmt.Errorf("request authority variants conflict")
	}
	request := snapshot.UserRequest
	if snapshot.CompletedRequest != nil {
		request = snapshot.CompletedRequest
	}
	if request == nil {
		for _, task := range snapshot.Tasks {
			if task.RequestID != "" {
				return fmt.Errorf("request task has no canonical request authority")
			}
		}
		return nil
	}
	if !state.ValidGeneratedUUID(request.RequestID) || strings.TrimSpace(request.Content) == "" {
		return fmt.Errorf("request authority identity or content is invalid")
	}
	if requestSnapshotHasProjectAuthority(snapshot) {
		return fmt.Errorf("request authority cannot contain project schedule authority")
	}
	return validateRequestSnapshotTasks(snapshot, request)
}

func validateRequestSnapshotTasks(snapshot ProjectSnapshot, request *RequestTaskAuthority) error {
	if snapshot.CompletedRequest != nil {
		if len(snapshot.Tasks) != 0 {
			return fmt.Errorf("completed request retains selected Task authority")
		}
		return nil
	}
	if len(snapshot.Tasks) != 1 {
		return fmt.Errorf("active request must have exactly one Task authority")
	}
	task := snapshot.Tasks[0]
	if task.TaskPath != "" || task.RequestID != request.RequestID || task.ContractDigest != digestBytes([]byte(request.Content)) {
		return fmt.Errorf("canonical request body and task identity differ")
	}
	return nil
}

func requestSnapshotHasProjectAuthority(snapshot ProjectSnapshot) bool {
	return snapshot.PlanDigest != "" || snapshot.SemanticGraphDigest != "" || snapshot.ScheduleDigest != "" || len(snapshot.Active)+len(snapshot.Next)+len(snapshot.Blocked) != 0
}

func (s *Store) executionTaskAuthority(head RepositoryControllerHead, task SemanticTaskRef) (*RequestTaskAuthority, error) {
	project, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return nil, err
	}
	if project.UserRequest == nil {
		if task.RequestID != "" {
			return nil, fmt.Errorf("request task has project-only authority")
		}
		return nil, nil
	}
	if len(project.Tasks) != 1 || !project.Tasks[0].Equal(task) {
		return nil, fmt.Errorf("request task does not match canonical project authority")
	}
	if err := RequireRequestRepository(s.identity.PrimaryRoot); err != nil {
		return nil, err
	}
	return project.UserRequest, nil
}

func (s *Store) requestTaskAuthority(task SemanticTaskRef) (*RequestTaskAuthority, error) {
	head, err := s.LoadHead()
	if err != nil {
		return nil, err
	}
	request, err := s.executionTaskAuthority(head, task)
	if err != nil {
		return nil, err
	}
	if request == nil {
		return nil, fmt.Errorf("task is not request-backed")
	}
	return request, nil
}

func (s *Store) ValidateRequestTask(task SemanticTaskRef, request string) error {
	authority, err := s.requestTaskAuthority(task)
	if err != nil {
		return err
	}
	if authority.Content != request {
		return fmt.Errorf("runtime request differs from canonical request authority")
	}
	return nil
}

func (s *Store) requestAdvancementProject(head RepositoryControllerHead, tip string) (ProjectSnapshot, error) {
	if head.RootTaskRef == nil || head.ExecutionTaskRef == nil ||
		head.RootTaskRef.RequestID == "" || !head.RootTaskRef.Equal(*head.ExecutionTaskRef) {
		return ProjectSnapshot{}, fmt.Errorf("request advancement requires held request Task authority")
	}
	if err := requireRequestAdvancementRepository(s.identity.PrimaryRoot, head.IntegrationTip, tip); err != nil {
		return ProjectSnapshot{}, err
	}
	source, err := s.LoadProjectSnapshot(head.ProjectSnapshotID)
	if err != nil {
		return ProjectSnapshot{}, err
	}
	if source.HeadOID != head.IntegrationTip || source.UserRequest == nil ||
		len(source.Tasks) != 1 || !source.Tasks[0].Equal(*head.ExecutionTaskRef) ||
		source.TaskCorpusDigest != digestTaskRefs(source.Tasks) {
		return ProjectSnapshot{}, fmt.Errorf("request advancement differs from canonical request authority")
	}
	next := source
	next.HeadOID = tip
	next.SnapshotID = projectSnapshotID(next)
	return next, nil
}

func requireRequestAdvancementRepository(repo, base, tip string) error {
	descendant, err := isPublicationDescendant(repo, base, tip)
	if err != nil {
		return err
	}
	if !descendant {
		return fmt.Errorf("request advancement is not a fast-forward of the controller integration authority")
	}
	if err := RequireRequestRepository(repo); err != nil {
		return err
	}
	marker, err := runGitBinary(repo, nil, "ls-tree", "--name-only", tip, "--", repositoryharness.MarkerPath)
	if err != nil {
		return err
	}
	if len(marker) != 0 {
		return fmt.Errorf("request advancement introduces a repository harness marker")
	}
	return nil
}
