package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestTerminalPayloadSingleRenderContractWiringは親tool orchestrationのterminal payload
// 単一描画contractのproduction wiringと、完了証拠authority区別・期待判断との因果を
// 決定論検証する。実運用3回の二面表示へ加え契約手順適用後の2026-08-24再現で、
// 単発live positive・模擬fixed Eval・instruction文面を継続的production enforcementと
// 同一視した2度のfalse-completeが確定した。撤回済みcaller除外文・file直接読出し手順・
// 完了証拠宣言・解消済み宣言の再混入のいずれかが発生すると失敗する。repo側dedupeで
// 代替した実装になっていないことと、worker/reviewer promptへ本契約のchecklistを追加
// しない方針も固定する。
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

	// production caller契約はfunctions.execのorchestration内での変数蓄積・Functionsのstoreへ
	// task固有key保存・captured marker返却・別の短い同期functions.execによるload(key)で構成される。
	for _, wire := range []string{
		"## 親tool orchestrationのterminal payload単一描画",
		"原因層はglm-worker内部emitではなく親tool orchestrationとDesktop表示である",
		"旧運用ではbackground `functions.exec`の完了outputと後続`functions.wait`のresult cardが同じraw terminal payloadを二面描画した",
		"本契約手順が強制できるのはmodel contextへの流入を1回にすることまでであり",
		"ユーザー可視描画回数はDesktop表示層の外部境界としてrepoから強制できない",
		"1 accepted terminal resultにつき、親tool orchestration全体でユーザー可視payloadは1回」である",
		"repo内emitの再調査・原因境界の特定だけでこの症状を解消扱いしない",
		"実害証拠がない限り、表示の再発をrepo内再調査・orchestration変更の理由にしない",
		"nested `exec_command`・`write_stdin`の各outputを変数へ蓄積し",
		"raw stdout・packetをtext・notify・image等の即時描画経路へ一切出さない",
		"Functionsのstoreへtask固有keyで保存し",
		"cellの返り値は短いcaptured marker(`GLM_TERMINAL_CAPTURED <key>`)だけにする",
		"別の短い同期`functions.exec`でstoreのload(key)を読み",
		"text(raw)として1回だけ親へ渡す",
		"この同期callで追加AI call・追加のglm-worker実行を行わない",
		"background cellの完了outputと`functions.wait`の双方へ同じraw payloadを流す運用は禁止する",
		"repo側のPACKET/JSON blind dedupeと正当な別terminal resultの抑止も行わない",
		"structured JSON移行後も結果object全体が同じ境界で二度描画され得る前提を維持し、JSON化を解決根拠にしない",
		"境界検証は追加AI callなしのdelayed markerと実`glm-worker` terminal resultを同じbackground exec→wait→同期取得境界で行う",
		"これは契約手順のsemantics検証であり実Desktop rendererの継続的保証ではなく",
		"単発live観測・模擬test・契約文面だけで解消済みと報告しない",
		"producer stdout・telemetry・親tool outputが各1件ならrepo内emitの再調査を繰り返さず",
	} {
		if !strings.Contains(glmExecution, wire) {
			t.Errorf("glm-execution.md lacks terminal payload wiring: %q", wire)
		}
	}

	// 実機で採用されたproduction caller手順はorchestration内store/loadであり、shell fileへの
	// redirect・file直接読出しの手順ではない。旧契約文の再混入を拒否する。
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

	// 撤回済みの解消宣言。契約手順適用後もDesktop表示層の二面表示が再現したため、
	// caller側手順で表示問題が解消したという旧主張と、再発時に手順へ戻すだけで再検証
	// できるという旧残余risk文を再混入させない。
	for _, revoked := range []string{
		"境界はcaller側で解消する",
		"将来のCodex desktop変更で同一境界の二面表示が再発した場合は本契約の手順へ戻す",
	} {
		if strings.Contains(glmExecution, revoked) {
			t.Errorf("glm-execution.md still contains the withdrawn resolution claim: %q", revoked)
		}
	}

	// 撤回済みのcaller除外文がEVAL.mdへ再混入していないことを固定する。本除外が3回の
	// 報告を通過した上位原因であるため、文言の部分再掲も許さない。
	for _, revoked := range []string{
		"repo外のため検証対象外",
		"caller側echoの二重表示はrepo外",
	} {
		if strings.Contains(evalDoc, revoked) {
			t.Errorf("EVAL.md still contains the revoked caller exclusion: %q", revoked)
		}
	}

	section := evalTerminalPayloadSection(t, evalDoc)
	// 親behavioral Evalの期待判断(EVAL.md本節)がproduction guidanceのどの契約文へ根拠を
	// 持つかを対で検証する。EVAL.md側の文面だけ、instruction側の契約文だけの片側存在は通さない。
	evalGrounds := []struct {
		eval     string
		guidance string
	}{
		{"1 accepted terminal resultにつき、親tool orchestration全体でユーザー可視payloadは1回」だけとする", "1 accepted terminal resultにつき、親tool orchestration全体でユーザー可視payloadは1回」である"},
		{"repo内emitの再調査・原因境界の特定だけで本項を完了扱いしない", "repo内emitの再調査・原因境界の特定だけでこの症状を解消扱いしない"},
		{"長時間cellではraw stdout・packetをtext・notify・image等の即時描画経路へ出さず内部store(task固有key)へ蓄積し", "nested `exec_command`・`write_stdin`の各outputを変数へ蓄積し"},
		{"長時間cellではraw stdout・packetをtext・notify・image等の即時描画経路へ出さず内部store(task固有key)へ蓄積し", "raw stdout・packetをtext・notify・image等の即時描画経路へ一切出さない"},
		{"長時間cellではraw stdout・packetをtext・notify・image等の即時描画経路へ出さず内部store(task固有key)へ蓄積し", "Functionsのstoreへtask固有keyで保存し"},
		{"cell終端後の短い同期callでのloadで1回だけ親へ渡す", "別の短い同期`functions.exec`でstoreのload(key)を読み"},
		{"同一raw payloadをbackground cellの完了outputと`functions.wait`双方へ流す実装・運用、repo側PACKET/JSON blind dedupe、正当な別terminal resultの抑止", "repo側のPACKET/JSON blind dedupeと正当な別terminal resultの抑止も行わない"},
		{"structured JSON object全体も同じ境界で二度描画され得る前提を維持し、JSON化を解決根拠にしない", "structured JSON移行後も結果object全体が同じ境界で二度描画され得る前提を維持し、JSON化を解決根拠にしない"},
		{"追加AI callなしのdelayed markerと実`glm-worker` binaryのterminal resultを同じbackground exec→wait→同期取得境界で検証する", "境界検証は追加AI callなしのdelayed markerと実`glm-worker` terminal resultを同じbackground exec→wait→同期取得境界で行う"},
		{"instruction文面・模擬test・単発live positiveは継続的production enforcementと同一視せず", "単発live観測・模擬test・契約文面だけで解消済みと報告しない"},
		{"表示の再発だけでは再調査せず", "producer stdout・telemetry・親tool outputが各1件ならrepo内emitの再調査を繰り返さず"},
		{"ユーザー可視単一描画は要求違反のままrepo外Desktop表示境界のため強制できず", "ユーザー可視描画回数はDesktop表示層の外部境界としてrepoから強制できない"},
	}
	for _, g := range evalGrounds {
		if !strings.Contains(glmExecution, g.guidance) {
			t.Errorf("glm-execution.md lacks guidance grounding %q", g.guidance)
		}
		if !strings.Contains(section, g.eval) {
			t.Errorf("EVAL.md terminal payload section lacks behavioral eval judgment grounded in guidance: %q", g.eval)
		}
	}

	// fixed Eval・層別evidence・authority区別・管理文面の参照。完了証拠の撤回・非対応分類・
	// activation条件・corpus重複禁止・checklist不追加をEVAL.md本節へ残す。
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

	// 撤回済みの完了証拠・解消宣言の再混入を拒否する。2026-08-21のlive観測自体は実施済み
	// のため未実行へ戻す文言も拒否し、単発観測を継続的保証へ格上げしない。
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

	// fixed Evalの実在。文面参照だけの自己充足を防ぐため、参照先test関数が実在することを
	// 確認する。
	boundaryTest := readContractFile("glm-worker/internal/app/terminal_payload_boundary_test.go")
	if !strings.Contains(boundaryTest, "func TestTerminalPayloadBoundarySingleRender(") {
		t.Error("EVAL.md references TestTerminalPayloadBoundarySingleRender but the test does not exist")
	}

	// corpusへ本contractの親behavioral Evalを重複するscenarioが追加されていないこと。
	// repo側dedupe・terminal result抑止の実装契約と矛盾するためである。
	sc, _ := loadCorpus(t)
	for _, s := range sc.Scenarios {
		if strings.HasPrefix(s.ID, "terminal-payload-") {
			t.Errorf("scenario %s must not duplicate the parent behavioral eval into the corpus", s.ID)
		}
	}

	// 本contractは親Codex側のcaller手順であり、常時checklistのworker/reviewer prompt
	// 追加で代替した実装になっていないことを固定する。
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
