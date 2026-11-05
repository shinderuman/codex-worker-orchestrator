package repositoryprojectcommit

import (
	"fmt"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/gitexec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/repositoryproject"
	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/taskcontract"
)

type Task struct {
	Path    string
	Content []byte
}

type Dependency struct {
	Task        string
	Outstanding []string
	Fulfilled   []string
}

type TerminalMetadata = repositoryproject.TerminalMetadata

type Project struct {
	Head         string
	RepoRoot     string
	Plan         []byte
	Schedule     taskcontract.PlanSchedule
	RootTaskPath string
	Tasks        []Task
	graph        *repositoryproject.TaskGraph
}

const requirementBlobType = "blob"

func Load(root, revision string) (Project, error) {
	head, err := gitOutput(root, "rev-parse", "--verify", revision+"^{commit}")
	if err != nil {
		return Project{}, err
	}
	head = strings.TrimSpace(head)
	if head == "" {
		return Project{}, fmt.Errorf("committed repository revision is empty")
	}
	plan, err := readObject(root, head, "IMPLEMENTATION_PLAN.local.md")
	if err != nil {
		return Project{}, fmt.Errorf("read committed implementation plan: %w", err)
	}
	schedule := taskcontract.ParsePlanSchedule(string(plan))
	rootTaskPath, err := schedule.ValidateComplete()
	if err != nil {
		return Project{}, err
	}
	entries, err := taskCorpusEntries(root, head, false)
	if err != nil {
		return Project{}, err
	}
	if err := repositoryproject.ValidateClosure(schedule, entries, "committed Plan/task corpus closure is invalid"); err != nil {
		return Project{}, err
	}
	tasks, contents, err := loadTasks(root, head, entries)
	if err != nil {
		return Project{}, err
	}
	graph, err := repositoryproject.BuildTaskGraph(scheduleEntries(schedule), contents)
	if err != nil {
		return Project{}, fmt.Errorf("build committed semantic task graph: %w", err)
	}
	return Project{
		Head:         head,
		RepoRoot:     root,
		Plan:         plan,
		Schedule:     schedule,
		RootTaskPath: rootTaskPath,
		Tasks:        tasks,
		graph:        graph,
	}, nil
}

func TaskCorpusEntries(root, revision string) ([]taskcontract.TaskCorpusEntry, error) {
	return taskCorpusEntries(root, revision, true)
}

func taskCorpusEntries(root, revision string, includeTrees bool) ([]taskcontract.TaskCorpusEntry, error) {
	args := []string{"ls-tree", "-r"}
	if includeTrees {
		args = append(args, "-t")
	}
	args = append(args, "-z", revision, "--", taskcontract.TasksDir)
	output, err := gitOutput(root, args...)
	if err != nil {
		return nil, fmt.Errorf("enumerate committed task corpus: %w", err)
	}
	var entries []taskcontract.TaskCorpusEntry
	for _, record := range strings.Split(output, "\x00") {
		entry, include, err := parseTaskCorpusEntry(record)
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

func (p Project) Dependencies() []Dependency {
	dependencies := p.graph.Dependencies()
	result := make([]Dependency, 0, len(dependencies))
	for _, dependency := range dependencies {
		result = append(result, Dependency{
			Task:        dependency.Task,
			Outstanding: append([]string(nil), dependency.Outstanding...),
			Fulfilled:   append([]string(nil), dependency.Fulfilled...),
		})
	}
	return result
}

func (p Project) RetireTerminalMetadata(taskPath string, blocker bool) (TerminalMetadata, error) {
	contents := make(map[string][]byte, len(p.Tasks))
	for _, task := range p.Tasks {
		contents[task.Path] = task.Content
	}
	metadata, err := repositoryproject.RetireTerminalMetadata(p.Plan, contents, taskPath, blocker)
	if err != nil {
		return TerminalMetadata{}, err
	}
	if err := p.validateCommittedRequirementSources(taskPath); err != nil {
		return TerminalMetadata{}, err
	}
	return metadata, nil
}

func (p Project) validateCommittedRequirementSources(retiring string) error {
	for _, task := range p.Tasks {
		if task.Path == retiring {
			continue
		}
		sources, err := taskcontract.ParseRequirementSources(task.Content)
		if err != nil {
			return fmt.Errorf("task %s: %w", task.Path, err)
		}
		for _, source := range sources {
			if err := p.verifyCommittedRequirementSource(source); err != nil {
				return fmt.Errorf("task %s line %d: %w", task.Path, source.Line, err)
			}
		}
	}
	return nil
}

func (p Project) verifyCommittedRequirementSource(source taskcontract.RequirementSource) error {
	switch source.Kind {
	case taskcontract.RequirementSourceFile:
		if p.RepoRoot == "" || p.Head == "" {
			return fmt.Errorf("current file requirement source without committed Git identity")
		}
		return p.verifyRequirementBlob(p.Head, source.Path)
	case taskcontract.RequirementSourceGit:
		if p.RepoRoot == "" {
			return fmt.Errorf("pinned requirement source without repository root")
		}
		kind, err := gitOutput(p.RepoRoot, "cat-file", "-t", source.Commit)
		if err != nil || kind != "commit" {
			return fmt.Errorf("unavailable pinned requirement commit %s", source.Commit)
		}
		return p.verifyRequirementBlob(source.Commit, source.Path)
	}
	return nil
}

func (p Project) verifyRequirementBlob(commit, sourcePath string) error {
	kind, err := gitOutput(p.RepoRoot, "cat-file", "-t", commit+":"+sourcePath)
	if err != nil || kind != requirementBlobType {
		return fmt.Errorf("unavailable requirement blob %s:%s", commit, sourcePath)
	}
	return nil
}

func parseTaskCorpusEntry(record string) (taskcontract.TaskCorpusEntry, bool, error) {
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
	regular := (fields[0] == "100644" || fields[0] == "100755") && fields[1] == requirementBlobType
	return taskcontract.TaskCorpusEntry{Path: filepath.ToSlash(path), Regular: regular}, true, nil
}

func loadTasks(root, head string, entries []taskcontract.TaskCorpusEntry) ([]Task, map[string][]byte, error) {
	tasks := make([]Task, 0, len(entries))
	contents := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		if !entry.Regular {
			continue
		}
		content, err := readObject(root, head, entry.Path)
		if err != nil {
			return nil, nil, fmt.Errorf("read committed semantic task %s: %w", entry.Path, err)
		}
		contents[entry.Path] = content
		tasks = append(tasks, Task{Path: entry.Path, Content: content})
	}
	return tasks, contents, nil
}

func scheduleEntries(schedule taskcontract.PlanSchedule) []string {
	entries := append([]string(nil), schedule.Active...)
	entries = append(entries, schedule.Next...)
	return append(entries, schedule.Blocked...)
}

func readObject(root, revision, path string) ([]byte, error) {
	command := gitexec.Command("git", root, "show", revision+":"+filepath.ToSlash(path))
	return command.Output()
}

func gitOutput(root string, args ...string) (string, error) {
	command := gitexec.Command("git", root, args...)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(output), "\n"), nil
}
