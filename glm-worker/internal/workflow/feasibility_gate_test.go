package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFeasibilityGateContractWiringは親Codex側feasibility gateのproduction routing配線と、
// 親behavioral Eval入力・期待判断との因果を決定論検証する。codex/AGENTS.mdの条件付きrouting・
// 品質gate項目、glm-execution.mdの委譲前読込指示、feasibility-gate.md本文の必須契約文の
// いずれかが欠けると失敗する。EVAL.mdの親behavioral Evalはscripted scenarioの終端検証とは
// 異なり親Codexの委譲/受理行動の証明ではないため、その入力・期待判断がinstruction本文の
// どの契約文へ根拠を持つかを対で固定する。worker/reviewer promptへgate checklistを
// 追加しない方針も本testで固定する。
func TestFeasibilityGateContractWiring(t *testing.T) {
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
		{"codex/AGENTS.md", "未検証の外部成立性を本番設計の前提へ進める変更のGo/No-Goと撤退判断"},
		{"codex/AGENTS.md", "~/.codex/instructions/feasibility-gate.md"},
		{"codex/instructions/glm-execution.md", "未検証成立性が本番設計の前提になる依頼は、`~/.codex/instructions/feasibility-gate.md`を読んでから委譲内容を構成する"},
		{"codex/instructions/feasibility-gate.md", "未検証のcritical assumptionの列挙"},
		{"codex/instructions/feasibility-gate.md", "assumptionごとの最小PoCと代表case"},
		{"codex/instructions/feasibility-gate.md", "transport成功だけを成立性の証明にしない"},
		{"codex/instructions/feasibility-gate.md", "Amazon取得PoCの48〜72時間はその対象固有の観測条件であり一般contractへ固定しない"},
		{"codex/instructions/feasibility-gate.md", "短時間の意味的検証で足りる対象へ長時間試験を要求しない"},
		{"codex/instructions/feasibility-gate.md", "形式的なPoCや固定の観測期間を要求しない"},
		{"codex/instructions/feasibility-gate.md", "外部producerが必要なfieldを必要な時点で公開する可用性とそのevent timing"},
		{"codex/instructions/feasibility-gate.md", "人工fixture・scripted packet・worker/reviewer/Solの合意は、producerのfield・schema・timing成立の証拠として受理しない"},
		{"codex/instructions/feasibility-gate.md", "Go/No-Go基準と撤退条件"},
		{"codex/instructions/feasibility-gate.md", "workaroundの追加実装をさせず観測事実をSol/ユーザー判断へ戻す"},
		{"codex/instructions/feasibility-gate.md", "PoC・観測taskとproduction実装taskを分離する"},
	}
	contents := make(map[string]string, 3)
	for _, c := range cases {
		if _, ok := contents[c.file]; !ok {
			contents[c.file] = readContractFile(c.file)
		}
		if !strings.Contains(contents[c.file], c.wire) {
			t.Errorf("%s lacks feasibility gate wiring: %q", c.file, c.wire)
		}
	}

	// 親behavioral Evalの期待判断(EVAL.md)がproduction guidanceのどの契約文へ根拠を持つかを
	// 対で検証する。EVAL.md側の文面だけ、instruction側の契約文だけの片側存在は通さない。
	evalDoc := readContractFile("EVAL.md")
	instruction := contents["codex/instructions/feasibility-gate.md"]
	evalGrounds := []struct {
		eval     string
		guidance string
	}{
		{"HTTP 200・process exit 0・単発取得等のtransport成功だけが得られ、意味的成功条件・代表caseのterminal outcome・必要な試行回数/観測期間が未充足のPoCからproduction code・IaC・運用展開の実装へ進もうとする委譲をfeasibility gate根拠で拒否し", "HTTP 200・process exit 0・単発取得等のtransport成功だけを成立性の証明にしない"},
		{"HTTP 200・process exit 0・単発取得等のtransport成功だけが得られ、意味的成功条件・代表caseのterminal outcome・必要な試行回数/観測期間が未充足のPoCからproduction code・IaC・運用展開の実装へ進もうとする委譲をfeasibility gate根拠で拒否し", "未検証の外部成立性を前提にしたproduction code・IaC・運用展開の実装をGLMへ委譲しない"},
		{"PoC・観測taskとproduction実装taskへ分割する", "PoC・観測taskとproduction実装taskを分離する"},
		{"transport成功だけの完了報告を成立性の証明として受領せず差し戻し", "transport成功だけの完了報告を成立性の証明として受領しない"},
		{"transport成功だけの完了報告を成立性の証明として受領せず差し戻し", "意味的成功条件・代表caseのterminal outcome・観測結果が揃わない完了報告は差し戻す"},
		{"Go/No-Goと撤退判断をSol/ユーザーへ戻す", "Go/No-Goと撤退判断はSol High・ユーザーへ戻し、GLMだけで確定させない"},
		{"短時間の意味的検証でcritical assumptionが解消する対象へ、Amazon取得PoC固有の48〜72時間等の長時間観測を要求せず", "Amazon取得PoCの48〜72時間はその対象固有の観測条件であり一般contractへ固定しない"},
		{"短時間の意味的検証でcritical assumptionが解消する対象へ、Amazon取得PoC固有の48〜72時間等の長時間観測を要求せず", "外部API schema確認・実行環境からの到達確認・認証方式の成立確認など短時間の意味的検証で足りる対象へ長時間試験を要求しない"},
		{"確立済み前提内の保守変更へ形式的PoCを要求しない", "短時間の意味的検証でcritical assumptionを解消できる対象へ、形式的なPoCや固定の観測期間を要求しない"},
		{"未検証のproducer field可視性・event timing assumptionをPoC前にproduction実装へ委譲し", "外部producerが必要なfieldを必要な時点で公開する可用性とそのevent timing"},
		{"production実装前に停止すべきcaseとして固定される", "実producerでの最小PoCをproduction実装より先に要求する"},
		{"evidence authority(人工fixture不受理)・親Go/No-Goという既存gate要件から一意に導かれ", "人工fixture・scripted packet・worker/reviewer/Solの合意は、producerのfield・schema・timing成立の証拠として受理しない"},
	}
	for _, g := range evalGrounds {
		if !strings.Contains(instruction, g.guidance) {
			t.Errorf("feasibility-gate.md lacks guidance grounding %q", g.guidance)
		}
		if !strings.Contains(evalDoc, g.eval) {
			t.Errorf("EVAL.md lacks behavioral eval judgment grounded in guidance: %q", g.eval)
		}
	}

	// behavioral Eval・corpus参照の管理文面。scripted packetの拒否宣言を親Codexの委譲/受理
	// 行動の証明としない限定と、未実行Evalの一次証拠・完了条件・実行条件をEVAL.mdへ残す。
	for _, wire := range []string{
		"TestFeasibilityGateContractWiring",
		"feasibility-gate-production-beyond-unverified-viability-returns-to-sol",
		"feasibility-gate-premise-collapse-stops-further-implementation",
		"feasibility-gate-short-semantic-verification-completes",
		"feasibility-gate-established-premise-change-completes",
		"scripted packetの拒否宣言だけを親Codexの委譲/受理行動の証明として採用しない",
		"親behavioral Evalの代替として重複scenarioをcorpusへ追加しない",
		"親Codexのgate読込・routing判断・委譲内容・完了報告の受領/差し戻し行動をraw telemetry・task log等の一次証拠で照合",
		"live model呼出しを要するためユーザーの明示指示後だけ実行し",
		"EVAL.md本節のpositive/negative caseと期待判断を`feasibility-gate.md`の適用条件・意味的成功条件・観測期間・orchestration契約文へ直接突き合わせて検証する",
		"Z.ai 5時間limit early-stop escaped case(commit `cbf71c7`)を本caseへ統合する",
		"実incident raw eventと親Codex直接`claude -p` PoCが一次証拠",
		"実producer evidenceと親Go判断なしのimplementation委譲をmodel呼出0回で拒否する",
		"自然な該当taskでの親Codex行動証拠",
		"scripted packet・corpus・wiring test・manifest hash・本節の文面は親行動の証明に使わない",
		"行動証拠が得られないままの場合は再発防止を完了扱いにせずBLOCKEDへ残す",
	} {
		if !strings.Contains(evalDoc, wire) {
			t.Errorf("EVAL.md lacks feasibility gate eval wiring: %q", wire)
		}
	}

	// EVAL.mdが参照するcorpus entryとmanifest pinが実在すること。文面参照だけの
	// 自己充足を防ぎ、参照先がvalidateCorpusの対象へ入っていることを固定する。
	sc, mf := loadCorpus(t)
	corpusIDs := make(map[string]bool, len(sc.Scenarios))
	for _, s := range sc.Scenarios {
		corpusIDs[s.ID] = true
	}
	for _, id := range []string{
		"feasibility-gate-production-beyond-unverified-viability-returns-to-sol",
		"feasibility-gate-premise-collapse-stops-further-implementation",
		"feasibility-gate-short-semantic-verification-completes",
		"feasibility-gate-established-premise-change-completes",
	} {
		if !corpusIDs[id] {
			t.Errorf("scenario corpus lacks %s referenced by EVAL.md", id)
		}
	}
	pinned := false
	for _, e := range mf.InstructionFiles {
		if e.Path == "codex/instructions/feasibility-gate.md" {
			pinned = true
		}
	}
	if !pinned {
		t.Error("manifest.json must pin codex/instructions/feasibility-gate.md")
	}

	// 本contractは親Codex側の委譲・受領条件であり、常時checklistのworker/reviewer prompt
	// 追加で代替した実装になっていないことを固定する。
	for _, promptFile := range []string{"codex/glm-worker/prompts/WORKER.md", "codex/glm-worker/prompts/REVIEWER.md"} {
		if strings.Contains(readContractFile(promptFile), "feasibility") {
			t.Errorf("%s must not add a feasibility gate checklist", promptFile)
		}
	}
}
