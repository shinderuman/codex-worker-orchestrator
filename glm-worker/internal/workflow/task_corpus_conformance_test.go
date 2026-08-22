package workflow

import (
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
		if strings.Contains(string(body), "\n## Status\n") || strings.HasPrefix(string(body), "## Status\n") {
			t.Errorf("%sは`## Status`節を持ちます。schedule stateはPlanだけを正とするため、task file内のStatus管理は許容されません", taskPath)
		}
		for _, line := range strings.Split(string(body), "\n") {
			trimmed := strings.TrimSpace(line)
			if !strings.HasPrefix(trimmed, "- `IMPLEMENTATION_TASKS/") || !strings.HasSuffix(trimmed, "`") {
				continue
			}
			ref := strings.TrimSuffix(strings.TrimPrefix(trimmed, "- `"), "`")
			if !existing[ref] {
				t.Errorf("%sのDependenciesが参照する %s は存在しません。fulfilled dependencyは除去し必要なinvariantをHistorical invariantsへ残してください", taskPath, ref)
			}
		}
	}
}
