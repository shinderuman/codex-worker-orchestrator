# Task: fixed Eval harness/corpusの未実装部分を統合

## Original instruction

````text
## Task 015: fixed Eval harness/corpusの未実装部分統合

wrapperで固定できるoffline/fake-provider scenarioだけを対象。

以下の既存contractを重複実装しない。

- HIGH semantic defectをreviewer/Solが逃すcase
- external feasibility未検証なのにproductionへ進むcase
- safe-stopだけで親USER_REQUEST完了扱いするcase
- diagnosisに本文が必要なのにstatus/sizeだけ残すcase

既にwiring済みのものはfalse-complete確認だけし、追加checklistを増殖させない。

### live behavior

実Sol/Codexを消費するpositive/negative Evalは別途ユーザー明示許可待ちとし、Task 015完了条件へ混ぜない。
````

## Amendments

### 2026-08-24: 5時間limit false-completeを受けた追加指示

````text
じゃあそれで進めろ
あとこの件で一番気になったのはお前がゲートとして機能していなかったことと、そもそも事前にPoC等をやっていなかったことだ
そしてGLMも言われたことをやるだけやって結局何も確認できていなかったことだ
これらは改善すべきではないか
````

### 2026-08-24: Markdown変更の実効性確認

````text
お前が今やったMarkdownの修正は再発防止策になっているのか？
````

## Resolved references

- 「この件」はcommit `cbf71c7`のZ.ai 5時間limit early-stopを指す。実Claude CLIの`api_retry` eventはattempt 1-10でgenericな429/rate_limitしか公開せず、exact `[1308]`・5h・reset時刻はretry終了後のterminal assistant/resultで初めて公開された
- 親Codexはproduction実装前に実`claude -p`の短時間PoCを行わず、「途中stream eventでexact signalを観測できる」という未検証critical assumptionをGLMへ渡した。GLM/reviewer/Solはexact本文入り人工fixtureをproduction eventの成立証拠として受理した
- 既存`feasibility-gate.md`はPoC分離と親Go/No-Goを既に要求しており、今回の改善はworker/reviewer checklist追加ではなく、親がproducerのfield可視性・event timingを外部runtime assumptionとしてgate対象へ含める適用境界と証拠authorityの固定を指す
- 最新AmendmentはOriginal instructionの「live behaviorをTask 015完了条件へ混ぜない」を、追加AI callによるsynthetic Evalは要求しないが、自然な該当taskの行動証拠がない限り再発防止完了とはせずBLOCKEDへ残す契約へ更新する

## Purpose

既知escaped behaviorを追加AI callなしのproduction-path corpusへ固定する。

## Contract

- existing wrapper gate/wiringを再利用し未実装だけ追加
- scripted期待packetとproduction prompt/dispatch因果を分離固定
- 外部producerが必要なfieldを必要な時点で公開すること自体が効果成立の前提なら、実producerの最小PoCをproduction実装より先に要求する。人工fixtureはfield/schema/timingの成立証拠にしない
- 親CodexがPoC前に実装委譲した本incidentを既存external-feasibility caseへ統合し、worker/reviewer/Solの合意を親gate通過の代替にしない
- 実incidentのraw eventと直接PoCを一次証拠として使い、同じ内容を確認する追加GLM/Sol live callは行わない
- instruction・EVAL・wiring testの更新は再発防止contractの実装であっても親Codexの行動証拠ではない。実際の該当taskで親が実装委譲前にassumptionを止め、実producer PoCとGo/No-Goを先行させた一次証拠までを別authorityとして確認する
- behavioral evidenceを得るためだけの追加AI callは行わない。次の自然な該当taskがない場合、offline実装完了とbehavioral未検証を分離し、本taskを完了削除せずPlanのBLOCKEDへ移す

## Must not

- live Eval、重複prompt checklist、新reviewer層を追加しない
- 本incident専用の新しいgeneric gate/frameworkを追加しない
- 「GLMが指示どおり人工fixtureを通した」ことを実producer成立性や親Go判断の証拠にしない
- Markdown、manifest hash、wiring test、scripted packetだけで再発防止完了と記録しない

## Acceptance criteria

- 4 caseのoffline contractとwiring現物照合
- 5時間limit incidentについて、未検証assumption・最小PoC・実producer authority・Go/No-Goが既存feasibility gateから一意に導かれ、production実装前に停止すべきcaseとして固定される
- 親gate不適用をworker/reviewer checklist追加で解決扱いせず、EVALの「scripted packetは親行動の証明ではない」境界を維持する
- 親Codexのpositive behavioral evidenceは、該当taskのOriginal instruction、委譲前判断、PoC出力、Go/No-Go、production委譲順序を一次証拠で照合する。これが無い場合は未検証のままBLOCKEDへ残す
- behavioral evidence取得だけを目的にDirect Codex/Sol/GLM callを追加せず、最上位EvalのCodex ReductionとQuality Deltaへ不要な消費を発生させない
- false-completeなら該当taskをreopen
- test/race/vet/build/gofmt、独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- `e79e1ab`、`6d8d278`、`fc5f740`、`6257133`
- Task 001で成立したACTIVE task fileを要求正本とするtask lifecycle

## Dependencies

- `IMPLEMENTATION_TASKS/external-feasibility-dispatch-gate.md`

## Review findings

none

## Current boundary

既存wiringあり。5時間limit incidentの実producer evidenceにより親gate不適用が再現済み。reopen済み5時間limit taskの選択的revertとexternal feasibility production dispatch gate完了後、Task 009より前にgate受理集合とoffline contractを評価する。正しい親行動の一次証拠が自然な後続taskで得られなければ、再発防止完了とはせず本taskをBLOCKEDへ移してTask 009以降を進める。
