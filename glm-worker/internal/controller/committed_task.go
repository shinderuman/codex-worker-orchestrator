package controller

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryprojectcommit"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type TaskAuthority struct {
	SourceRevision    *ParentSourceRevision
	Task              SemanticTaskRef
	ProjectSnapshotID string
	Snapshot          ProjectSnapshot
	ExecutionMode     string
}

func ResolveCommittedTaskAuthority(repoRoot string) (TaskAuthority, error) {
	repository, err := ResolveRepositoryIdentity(repoRoot)
	if err != nil {
		return TaskAuthority{}, err
	}
	head, err := gitTrimmed(repoRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return TaskAuthority{}, fmt.Errorf("resolve committed repository HEAD: %w", err)
	}
	if head == "" {
		return TaskAuthority{}, fmt.Errorf("committed repository HEAD is empty")
	}
	return resolveCommittedTaskAuthority(repoRoot, repository.LineageID, head)
}

func ResolveCommittedTaskAuthorityAt(repoRoot, revision string) (TaskAuthority, error) {
	repository, err := ResolveRepositoryIdentity(repoRoot)
	if err != nil {
		return TaskAuthority{}, err
	}
	head, err := gitTrimmed(repoRoot, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return TaskAuthority{}, err
	}
	return resolveCommittedTaskAuthority(repoRoot, repository.LineageID, head)
}

func resolveCommittedTaskAuthority(repoRoot, repositoryID, head string) (TaskAuthority, error) {
	project, err := repositoryprojectcommit.Load(repoRoot, head)
	if err != nil {
		return TaskAuthority{}, err
	}
	refs := make([]SemanticTaskRef, 0, len(project.Tasks))
	for _, task := range project.Tasks {
		refs = append(refs, SemanticTaskRef{TaskPath: task.Path, ContractDigest: digestBytes(task.Content)})
	}
	root, ok := findTaskRef(refs, project.RootTaskPath)
	if !ok && project.RootTaskPath != "" {
		return TaskAuthority{}, fmt.Errorf("committed root task %s is missing from task corpus", project.RootTaskPath)
	}
	authority, err := committedTaskExecutionMode(project)
	if err != nil {
		return TaskAuthority{}, err
	}
	snapshot := ProjectSnapshot{
		SchemaVersion:       controllerSchemaVersion,
		RepositoryIdentity:  repositoryID,
		HeadOID:             project.Head,
		PlanDigest:          digestBytes(project.Plan),
		TaskCorpusDigest:    digestTaskRefs(refs),
		SemanticGraphDigest: digestDependencies(project.Dependencies()),
		ScheduleDigest:      digestSchedule(project.Schedule),
		Active:              append([]string(nil), project.Schedule.Active...),
		Next:                append([]string(nil), project.Schedule.Next...),
		Blocked:             append([]string(nil), project.Schedule.Blocked...),
		Tasks:               append([]SemanticTaskRef(nil), refs...),
	}
	snapshot.SnapshotID = projectSnapshotID(snapshot)
	return TaskAuthority{Task: root, ProjectSnapshotID: snapshot.SnapshotID, Snapshot: snapshot, ExecutionMode: authority.Mode}, nil
}

func committedTaskExecutionMode(project repositoryprojectcommit.Project) (taskcontract.ExecutionAuthority, error) {
	if project.RootTaskPath == "" {
		return taskcontract.ExecutionAuthority{}, nil
	}
	for _, task := range project.Tasks {
		if task.Path != project.RootTaskPath {
			continue
		}
		authority, err := taskcontract.ParseExecutionAuthority(task.Content)
		if err != nil {
			return taskcontract.ExecutionAuthority{}, fmt.Errorf("committed root task %s: %w", task.Path, err)
		}
		return authority, nil
	}
	return taskcontract.ExecutionAuthority{}, fmt.Errorf("committed root task %s is missing from task corpus", project.RootTaskPath)
}

func (s *Store) committedTaskExecutionMode(path string) (string, error) {
	project, err := repositoryprojectcommit.Load(s.identity.PrimaryRoot, "HEAD")
	if err != nil {
		return "", err
	}
	for _, task := range project.Tasks {
		if task.Path != path {
			continue
		}
		authority, err := taskcontract.ParseExecutionAuthority(task.Content)
		if err != nil {
			return "", fmt.Errorf("committed task %s: %w", path, err)
		}
		return authority.Mode, nil
	}
	return "", fmt.Errorf("committed task %s is missing from the corpus at HEAD", path)
}

func findTaskRef(refs []SemanticTaskRef, path string) (SemanticTaskRef, bool) {
	for _, ref := range refs {
		if ref.TaskPath == path {
			return ref, true
		}
	}
	return SemanticTaskRef{}, false
}

func digestTaskRefs(refs []SemanticTaskRef) string {
	parts := []string{"controller-task-corpus-v1"}
	for _, ref := range refs {
		parts = append(parts, ref.Identity(), ref.ContractDigest)
	}
	return digestStrings(parts...)
}

func digestDependencies(dependencies []repositoryprojectcommit.Dependency) string {
	copied := append([]repositoryprojectcommit.Dependency(nil), dependencies...)
	sort.Slice(copied, func(i, j int) bool { return copied[i].Task < copied[j].Task })
	parts := []string{"controller-semantic-graph-v1"}
	for _, dependency := range copied {
		outstanding := append([]string(nil), dependency.Outstanding...)
		fulfilled := append([]string(nil), dependency.Fulfilled...)
		sort.Strings(outstanding)
		sort.Strings(fulfilled)
		parts = append(parts, dependency.Task, "outstanding")
		parts = append(parts, outstanding...)
		parts = append(parts, "fulfilled")
		parts = append(parts, fulfilled...)
	}
	return digestStrings(parts...)
}

