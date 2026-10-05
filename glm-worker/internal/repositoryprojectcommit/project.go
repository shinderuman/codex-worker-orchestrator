package repositoryprojectcommit

import (
	"fmt"
	"os/exec"
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
	Plan         []byte
	Schedule     taskcontract.PlanSchedule
	RootTaskPath string
	Tasks        []Task
	graph        *repositoryproject.TaskGraph
}

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
	return repositoryproject.RetireTerminalMetadata(p.Plan, contents, taskPath, blocker)
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
	regular := (fields[0] == "100644" || fields[0] == "100755") && fields[1] == "blob"
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
	command := exec.Command("git", "-C", root, "show", revision+":"+filepath.ToSlash(path))
	return command.Output()
}

func gitOutput(root string, args ...string) (string, error) {
	commandArgs := append([]string{"-C", root}, args...)
	command := exec.Command("git", commandArgs...)
	output, err := command.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimRight(string(output), "\n"), nil
}
