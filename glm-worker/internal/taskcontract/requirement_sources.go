package taskcontract

import (
	"fmt"
	"path"
	"regexp"
	"strings"
)

type RequirementSource struct {
	Kind   string
	Path   string
	Commit string
	Line   int
}

const (
	RequirementSourceTask = "task"
	RequirementSourceFile = "file"
	RequirementSourceGit  = "git"

	requirementSourceHeading = "## Resolved references"
	requirementSourcePrefix  = "- requirement-source: "
)

var fullGitCommit = regexp.MustCompile("^[0-9a-f]{40}$")

func ParseRequirementSources(content []byte) ([]RequirementSource, error) {
	lines := strings.Split(string(content), "\n")
	section, err := findUniqueTaskSection(lines, requirementSourceHeading)
	if err != nil || section < 0 {
		return nil, err
	}
	return parseRequirementSourceSection(lines, section)
}

func parseRequirementSourceSection(lines []string, section int) ([]RequirementSource, error) {
	var sources []RequirementSource
	seen := map[string]bool{}
	fence := 0
	for i := section + 1; i < len(lines); i++ {
		line := lines[i]
		if !lineOutsideFence(line, &fence) {
			continue
		}
		if strings.HasPrefix(line, "## ") {
			break
		}
		item := strings.TrimSpace(line)
		if !strings.HasPrefix(item, requirementSourcePrefix) {
			continue
		}
		source, err := parseRequirementSourceLine(item, i+1)
		if err != nil {
			return nil, err
		}
		key := source.Kind + ":" + source.Commit + ":" + source.Path
		if seen[key] {
			return nil, fmt.Errorf("duplicate requirement-source %q", item)
		}
		seen[key] = true
		sources = append(sources, source)
	}
	return sources, nil
}

func parseRequirementSourceLine(item string, line int) (RequirementSource, error) {
	value := strings.TrimSpace(strings.TrimPrefix(item, requirementSourcePrefix))
	if len(value) < 3 || value[0] != 96 || value[len(value)-1] != 96 || strings.Count(value, string(rune(96))) != 2 {
		return RequirementSource{}, fmt.Errorf("requirement-source at line %d must be a single backtick-wrapped locator", line)
	}
	source, err := parseRequirementSource(value[1 : len(value)-1])
	if err != nil {
		return RequirementSource{}, fmt.Errorf("requirement-source at line %d: %w", line, err)
	}
	source.Line = line
	return source, nil
}

func parseRequirementSource(value string) (RequirementSource, error) {
	kind, rest, ok := strings.Cut(value, ":")
	if !ok {
		return RequirementSource{}, fmt.Errorf("missing source type in %q", value)
	}
	source := RequirementSource{Kind: kind}
	switch kind {
	case RequirementSourceTask, RequirementSourceFile:
		source.Path = rest
	case RequirementSourceGit:
		commit, sourcePath, hasPath := strings.Cut(rest, ":")
		if !hasPath || !fullGitCommit.MatchString(commit) {
			return RequirementSource{}, fmt.Errorf("git source requires a full lowercase commit SHA and path")
		}
		source.Commit, source.Path = commit, sourcePath
	default:
		return RequirementSource{}, fmt.Errorf("unknown source type %q", kind)
	}
	if err := validateRequirementSourcePath(source.Path); err != nil {
		return RequirementSource{}, err
	}
	if kind == RequirementSourceTask {
		if err := ValidateActiveTaskPath(source.Path); err != nil {
			return RequirementSource{}, err
		}
	}
	return source, nil
}

func validateRequirementSourcePath(sourcePath string) error {
	if sourcePath == "" || sourcePath == "." || sourcePath == ".." ||
		strings.HasPrefix(sourcePath, "../") || strings.HasPrefix(sourcePath, "/") ||
		strings.Contains(sourcePath, "\\") || strings.Contains(sourcePath, ":") ||
		path.Clean(sourcePath) != sourcePath || sourcePath == ".git" ||
		strings.HasPrefix(sourcePath, ".git/") {
		return fmt.Errorf("invalid repository-relative requirement source path %q", sourcePath)
	}
	return nil
}

func RequirementSourcesRetirementError(tasks map[string][]byte, retiring string) error {
	for taskPath, content := range tasks {
		if taskPath == retiring {
			continue
		}
		sources, err := ParseRequirementSources(content)
		if err != nil {
			return fmt.Errorf("task %s: %w", taskPath, err)
		}
		if err := checkRetiringRequirementSources(tasks, taskPath, retiring, sources); err != nil {
			return err
		}
	}
	return nil
}

func checkRetiringRequirementSources(tasks map[string][]byte, taskPath, retiring string, sources []RequirementSource) error {
	for _, source := range sources {
		if isTaskRequirementSource(source) {
			if _, exists := tasks[source.Path]; !exists && source.Path != retiring {
				return fmt.Errorf("task %s line %d requires missing task source %s", taskPath, source.Line, source.Path)
			}
		}
		if source.Path == retiring && isTaskRequirementSource(source) {
			return fmt.Errorf("task %s line %d still requires retiring source %s; parent must preserve or transfer the requirement before retirement", taskPath, source.Line, retiring)
		}
	}
	return nil
}

func isTaskRequirementSource(source RequirementSource) bool {
	return source.Kind == RequirementSourceTask ||
		(source.Kind == RequirementSourceFile && strings.HasPrefix(source.Path, TasksDir+"/"))
}
