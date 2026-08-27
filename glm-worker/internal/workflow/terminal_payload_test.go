package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestTerminalPayloadSingleRenderContractWiring(t *testing.T) {
	root := scenarioRepoRoot(t)

	readContractFile := func(rel string) string {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatalf("read %s: %v", rel, err)
		}
		return string(b)
	}

	glmExecution := readContractFile("codex/instructions/glm-execution.md")

	for _, wire := range []string{
		"## 親tool orchestrationのterminal payload単一描画",
		"原因層はglm-worker内部emitではなく",
		"model contextへの流入を1回にすることまで",
		"Desktop表示層の外部境界",
		"ユーザー可視payloadは1回」である",
		"この症状を解消扱いしない",
		"表示の再発をrepo内再調査・orchestration変更の理由にしない",
		"各outputを変数へ蓄積し",
		"即時描画経路へ一切出さない",
		"task固有keyで保存し",
		"GLM_TERMINAL_CAPTURED <key>",
		"load(key)を読み",
		"1回だけ親へ渡す",
		"追加AI call・追加のglm-worker実行を行わない",
		"双方へ同じraw payloadを流す運用は禁止する",
		"blind dedupe",
		"JSON化を解決根拠にしない",
		"delayed marker",
		"semantics検証であり",
		"解消済みと報告しない",
		"再調査を繰り返さず",
	} {
		if !strings.Contains(glmExecution, wire) {
			t.Errorf("glm-execution.md lacks terminal payload wiring: %q", wire)
		}
	}

	for _, revoked := range []string{
		"> <store>",
		"cat <store>",
		"redirect形",
		"内部store(file)",
	} {
		if strings.Contains(glmExecution, revoked) {
			t.Errorf("glm-execution.md still contains the revoked file-store procedure: %q", revoked)
		}
	}

	for _, revoked := range []string{
		"境界はcaller側で解消する",
		"将来のCodex desktop変更で同一境界の二面表示が再発した場合は本契約の手順へ戻す",
	} {
		if strings.Contains(glmExecution, revoked) {
			t.Errorf("glm-execution.md still contains the withdrawn resolution claim: %q", revoked)
		}
	}

	boundaryTest := readContractFile("glm-worker/internal/app/terminal_payload_boundary_test.go")
	if !strings.Contains(boundaryTest, "func TestTerminalPayloadBoundarySingleRender(") {
		t.Error("terminal payload boundary contract references TestTerminalPayloadBoundarySingleRender but the test does not exist")
	}

	sc, _ := loadCorpus(t)
	for _, s := range sc.Scenarios {
		if strings.HasPrefix(s.ID, "terminal-payload-") {
			t.Errorf("scenario %s must not duplicate the caller boundary contract into the corpus", s.ID)
		}
	}

	for _, promptFile := range []string{"codex/glm-worker/prompts/WORKER.md", "codex/glm-worker/prompts/REVIEWER.md"} {
		prompt := readContractFile(promptFile)
		for _, keyword := range []string{"terminal payload", "単一描画", "二面表示", "内部store", "GLM_TERMINAL_CAPTURED"} {
			if strings.Contains(prompt, keyword) {
				t.Errorf("%s must not add a terminal payload checklist (%s)", promptFile, keyword)
			}
		}
	}
}
