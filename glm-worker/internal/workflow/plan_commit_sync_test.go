package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanCommitSyncContractWiring(t *testing.T) {
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
		{"codex/AGENTS.md", "commit・Git履歴操作 → `~/.codex/instructions/git.md`"},
		{"codex/instructions/git.md", "## tracked canonical planのcommit同期"},
		{"codex/instructions/git.md", "repository rootに親Codex管理のtracked canonical plan(`IMPLEMENTATION_PLAN.local.md`)が存在するrepositoryのcommitだけに適用する親Codex orchestration contractである"},
		{"codex/instructions/git.md", "初回commitと同一commitへのamendからなる二段階で解消する"},
		{"codex/instructions/git.md", "実装とcommit-ready planを初回commitへ含める"},
		{"codex/instructions/git.md", "同期済みplan/historyだけを初回commitと同じcommitへamendする"},
		{"codex/instructions/git.md", "final HEADとclean working treeを確認してからinstall・次task・handoffへ進む"},
		{"codex/instructions/git.md", "amend失敗時はobsolete HEADのままinstall・次task・handoffへ進まず"},
		{"codex/instructions/git.md", "大規模ledger・別status DB・追加commitの連鎖・worker/reviewer個別checklistは追加しない"},
		{"codex/instructions/git.md", "### final HEAD postconditionの機械強制"},
		{"codex/instructions/git.md", "install.shがmanaged配置の前段階としてfinal HEAD postconditionを機械検証する"},
		{"codex/instructions/git.md", "PlanのACTIVE欄が`IMPLEMENTATION_TASKS/`配下の`.md` task fileへ一意に解決できること"},
		{"codex/instructions/git.md", "ACTIVE/NEXT/BLOCKEDが参照するtask fileがすべてHEAD treeへregular fileとして存在すること"},
		{"codex/instructions/git.md", "`TestPlanFinalHeadTaskPathValidatorMatchesRuntime`が固定する"},
		{"codex/instructions/git.md", "`TestPlanFinalHeadBulletExtractionMatchesRuntime`が固定する"},
		{"codex/instructions/git.md", "判定はbyte志向の`LC_ALL=C`で行い"},
		{"install.sh", "verify_plan_final_head || exit $?"},
		{"install.sh", "validate_plan_task_path"},
		{"install.sh", "plan_bullet_paths"},
		{"install.sh", "require grep"},
		{"codex/instructions/git.md", "明示的な依頼がない限り`git commit`しない"},
		{"codex/instructions/git.md", "`git push`等Gitリモートへの書き込みは禁止"},
		{"AGENTS.md", "このplanの本文・`[x]`・優先順・現在状態を更新できるのは親Codexだけである"},
		{"AGENTS.md", "GLM worker/reviewerはこのfileを読み取り専用で参照し、編集・生成・復元・削除を行わない"},
	}
	contents := make(map[string]string, 4)
	for _, c := range cases {
		if _, ok := contents[c.file]; !ok {
			contents[c.file] = readContractFile(c.file)
		}
		if !strings.Contains(contents[c.file], c.wire) {
			t.Errorf("%s lacks plan commit sync wiring: %q", c.file, c.wire)
		}
	}

	requireParentBehaviorEval(t, "plan-commit-sync")

	sc, _ := loadCorpus(t)
	for _, s := range sc.Scenarios {
		if strings.HasPrefix(s.ID, "plan-commit-sync-") {
			t.Errorf("scenario %s must not duplicate the live parent behavioral eval into the corpus", s.ID)
		}
	}

	for _, promptFile := range []string{"codex/glm-worker/prompts/WORKER.md", "codex/glm-worker/prompts/REVIEWER.md"} {
		prompt := readContractFile(promptFile)
		for _, keyword := range []string{"commit-ready", "stale-by-one", "commit同期"} {
			if strings.Contains(prompt, keyword) {
				t.Errorf("%s must not add a plan commit sync checklist (%s)", promptFile, keyword)
			}
		}
	}

	installer := contents["install.sh"]
	gateCall := strings.Index(installer, "verify_plan_final_head || exit $?")
	if gateCall < 0 {
		t.Fatalf("install.sh lacks verify_plan_final_head call")
	}
	requireGrep := strings.Index(installer, "\nrequire grep\n")
	if requireGrep < 0 {
		t.Fatalf("install.sh lacks require grep dependency for the gate")
	}
	if requireGrep > gateCall {
		t.Errorf("require grep must precede verify_plan_final_head so grep absence fails explicitly")
	}
	for _, placementCall := range []string{"\npreflight || exit $?\n", "\nbuild_glm_worker\n", "\ninstall_codex_files\n"} {
		at := strings.Index(installer, placementCall)
		if at < 0 {
			t.Fatalf("install.sh lacks placement call %q", strings.TrimSpace(placementCall))
		}
		if gateCall > at {
			t.Errorf("verify_plan_final_head must run before %s", strings.TrimSpace(placementCall))
		}
	}
}
