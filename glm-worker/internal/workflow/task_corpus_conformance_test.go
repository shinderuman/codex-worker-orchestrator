package workflow

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTaskCorpusScheduleStateConformanceはtask corpusのschedule state二重管理廃止後の
// 形式を固定する。schedule state(ACTIVE/NEXT/BLOCKED)の正はPlanだけであり、全task file本文は
// `## Status`節を1件も持たない。installer smoke等の合成repoは実際のcorpusを含むままPlanの
// ACTIVEだけscenario taskへ差し替えるため、検証はPlanのACTIVE指す先によらず corpus 全体へ
// 対する0件要求として機械検証する。
// また完了task fileは削除されるcontractのため、`## Dependencies`節の`IMPLEMENTATION_TASKS/`
// 参照は現存fileへ解決できなければならない。解決不能な依存を残すと依存完了時の除去契約
// (fulfilled dependencyの除去・必要invariantのHistorical invariants移行・file削除)が
// 実行されていないことを意味するためfailする。
// 両判定ともOriginal instruction等のfenced code block内部は文書構造ではないため対象外とし、
// top-level sectionだけを見る。
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

// TestTaskCorpusFindingsScopeToTopLevelSectionsはsection判定がOriginal instruction内の
// `## Status`見出し・`IMPLEMENTATION_TASKS/` task path bulletを誤検出せず、top-levelの
// `## Status`節とtop-level `## Dependencies`節の未解決参照だけを検出することを固定する。
// fence内の同文字列はlosslessなOriginal instruction本文の一部であり、検出してはならない。
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

// taskCorpusFindingsはtask file本文をtop-level sectionだけへ絞ってschedule state・dependency
// 契約へ照合し、違反の説明文を返す。`## Status`節の存在はschedule state二重管理、
// `## Dependencies`節内の`IMPLEMENTATION_TASKS/`参照の未解決はfulfilled dependency除去契約の
// 未実行をそれぞれ意味する。
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

// taskFileSectionLinesはtask file本文の行のうちfenced code blockの外だけを返す。
// Original instruction等は````text fenceでlosslessに保持され、fence内の`## Status`見出しや
// `IMPLEMENTATION_TASKS/` path bulletは文書構造ではない。一般Markdown parserを入れず、
// section判定に必要なfence境界だけを扱い、fenceは3つ以上のbacktick連続で開き
// 同数以上のbacktick連続の行で閉じる。
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
