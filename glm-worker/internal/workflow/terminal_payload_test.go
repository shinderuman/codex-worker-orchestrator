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
	evalDoc := readContractFile("EVAL.md")

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

	for _, revoked := range []string{
		"repo外のため検証対象外",
		"caller側echoの二重表示はrepo外",
	} {
		if strings.Contains(evalDoc, revoked) {
			t.Errorf("EVAL.md still contains the revoked caller exclusion: %q", revoked)
		}
	}

	section := evalTerminalPayloadSection(t, evalDoc)

	evalGrounds := []struct {
		eval     string
		guidance string
	}{
		{"1 accepted terminal resultにつき、親tool orchestration全体でユーザー可視payloadは1回」だけとする", "ユーザー可視payloadは1回」である"},
		{"repo内emitの再調査・原因境界の特定だけで本項を完了扱いしない", "この症状を解消扱いしない"},
		{"長時間cellではraw stdout・packetをtext・notify・image等の即時描画経路へ出さず内部store(task固有key)へ蓄積し", "各outputを変数へ蓄積し"},
		{"長時間cellではraw stdout・packetをtext・notify・image等の即時描画経路へ出さず内部store(task固有key)へ蓄積し", "即時描画経路へ一切出さない"},
		{"長時間cellではraw stdout・packetをtext・notify・image等の即時描画経路へ出さず内部store(task固有key)へ蓄積し", "task固有keyで保存し"},
		{"cell終端後の短い同期callでのloadで1回だけ親へ渡す", "load(key)を読み"},
		{"同一raw payloadをbackground cellの完了outputと`functions.wait`双方へ流す実装・運用、repo側PACKET/JSON blind dedupe、正当な別terminal resultの抑止", "blind dedupe"},
		{"structured JSON object全体も同じ境界で二度描画され得る前提を維持し、JSON化を解決根拠にしない", "JSON化を解決根拠にしない"},
		{"追加AI callなしのdelayed markerと実`glm-worker` binaryのterminal resultを同じbackground exec→wait→同期取得境界で検証する", "delayed marker"},
		{"instruction文面・模擬test・単発live positiveは継続的production enforcementと同一視せず", "解消済みと報告しない"},
		{"表示の再発だけでは再調査せず", "再調査を繰り返さず"},
		{"ユーザー可視単一描画は要求違反のままrepo外Desktop表示境界のため強制できず", "Desktop表示層の外部境界"},
	}
	for _, g := range evalGrounds {
		if !strings.Contains(glmExecution, g.guidance) {
			t.Errorf("glm-execution.md lacks guidance grounding %q", g.guidance)
		}
		if !strings.Contains(section, g.eval) {
			t.Errorf("EVAL.md terminal payload section lacks behavioral eval judgment grounded in guidance: %q", g.eval)
		}
	}

	for _, wire := range []string{
		"TestTerminalPayloadBoundarySingleRender",
		"internal/app/terminal_payload_boundary_test.go",
		"tool orchestration semanticsをmodel化し",
		"追加AI callなしのdelayed marker",
		"fake claude binary固定応答で追加AI callなしに取得する",
		"TestTerminalPayloadSingleRenderContractWiring",
		"file直接読出し手順の再混入",
		"caller側echoの二重表示をrepo外として検証対象から除外し",
		"完了条件を「ユーザー可視payload 1回」から「repo内emit調査・原因境界特定」へ狭めても機械的に拒否しなかった",
		"caller側の二重描画をrepo外として検証対象から除外し直さない",
		"単発live positive・模擬fixed Eval・instruction文面を継続的production enforcementと同一視した2度目のfalse-completeが確定した",
		"以後この3種を本項の完了証拠として採用しない",
		"producer raw stdoutのaccepted terminal result 1件",
		"telemetry reviewer result 1件",
		"親Codex tool output 1件",
		"ユーザー可視表示2件",
		"Codex actual token影響は未観測のためunknownを維持し推測しない",
		"「要求を満たした」と「要求違反が残るが最上位目的へ影響しないため非対応」を区別して記録する",
		"ユーザー可視単一描画は要求違反のままrepo外Desktop表示境界のため強制できず",
		"完了条件・観測可能なproduction postcondition・検証証拠のauthorityを分離する",
		"実Desktop rendererの継続的保証ではない",
		"同一payloadのmodel context・永続contextへの二重流入の新証拠、測定可能なCodex実消費増、Quality Delta低下、またはCodex Desktop側の調査可能な修正境界が得られた場合だけBLOCKED taskを再開する",
		"親behavioral Evalの代替として`terminal-payload-*`scenarioをcorpusへ追加せず、repo側dedupe実装で代替しない",
		"worker/reviewer promptへ本契約のchecklistを追加しない",
	} {
		if !strings.Contains(section, wire) {
			t.Errorf("EVAL.md terminal payload section lacks eval wiring: %q", wire)
		}
	}

	for _, revoked := range []string{
		"fixed Evalと本live観測を本項の完了証拠とする",
		"live positive Evalは2026-08-21に実施済みである",
		"同期load後にterminal result全文が1回だけユーザー可視表示された",
		"実機の単一描画確認は上記live観測が担う",
		"未実行の固定Eval case",
		"ユーザーの明示指示後だけ実行し",
		"本項を未完了のままにする",
	} {
		if strings.Contains(section, revoked) {
			t.Errorf("EVAL.md terminal payload section reverts to the withdrawn completion evidence: %q", revoked)
		}
	}

	boundaryTest := readContractFile("glm-worker/internal/app/terminal_payload_boundary_test.go")
	if !strings.Contains(boundaryTest, "func TestTerminalPayloadBoundarySingleRender(") {
		t.Error("EVAL.md references TestTerminalPayloadBoundarySingleRender but the test does not exist")
	}

	sc, _ := loadCorpus(t)
	for _, s := range sc.Scenarios {
		if strings.HasPrefix(s.ID, "terminal-payload-") {
			t.Errorf("scenario %s must not duplicate the parent behavioral eval into the corpus", s.ID)
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

func evalTerminalPayloadSection(t *testing.T, evalDoc string) string {
	t.Helper()
	const header = "## 親tool orchestrationのterminal payload単一描画"
	start := strings.Index(evalDoc, header)
	if start < 0 {
		t.Fatalf("EVAL.md lacks section header %q", header)
	}
	rest := evalDoc[start+len(header):]
	if end := strings.Index(rest, "\n## "); end >= 0 {
		rest = rest[:end]
	}
	return rest
}
