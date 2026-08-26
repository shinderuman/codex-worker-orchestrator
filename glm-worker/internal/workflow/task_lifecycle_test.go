package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTaskLifecycleContractWiring(t *testing.T) {
	root := scenarioRepoRoot(t)

	readContractFile := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(b)
	}

	cases := []struct {
		file string
		wire string
	}{
		{"codex/AGENTS.md", "安全停止・子task終端と親USER_REQUEST完了の区別"},
		{"codex/AGENTS.md", "~/.codex/instructions/task-lifecycle.md"},
		{"codex/instructions/glm-execution.md", "packet受理・個別commit・install完了は局所終端であり、親USER_REQUESTの完了か次の継続操作かは`~/.codex/instructions/task-lifecycle.md`を読んで判断する"},
		{"codex/instructions/task-lifecycle.md", "scheduler停止・queue/checkpoint保全・alarm報告の完了は局所終端である"},
		{"codex/instructions/task-lifecycle.md", "task・review・commit・installの個別完了は局所終端である"},
		{"codex/instructions/task-lifecycle.md", "親依頼本体と、ユーザー・automationが明示的に継続対象とした実装計画範囲の未解決作業がすべて解消した時だけ"},
		{"codex/instructions/task-lifecycle.md", "原因修正・再開確認・後続改善等が残るなら、同じCodexタスクで次の操作へ継続する"},
		{"codex/instructions/task-lifecycle.md", "monitorがscheduler停止・queue保全・alarm報告を完了しても、元依頼に診断・修正・再開確認が残る場合は親USER_REQUESTを完了扱いしない"},
		{"codex/instructions/task-lifecycle.md", "個別commit・installが完了しても、明示的に継続対象とした計画範囲が残る場合は親USER_REQUESTを完了扱いしない"},
		{"codex/instructions/task-lifecycle.md", "新しい権限、Codexの外で変わる外部状態、意味のあるユーザー判断が本当に必要な場合だけ停止する"},
		{"codex/instructions/task-lifecycle.md", "checkpoint・session・working treeを保持し、残作業とblockerを報告する"},
		{"codex/instructions/task-lifecycle.md", "実装計画に長期roadmapが存在するだけで、現在の親依頼範囲へ作業を勝手に拡張しない"},
		{"codex/instructions/task-lifecycle.md", "「後続へ継続」「停止しない」と明示した範囲を、直近subtaskの局所終端で打ち切らない"},
	}
	contents := make(map[string]string, 3)
	for _, c := range cases {
		if _, ok := contents[c.file]; !ok {
			contents[c.file] = readContractFile(c.file)
		}
		if !strings.Contains(contents[c.file], c.wire) {
			t.Errorf("%s lacks task lifecycle wiring: %q", c.file, c.wire)
		}
	}

	requireParentBehaviorEval(t, "task-lifecycle")

	expectedIDs := []string{
		"task-lifecycle-monitor-safe-stop-local-terminal-returns-to-sol",
		"task-lifecycle-external-judgment-blocker-stops-with-state",
		"task-lifecycle-explicitly-limited-deliverable-completes",
	}
	sc, mf := loadCorpus(t)
	expectedSet := make(map[string]bool, len(expectedIDs))
	for _, id := range expectedIDs {
		expectedSet[id] = true
	}
	for _, s := range sc.Scenarios {
		if !strings.HasPrefix(s.ID, "task-lifecycle-") {
			continue
		}
		if !expectedSet[s.ID] {
			t.Errorf("scenario %s must not duplicate the parent behavioral eval into the corpus", s.ID)
		}
	}
	for _, id := range expectedIDs {
		found := false
		for _, s := range sc.Scenarios {
			if s.ID == id {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("scenario corpus lacks required task lifecycle scenario %s", id)
		}
	}
	pinned := false
	for _, e := range mf.InstructionFiles {
		if e.Path == "codex/instructions/task-lifecycle.md" {
			pinned = true
		}
	}
	if !pinned {
		t.Error("manifest.json must pin codex/instructions/task-lifecycle.md")
	}

	for _, promptFile := range []string{"codex/glm-worker/prompts/WORKER.md", "codex/glm-worker/prompts/REVIEWER.md"} {
		prompt := readContractFile(promptFile)
		for _, keyword := range []string{"task-lifecycle", "親USER_REQUEST", "局所終端"} {
			if strings.Contains(prompt, keyword) {
				t.Errorf("%s must not add a task lifecycle checklist (%s)", promptFile, keyword)
			}
	}
}