func digestSchedule(schedule taskcontract.PlanSchedule) string {
	parts := []string{"controller-schedule-v1", "ACTIVE"}
	parts = append(parts, schedule.Active...)
	parts = append(parts, "NEXT")
	parts = append(parts, schedule.Next...)
	parts = append(parts, "BLOCKED")
	parts = append(parts, schedule.Blocked...)
	return digestStrings(parts...)
}

func projectSnapshotID(snapshot ProjectSnapshot) string {
	parts := []string{
		"controller-project-snapshot-v3",
		snapshot.RepositoryIdentity,
		snapshot.HeadOID,
		snapshot.PlanDigest,
		snapshot.TaskCorpusDigest,
		snapshot.SemanticGraphDigest,
		snapshot.ScheduleDigest,
		"ACTIVE",
	}
	parts = append(parts, snapshot.Active...)
	parts = append(parts, "NEXT")
	parts = append(parts, snapshot.Next...)
	parts = append(parts, "BLOCKED")
	parts = append(parts, snapshot.Blocked...)
	if snapshot.UserRequest != nil {
		parts = append(parts, "REQUEST", snapshot.UserRequest.RequestID, digestBytes([]byte(snapshot.UserRequest.Content)))
	}
	if snapshot.CompletedRequest != nil {
		parts = append(parts, "COMPLETED-REQUEST", snapshot.CompletedRequest.RequestID, digestBytes([]byte(snapshot.CompletedRequest.Content)))
	}
	parts = append(parts, "TASKS")
	for _, ref := range snapshot.Tasks {
		parts = append(parts, ref.Identity(), ref.ContractDigest)
	}
	return digestStrings(parts...)
}

func (s *Store) writeProjectSnapshot(snapshot ProjectSnapshot) error {
	if snapshot.SchemaVersion != controllerSchemaVersion || snapshot.RepositoryIdentity != s.identity.LineageID {
		return fmt.Errorf("project snapshot identity is invalid")
	}
	if err := validateRequestTaskSnapshot(snapshot); err != nil {
		return err
	}
	if snapshot.SnapshotID == "" || snapshot.SnapshotID != projectSnapshotID(snapshot) {
		return fmt.Errorf("project snapshot digest is invalid")
	}
	dir := filepath.Join(s.dir, "project-snapshots")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create project snapshot store: %w", err)
	}
	path := filepath.Join(dir, snapshot.SnapshotID+".json")
	if _, err := os.Stat(path); err == nil {
		_, err := s.LoadProjectSnapshot(snapshot.SnapshotID)
		return err
	} else if !os.IsNotExist(err) {
		return err
	}
	return writeJSONAtomic(path, snapshot)
}

func (s *Store) LoadProjectSnapshot(id string) (ProjectSnapshot, error) {
	var snapshot ProjectSnapshot
	if err := readJSON(filepath.Join(s.dir, "project-snapshots", id+".json"), &snapshot); err != nil {
		return ProjectSnapshot{}, fmt.Errorf("read project snapshot %s: %w", id, err)
	}
	if snapshot.SchemaVersion != controllerSchemaVersion || snapshot.RepositoryIdentity != s.identity.LineageID || snapshot.SnapshotID != id || projectSnapshotID(snapshot) != id {
		return ProjectSnapshot{}, fmt.Errorf("project snapshot %s identity is invalid", id)
	}
	if err := validateRequestTaskSnapshot(snapshot); err != nil {
		return ProjectSnapshot{}, err
	}
	if err := s.restoreRegisteredProjectSource(snapshot); err != nil {
		return ProjectSnapshot{}, err
	}
	return snapshot, nil
}

func (s *Store) restoreRegisteredProjectSource(snapshot ProjectSnapshot) error {
	head, err := s.LoadHead()
	if err != nil || head.ProjectSnapshotID != snapshot.SnapshotID || snapshot.HeadOID == head.IntegrationTip {
		return err
	}
	attemptID, err := s.registeredSourceAttemptID(head)
	if err != nil || attemptID == "" {
		return err
	}
	attempt, err := s.loadAttempt(attemptID)
	if err != nil || attempt.SourceRevision == nil || attempt.SourceRevision.AuthorityOID != snapshot.HeadOID {
		return err
	}
	if _, err := gitTrimmed(s.identity.PrimaryRoot, "cat-file", "-t", snapshot.HeadOID); err == nil {
		return nil
	}
	return s.restoreExecutionBaseline(s.identity.PrimaryRoot, attempt)
}

func (s *Store) registeredSourceAttemptID(head RepositoryControllerHead) (string, error) {
	if head.LiveAttemptID != "" {
		return head.LiveAttemptID, nil
	}
	if head.AcceptedCandidateRef == nil {
		return "", nil
	}
	candidate, err := s.loadCandidateArchive(*head.AcceptedCandidateRef)
	if err != nil {
		return "", err
	}
	if candidate.ProjectSnapshotID != head.ProjectSnapshotID {
		return "", fmt.Errorf("registered source candidate project differs from controller")
	}
	return candidate.AttemptID, nil
}

func readCommittedObject(repoRoot, revision, path string) ([]byte, error) {
	command := exec.Command("git", "-C", repoRoot, "show", revision+":"+filepath.ToSlash(path))
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}
