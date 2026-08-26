package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEscapedCauseLayerContractWiring(t *testing.T) {
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
		{"codex/AGENTS.md", "escaped bug/reviewの原因層分類"},
		{"codex/AGENTS.md", "~/.codex/instructions/escaped-cause-layer.md"},
		{"codex/instructions/glm-execution.md", "外部review・実運用で見つかったescaped bug・escaped reviewの原因分析を委譲する場合は、`~/.codex/instructions/escaped-cause-layer.md`を読んでから委譲内容を構成する"},
		{"codex/instructions/escaped-cause-layer.md", "escaped bug・escaped reviewの原因分析を開始する場合だけ適用する"},
		{"codex/instructions/escaped-cause-layer.md", "通常の実装・調査task、review通過時の通常確認、新規依頼の受け付けへこの分類を要求しない"},
		{"codex/instructions/escaped-cause-layer.md", "対策・prompt変更・gate追加の検討より先に"},
		{"codex/instructions/escaped-cause-layer.md", "production code・prompt・PACKET契約・raw telemetry/log・Git履歴等の一次証拠から"},
		{"codex/instructions/escaped-cause-layer.md", "内部のworker/reviewer pipeline失敗"},
		{"codex/instructions/escaped-cause-layer.md", "親Codex orchestration失敗: critical assumptionの確定、親USER_REQUEST lifecycle、runtime evidence管理、semantic deltaに基づくreview invocation"},
		{"codex/instructions/escaped-cause-layer.md", "一次証拠で層が確定できない場合は推測で確定させず"},
		{"codex/instructions/escaped-cause-layer.md", "worker/reviewer promptへの個別checklist追加や個別gate追加だけで解決扱いにしない"},
		{"codex/instructions/escaped-cause-layer.md", "worker/reviewer prompt・個別gate・新しい対策を追加する前に、原因が本当にその層で発生したかを分類結果と照合する"},
		{"codex/instructions/escaped-cause-layer.md", "既存対策が直接対応している原因へ、重複する新しい対策を追加しない"},
		{"codex/instructions/escaped-cause-layer.md", "期待終端の再現だけでは採用根拠としない"},
		{"codex/instructions/escaped-cause-layer.md", "productionのprompt・dispatch分岐と実際に渡す内容・期待判断の因果を別testで固定する"},
		{"codex/instructions/escaped-cause-layer.md", "原因層の分類と対策方向の最終判断は親Codexが行い、GLMだけで確定させない"},
	}
	contents := make(map[string]string, 3)
	for _, c := range cases {
		if _, ok := contents[c.file]; !ok {
			contents[c.file] = readContractFile(c.file)
		}
		if !strings.Contains(contents[c.file], c.wire) {
			t.Errorf("%s lacks escaped cause layer wiring: %q", c.file, c.wire)
		}
	}

	requireParentBehaviorEval(t, "escaped-cause-layer")

	sc, mf := loadCorpus(t)
	corpusIDs := make(map[string]bool, len(sc.Scenarios))
	for _, s := range sc.Scenarios {
		corpusIDs[s.ID] = true
	}
	for _, id := range []string{
		"escaped-cause-layer-parent-orchestration-cause-returns-to-sol",
		"escaped-cause-layer-worker-pipeline-cause-fix-returns-to-sol-review",
		"escaped-cause-layer-unrelated-normal-task-completes",
	} {
		if !corpusIDs[id] {
			t.Errorf("scenario corpus lacks required escaped cause layer scenario %s", id)
		}
	}
	pinned := false
	for _, e := range mf.InstructionFiles {
		if e.Path == "codex/instructions/escaped-cause-layer.md" {
			pinned = true
		}
	}
	if !pinned {
		t.Error("manifest.json must pin codex/instructions/escaped-cause-layer.md")
	}

	for _, promptFile := range []string{"codex/glm-worker/prompts/WORKER.md", "codex/glm-worker/prompts/REVIEWER.md"} {
		if strings.Contains(readContractFile(promptFile), "escaped-cause-layer") {
			t.Errorf("%s must not add an escaped cause layer checklist", promptFile)
		}
	}
}
