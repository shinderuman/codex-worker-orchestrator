package controller

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
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
	plan, err := readCommittedObject(repoRoot, head, "IMPLEMENTATION_PLAN.local.md")
	if err != nil {
		return CommittedTaskAuthority{}, fmt.Errorf("read committed implementation plan: %w", err)
	}
	schedule := taskcontract.ParsePlanSchedule(string(plan))
	rootPath, err := schedule.ValidateComplete()
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	entries, refs, contents, err := committedTaskCorpus(repoRoot, head)
	if err != nil {
		return CommittedTaskAuthority{}, err
	}
	if err := repositoryproject.ValidateClosure(schedule, entries, "committed Plan/task corpus closure is invalid"); err != nil {
		return CommittedTaskAuthority{}, err
	}
	graph, err := repositoryproject.BuildTaskGraph(scheduleEntries(schedule), contents)
	if err != nil {
		return CommittedTaskAuthority{}, fmt.Errorf("build committed semantic task graph: %w", err)
	}
	root, ok := findTaskRef(refs, rootPath)
	if !ok {
		return CommittedTaskAuthority{}, fmt.Errorf("committed root task %s is missing from task corpus", rootPath)
	}
	snapshot := ProjectSnapshot{
		SchemaVersion:       controllerSchemaVersion,
		RepositoryIdentity:  repositoryID,
		HeadOID:             head,
		PlanDigest:          digestBytes(plan),
		TaskCorpusDigest:    digestTaskRefs(refs),
		SemanticGraphDigest: digestDependencies(graph.Dependencies()),
		ScheduleDigest:      digestSchedule(schedule),
		Active:              append([]string(nil), schedule.Active...),
		Next:                append([]string(nil), schedule.Next...),
		Blocked:             append([]string(nil), schedule.Blocked...),
		Tasks:               append([]SemanticTaskRef(nil), refs...),
	}
	snapshot.SnapshotID = projectSnapshotID(snapshot)
	return CommittedTaskAuthority{Task: root, ProjectSnapshotID: snapshot.SnapshotID, Snapshot: snapshot}, nil
}

func committedTaskCorpus(repoRoot, head string) ([]taskcontract.TaskCorpusEntry, []SemanticTaskRef, map[string][]byte, error) {
	entries, err := committedTaskEntries(repoRoot, head)
	if err != nil {
		return nil, nil, nil, err
	}
	refs, contents, err := loadCommittedTasks(repoRoot, head, entries)
	if err != nil {
		return nil, nil, nil, err
	}
	return entries, refs, contents, nil
}

func committedTaskEntries(repoRoot, head string) ([]taskcontract.TaskCorpusEntry, error) {
	command := exec.Command("git", "-C", repoRoot, "ls-tree", "-r", "-z", head, "--", taskcontract.TasksDir)
	output, err := command.Output()
	if err != nil {
		return nil, fmt.Errorf("enumerate committed task corpus: %w", err)
	}
	var entries []taskcontract.TaskCorpusEntry
	for _, record := range strings.Split(string(output), "\x00") {
		entry, include, err := parseCommittedTaskEntry(record)
		if err != nil {
			return nil, err
		}
		if include {
			entries = append(entries, entry)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	return entries, nil
}

func parseCommittedTaskEntry(record string) (taskcontract.TaskCorpusEntry, bool, error) {
	if record == "" {
		return taskcontract.TaskCorpusEntry{}, false, nil
	}
	metadata, path, found := strings.Cut(record, "\t")
	if !found || !strings.HasSuffix(path, ".md") {
		return taskcontract.TaskCorpusEntry{}, false, nil
	}
	fields := strings.Fields(metadata)
	if len(fields) < 2 {
		return taskcontract.TaskCorpusEntry{}, false, fmt.Errorf("committed task corpus entry %q is malformed", record)
	}
	regular := (fields[0] == "100644" || fields[0] == "100755") && fields[1] == "blob"
	return taskcontract.TaskCorpusEntry{Path: filepath.ToSlash(path), Regular: regular}, true, nil
}

func loadCommittedTasks(
	repoRoot string,
	head string,
	entries []taskcontract.TaskCorpusEntry,
) ([]SemanticTaskRef, map[string][]byte, error) {
	refs := make([]SemanticTaskRef, 0, len(entries))
	contents := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if !entry.Regular {
			continue
		}
		content, err := readCommittedObject(repoRoot, head, entry.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read committed semantic task %s: %w", entry.Path, err)
		}
		contents[entry.Path] = content
		refs = append(refs, SemanticTaskRef{TaskPath: entry.Path, ContractDigest: digestBytes(content)})
	}
	return refs, contents, nil
}

func scheduleEntries(schedule taskcontract.PlanSchedule) []string {
	entries := append([]string(nil), schedule.Active...)
	entries = append(entries, schedule.Next...)
	return append(entries, schedule.Blocked...)
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

func digestDependencies(dependencies []repositoryproject.Dependency) string {
	copied := append([]repositoryproject.Dependency(nil), dependencies...)
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
