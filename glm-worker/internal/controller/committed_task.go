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

type CommittedTaskAuthority struct {
	Task              SemanticTaskRef
	ProjectSnapshotID string
	Snapshot          ProjectSnapshot
}

func ResolveCommittedTaskAuthority(repoRoot string) (CommittedTaskAuthority, error) {
	repository, err := ResolveRepositoryIdentity(repoRoot)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	head, err := gitTrimmed(repoRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return CommittedTaskAuthority{}, fmt.Errorf("resolve committed repository HEAD: %w", err)
	}
	if head == "" {
		return CommittedTaskAuthority{}, fmt.Errorf("committed repository HEAD is empty")
	}
	return resolveCommittedTaskAuthority(repoRoot, repository.LineageID, head)
}

func ResolveCommittedTaskAuthorityAt(repoRoot, revision string) (CommittedTaskAuthority, error) {
	repository, err := ResolveRepositoryIdentity(repoRoot)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	head, err := gitTrimmed(repoRoot, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	return resolveCommittedTaskAuthority(repoRoot, repository.LineageID, head)
}

func resolveCommittedTaskAuthority(repoRoot, repositoryID, head string) (CommittedTaskAuthority, error) {
	project, err := repositoryprojectcommit.Load(repoRoot, head)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	refs := make([]SemanticTaskRef, 0, len(project.Tasks))
	for _, task := range project.Tasks {
		refs = append(refs, SemanticTaskRef{TaskPath: task.Path, ContractDigest: digestBytes(task.Content)})
	}
	root, ok := findTaskRef(refs, project.RootTaskPath)
	if !ok {
		return CommittedTaskAuthority{}, fmt.Errorf("committed root task %s is missing from task corpus", project.RootTaskPath)
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
	return CommittedTaskAuthority{Task: root, ProjectSnapshotID: snapshot.SnapshotID, Snapshot: snapshot}, nil
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
		parts = append(parts, ref.TaskPath, ref.ContractDigest)
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
		"controller-project-snapshot-v2",
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
	parts = append(parts, "TASKS")
	for _, ref := range snapshot.Tasks {
		parts = append(parts, ref.TaskPath, ref.ContractDigest)
	}
	return digestStrings(parts...)
}

func (s *Store) writeProjectSnapshot(snapshot ProjectSnapshot) error {
	if snapshot.SchemaVersion != controllerSchemaVersion || snapshot.RepositoryIdentity != s.identity.LineageID {
		return fmt.Errorf("project snapshot identity is invalid")
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
	return snapshot, nil
}

func readCommittedObject(repoRoot, revision, path string) ([]byte, error) {
	command := exec.Command("git", "-C", repoRoot, "show", revision+":"+filepath.ToSlash(path))
	output, err := command.Output()
	if err != nil {
		return nil, err
	}
	return output, nil
}
