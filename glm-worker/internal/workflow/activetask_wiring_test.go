package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestActiveTaskContractWiring(t *testing.T) {
	root := scenarioRepoRoot(t)

	readFile := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(b)
	}

	agents := readFile("AGENTS.md")
	for _, wire := range []string{
		"parent-managed implementation metadataの単一集合",
		"IMPLEMENTATION_RULES.md`・`IMPLEMENTATION_PLAN.local.md`・`IMPLEMENTATION_TASKS/`配下全file・`IMPLEMENTATION_HISTORY.md",
		"pathごとの分岐を増やさず",
		"`## ACTIVE`節は実行中taskの要求正本を`IMPLEMENTATION_TASKS/`配下へ1件だけ指す",
		"workerとreviewerがそれぞれ独立にtask file本文から読む",
		"USER_REQUEST・会話要約・過去session記憶を要求定義の代わりにしない",
		"一意に解決できない場合(未記載・複数記載・配置契約外・参照file欠損)はmodel呼出前にfail closed",
		"task完了時のfile削除・history移行・plan昇格はTask 002の完了flowで行う",
	} {
		if !strings.Contains(agents, wire) {
			t.Errorf("root AGENTS.md lacks ACTIVE task contract wiring: %q", wire)
		}
	}

	execution := readFile("codex/instructions/glm-execution.md")
	for _, wire := range []string{
		"Planの`## ACTIVE`節から1件だけ指す",
		"USER_REQUESTへtask詳細を複製せず",
		"次の呼出前にtask fileのAmendmentsへ追記してから委譲する",
		"task完了前のtask file削除・history移行・plan昇格は行わない",
		"ACTIVE task解決失敗(`parent_metadata_active_unresolvable`)",
		"親Codexが直接確認・修復してから同じtaskを再開する",
	} {
		if !strings.Contains(execution, wire) {
			t.Errorf("codex/instructions/glm-execution.md lacks caller-side ACTIVE task wiring: %q", wire)
		}
	}

	for _, promptFile := range []string{"codex/glm-worker/prompts/WORKER.md", "codex/glm-worker/prompts/REVIEWER.md"} {
		prompt := readFile(promptFile)
		for _, keyword := range []string{"ACTIVE_TASK_FILE", "IMPLEMENTATION_TASKS", "IMPLEMENTATION_RULES.md"} {
			if strings.Contains(prompt, keyword) {
				t.Errorf("%s must not hard-code repository-specific ACTIVE task wiring (%s)", promptFile, keyword)
			}
		}
	}

	_, mf := loadCorpus(t)
	pinned := map[string]bool{}
	for _, e := range mf.InstructionFiles {
		pinned[e.Path] = true
	}
	for _, path := range []string{"AGENTS.md", "codex/instructions/glm-execution.md"} {
		if !pinned[path] {
			t.Errorf("manifest.json must pin %s for the ACTIVE task contract wiring", path)
		}
	}
}
