package workflow

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskCorpusScheduleStateConformance(t *testing.T) {
	root := scenarioRepoRoot(t)
	tasksDir := filepath.Join(root, "IMPLEMENTATION_TASKS")

	planBytes, err := os.ReadFile(filepath.Join(root, implementationPlanFile))
	if err != nil {
		t.Fatalf("read %s: %v", implementationPlanFile, err)
	}
	activeEntries, err := activeSectionEntries(string(planBytes))
	if err != nil || len(activeEntries) != 1 {
		t.Fatalf("plan ACTIVE解決が成立していません: entries=%v err=%v", activeEntries, err)
	}
	activeTask := activeEntries[0]

	existing := map[string]bool{}
	err = filepath.WalkDir(tasksDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		existing[filepath.ToSlash(rel)] = true
		return nil
	})
	if err != nil {
		t.Fatalf("walk IMPLEMENTATION_TASKS: %v", err)
	}
	if !existing[activeTask] {
		t.Fatalf("planのACTIVE task file %s がtask corpusへ存在しません", activeTask)
	}

	for taskPath := range existing {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(taskPath)))
		if err != nil {
			t.Fatalf("read %s: %v", taskPath, err)
		}
		for _, finding := range taskCorpusFindings(taskPath, string(body), existing) {
			t.Errorf("%s", finding)
		}
	}
}

func TestTaskCorpusFindingsScopeToTopLevelSections(t *testing.T) {
	existing := map[string]bool{"IMPLEMENTATION_TASKS/016-worker-repo-search-integration.md": true}

	quotedBody := "# Task: fixture\n" +
		"## Original instruction\n" +
		"`````text\n" +
		"planのschedule欄は次の形で書かせないこと:\n" +
		"## Status\n" +
		"- ACTIVE\n" +
		"## Dependencies\n" +
		"- `IMPLEMENTATION_TASKS/missing-inside-instruction.md`\n" +
		"```\n" +
		"fence内に3-backtick blockを含む例\n" +
		"`````\n" +
		"## Dependencies\n" +
		"- `IMPLEMENTATION_TASKS/016-worker-repo-search-integration.md`\n"
	if findings := taskCorpusFindings("IMPLEMENTATION_TASKS/fixture.md", quotedBody, existing); len(findings) != 0 {
		t.Fatalf("Original instruction内のStatus見出し・task path bulletを誤検出しています: %v", findings)
	}

	topLevelStatusBody := "# Task: fixture\n" +
		"## Original instruction\n" +
		"````text\n" +
		"本文\n" +
		"````\n" +
		"## Status\n" +
		"- ACTIVE\n" +
		"## Dependencies\n" +
		"- `IMPLEMENTATION_TASKS/016-worker-repo-search-integration.md`\n"
	findings := taskCorpusFindings("IMPLEMENTATION_TASKS/fixture.md", topLevelStatusBody, existing)
	if len(findings) != 1 || !strings.Contains(findings[0], "`## Status`節を持ちます") {
		t.Fatalf("top-level `## Status`節が検出されていません: %v", findings)
	}

	unresolvedBody := "# Task: fixture\n" +
		"## Original instruction\n" +
		"````text\n" +
		"本文\n" +
		"````\n" +
		"## Dependencies\n" +
		"- `IMPLEMENTATION_TASKS/missing-top-level.md`\n"
	findings = taskCorpusFindings("IMPLEMENTATION_TASKS/fixture.md", unresolvedBody, existing)
	if len(findings) != 1 || !strings.Contains(findings[0], "missing-top-level.md") {
		t.Fatalf("top-level `## Dependencies`節の未解決参照が検出されていません: %v", findings)
	}
}

func taskCorpusFindings(taskPath string, body string, existing map[string]bool) []string {
	var findings []string
	inDependencies := false
	for _, line := range taskFileSectionLines(body) {
		if strings.HasPrefix(line, "## ") {
			heading := strings.TrimSpace(strings.TrimPrefix(line, "## "))
			inDependencies = heading == "Dependencies"
			if heading == "Status" {
				findings = append(findings, fmt.Sprintf("%sは`## Status`節を持ちます。schedule stateはPlanだけを正とするため、task file内のStatus管理は許容されません", taskPath))
			}
			continue
		}
		if !inDependencies {
			continue
		}
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "- `IMPLEMENTATION_TASKS/") || !strings.HasSuffix(trimmed, "`") {
			continue
		}
		ref := strings.TrimSuffix(strings.TrimPrefix(trimmed, "- `"), "`")
		if !existing[ref] {
			findings = append(findings, fmt.Sprintf("%sのDependenciesが参照する %s は存在しません。fulfilled dependencyは除去し必要なinvariantをHistorical invariantsへ残してください", taskPath, ref))
		}
	}
	return findings
}

func taskFileSectionLines(body string) []string {
	var lines []string
	fence := 0
	for _, line := range strings.Split(body, "\n") {
		backticks := leadingBackticks(line)
		if fence > 0 {
			if backticks >= fence {
				fence = 0
			}
			continue
		}
		if backticks >= 3 {
			fence = backticks
			continue
		}
		lines = append(lines, line)
	}
	return lines
}
