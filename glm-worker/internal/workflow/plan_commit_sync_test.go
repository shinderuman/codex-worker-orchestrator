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
		{"codex/instructions/git.md", "HEADのplanが現在作業より一世代古いstale-by-oneになる"},
		{"codex/instructions/git.md", "初回commitと同一commitへのamendからなる二段階で解消する"},
		{"codex/instructions/git.md", "実装・test・独立review・必要なSol品質gate完了後も未完了項目を`[x]`にせず、planを作業実態と次task内容へ同期したcommit-ready状態へ更新する"},
		{"codex/instructions/git.md", "実装とcommit-ready planを初回commitへ含める"},
		{"codex/instructions/git.md", "親Codexが直ちにplanと`IMPLEMENTATION_HISTORY.md`を完了証跡(`[x]`)・次task・実working tree状態へ同期する"},
		{"codex/instructions/git.md", "同期済みplan/historyだけを初回commitと同じcommitへamendする"},
		{"codex/instructions/git.md", "final HEADとclean working treeを確認してからinstall・次task・handoffへ進む"},
		{"codex/instructions/git.md", "初回commitとamendの間に停止・ユーザー報告でのturn終了・別task開始・GLM起動・install・handoffを行わず、amendまでを同じturnの連続操作とする"},
		{"codex/instructions/git.md", "amend失敗時はobsolete HEADのままinstall・次task・handoffへ進まず、同じcommitへのplan/history同期を復旧して再度amendする"},
		{"codex/instructions/git.md", "追加commitの連鎖でplan同期を先送りしない"},
		{"codex/instructions/git.md", "大規模ledger・別status DB・追加commitの連鎖・worker/reviewer個別checklistは追加しない"},
		{"codex/instructions/git.md", "worker/reviewerへの個別checklist追加で代替しない"},
		{"codex/instructions/git.md", "plan本文・`[x]`・優先順・現在状態の更新権限が親Codex専有であること、commit実行の承認条件、Gitリモートへの書込禁止、wrapperのplan file不変guardは本契約で変更しない"},

		{"codex/instructions/git.md", "### final HEAD postconditionの機械強制"},
		{"codex/instructions/git.md", "install.shがmanaged配置の前段階としてfinal HEAD postconditionを機械検証する"},
		{"codex/instructions/git.md", "順序は「実装・commit-ready planの初回commit → 親CodexによるPlan・IMPLEMENTATION_TASKS・Historyの完了同期 → 同一commitへのamend → install.shのgate通過と本配置 → 次task・handoff」であり、installをamendより先に行わない"},
		{"codex/instructions/git.md", "gateは`git show HEAD:IMPLEMENTATION_PLAN.local.md`の内容だけを判定し、dirty working treeのplanを判定に使わない"},
		{"codex/instructions/git.md", "PlanのACTIVE欄が`IMPLEMENTATION_TASKS/`配下の`.md` task fileへ一意に解決できること"},
		{"codex/instructions/git.md", "ACTIVE/NEXT/BLOCKEDが参照するtask fileがすべてHEAD treeへregular fileとして存在すること"},
		{"codex/instructions/git.md", "ACTIVE task fileがNEXT/BLOCKEDへ重複記載されていないこと"},
		{"codex/instructions/git.md", "Git境界のbranchがHEADの実際のbranchと一致すること"},
		{"codex/instructions/git.md", "現在のGit境界・停止理由・次の親Codex操作が完了済みcommitの操作をamend直前・install前・amendの前等の未実施として記述していないこと"},
		{"codex/instructions/git.md", "ACTIVE/NEXT/BLOCKEDの各欄はbulletが存在するならすべてがbullet構文およびtask path契約へ解決されること(NEXT/BLOCKEDの空欄は許容する)"},
		{"codex/instructions/git.md", "task path契約はruntime配置契約(`validateActiveTaskPath`)と同じである"},
		{"codex/instructions/git.md", "bullet構文はruntime ACTIVE解決(`activeSectionEntries`/`activeEntryPath`)と同じである"},
		{"codex/instructions/git.md", "閉じbacktick欠損・前後の余分なtext・複数backtick組はmalformedとしてACTIVE/NEXT/BLOCKEDすべてでfail closedに拒否する"},
		{"codex/instructions/git.md", "schedule欄のlist記法は`- `bulletとblank行だけを許容し、`*`・`+`・番号付きmarker等のtask-like list行や説明文などの非bullet行も黙って無視せずACTIVE/NEXT/BLOCKEDすべてでfail closedに拒否する"},
		{"codex/instructions/git.md", "`TestPlanFinalHeadTaskPathValidatorMatchesRuntime`が固定する"},
		{"codex/instructions/git.md", "`TestPlanFinalHeadBulletExtractionMatchesRuntime`が固定する"},
		{"codex/instructions/git.md", "完了済み操作に続く正当な現在task記述(「amend後のpostconditionを実装する」等)で使う「amend後」は対象外とする"},
		{"codex/instructions/git.md", "判定はbyte志向の`LC_ALL=C`で行い"},
		{"codex/instructions/git.md", "gate失敗時はinstall・次task・handoffへ進まず、Plan・IMPLEMENTATION_TASKS・Historyを完了同期して同一commitへamendする"},
		{"codex/instructions/git.md", "amend失敗でobsolete HEADが残っている間もgateは拒否し続ける"},
		{"codex/instructions/git.md", "gateの適用外は非Git directory・commitが存在しないrepository・planがGit indexで未追跡のrepository・planがHEADへ未収録のrepositoryだけとし"},
		{"codex/instructions/git.md", "worker-start guard・worker/reviewer個別checklist・全repository共通hookは追加しない"},
		{"install.sh", "verify_plan_final_head || exit $?"},
		{"install.sh", "plan_transitional_pattern='(^|[^[:alnum:]_])(amend|install)(する)?(の|直)?前'"},
		{"install.sh", "require grep"},
		{"install.sh", "validate_plan_task_path"},
		{"install.sh", "LC_ALL=C grep -E \"$plan_transitional_pattern\""},
		{"install.sh", "skipped (IMPLEMENTATION_PLAN.local.md is untracked)"},
		{"install.sh", "skipped (IMPLEMENTATION_PLAN.local.md is not in HEAD yet)"},
		{"codex/instructions/git.md", "明示的な依頼がない限り`git commit`しない"},
		{"codex/instructions/git.md", "`git push`等Gitリモートへの書き込みは禁止"},
		{"AGENTS.md", "このplanの本文・`[x]`・優先順・現在状態を更新できるのは親Codexだけである"},
		{"AGENTS.md", "GLM worker/reviewerはこのfileを読み取り専用で参照し、編集・生成・復元・削除を行わない"},
		{"AGENTS.md", "同historyは親Codex専有のtracked archiveであり、GLM worker/reviewerは編集・生成・削除を行わず"},
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
		for _, keyword := range []string{"commit-ready", "stale-by-one", "amend", "commit同期"} {
			if strings.Contains(prompt, keyword) {
				t.Errorf("%s must not add a plan commit sync checklist (%s)", promptFile, keyword)
			}
		}
	}

	installer := readContractFile("install.sh")
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
