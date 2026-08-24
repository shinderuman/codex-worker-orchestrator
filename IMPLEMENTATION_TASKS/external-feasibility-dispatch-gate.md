# Task: external feasibilityをimplementation dispatch前にfail closedする

## Original instruction

````text
じゃあそれで進めろ
あとこの件で一番気になったのはお前がゲートとして機能していなかったことと、そもそも事前にPoC等をやっていなかったことだ
そしてGLMも言われたことをやるだけやって結局何も確認できていなかったことだ
これらは改善すべきではないか
````

## Amendments

### 2026-08-24: Markdownだけでは再発防止にならない

````text
お前が今やったMarkdownの修正は再発防止策になっているのか？
````

### 2026-08-24: Task 015の完了条件変更だけでは不足

````text
だから015の完了条件を変えるだけじゃ再発するだけなんじゃないのって言ってるの
````

### 2026-08-24: ACTIVE boundary同期

````text
## 2. ACTIVE external feasibility taskのstale boundaryを同期する

IMPLEMENTATION_TASKS/external-feasibility-dispatch-gate.md に現在も、

- 「現ACTIVE Task 008」
- 「Task 008停止中は...production codeへ触れない」

という作成時の境界が残っている。

現在の正はPlanどおり:

- Task 008は完了済み
- 5h early-stop selective revertも完了済み
- external-feasibility-dispatch-gate自体がACTIVE
- 次はこのgateをimplementationする

したがってOriginal instruction / Amendments / Resolved references等の
historical evidenceは保持したまま、
Must not / Current boundaryだけを現在状態へ同期する。

原因はTask 008実行中にfollow-upを積み、
Task 008完了→5h revert→ACTIVE昇格時にtask-local boundaryを更新しなかった
parent lifecycle metadata同期漏れ。
GLM実装上の問題として扱わない。
````

## Resolved references

- 「この件」はcommit `cbf71c7`のZ.ai 5時間limit early-stop false-complete。実Claude CLIはretry中の`api_retry`へgeneric 429しか出さず、exact `[1308]`は全retry後に初めて公開した
- 既存feasibility instruction・EVAL・wiring testは存在したが、親Codexが適用せずimplementationをdispatchできた。したがってMarkdown存在確認やTask 015の評価条件だけでは同じ失敗を防げない
- 現incidentではproduction価値が「exact signalをretry前に観測できる」という未検証external runtime assumptionへ全面依存しており、実producer PoCなしにimplementationへ進めてはならなかった

## Purpose

未検証external feasibilityをproduction設計の前提にするtaskをmodel call前に機械的に停止し、PoC・親Go判断なしのimplementation dispatchをinstruction遵守だけへ依存させない。

## Contract

- ACTIVE taskにcompactなexternal feasibility宣言を必須化し、missing・malformed・未検証のimplementation状態をmodel call前にfail closedする
- 最低限、外部service/runtime/producerのavailability・schema・field visibility・event timingがproduction correctnessまたは期待効果の前提になるcaseを表現できること
- `not-applicable`、PoC/observation段階、実producer evidence付きparent Go判断後implementationを区別し、PoC結果をGLMだけでGoへ昇格させない
- PoC/observation段階を許可する場合、production implementationを行えない境界をpromptだけでなく既存read-only/snapshot機構の再利用で強制する。成立しないなら別案をSol判断へ戻す
- implementation許可には実producer由来のevidenceと親Go判断を要求し、人工fixture・scripted packet・worker/reviewer PASSだけをevidenceとして受理しない
- resume/review/fixを含む全model dispatch entrypointで同じ受理集合を使い、runtime/installer/testで別parserを増殖させない
- 既存taskは一括churnせず、ACTIVE化前に親Codexが宣言をmigrationする。新規task生成contractは最初から同sectionを持つ
- 追加AI classifierや全taskの事前review callを導入せず、固定context・Codex/Sol消費増を最小化する

## Must not

- instruction文面、manifest pin、Task 015のcorpusだけでproduction enforcement済みとしない
- semantic applicabilityを脆いkeyword/regexだけで自動判定し、false negativeを保証済みとしない
- worker/reviewer checklist、新しいstate DB、generic policy frameworkを追加しない
- `not-applicable`宣言が誤っていても絶対防止できると主張しない。残余riskとreview責務を明示する
- 独立review follow-upや他taskの未コミットdiff・task stateへ混ぜない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- 現5時間limit taskと同型の「実producerが必要fieldを必要時点で公開するか未検証」のimplementation taskが、PoC evidenceと親Go判断なしではworker/reviewer model call 0回で拒否される
- PoC/observation taskはproduction diffを残せず、結果が親SolのGo/No-Goへ戻る
- 実producer evidence付きGo、external feasibility非該当の通常task、resume/review/fixの正当経路は通る
- task declaration省略・unknown値・evidence欠落・人工fixtureだけのimplementation許可はfail closedする
- declarationの通常固定context増分をbytes/token proxyで測定し、追加model call 0を維持する
- 全test/race/vet/build/gofmt、独立review、Sol最終採否、親Codex commit/install/source一致/production smokeを完了する
- Task 015がこのgateのproduction受理集合と親behavioral evidenceを評価し、Markdown存在だけをPASS根拠にしない

## Historical invariants

- 最上位目的はSol High相当の品質を維持しながらCodex/Sol実消費を大幅削減することであり、Direct Codex対Codex + glm-workerのCodex ReductionとQuality Deltaを正とする
- `codex/instructions/feasibility-gate.md`のPoC分離・意味的成功・親Go/No-Go契約を置換せずproduction enforcementへ接続する
- ACTIVE task fileが要求正本でありparent-managed metadataは親Codex専有とする

## Dependencies

none

## Review findings

- 既存`TestFeasibilityGateContractWiring`はinstruction/EVAL/manifestの存在整合だけを検証し、親が未検証implementationをdispatchするproduction pathを止めない
- EVAL自身がscripted packetは親行動の証明ではなくparent behavioral Eval未実行と明記していたが、親Codexは`cbf71c7`を完了受領した
- 現incidentの人工fixtureはstopperへexact本文を直接与える自己充足testであり、実`api_retry` eventのfield/timingを証明していなかった

## Current boundary

Task 008、5時間limit early-stop selective revert、task status machine enum follow-upは完了済み。本taskの最初のimplementation callはexternal-review follow-up受領時に停止したが、Claude childがorphan化してsource diffを書き続けたため、最終process group停止後のdiffをmessage identity付きstashへ保全した。現在はsafe interruption taskを先行し、その完了後にstash・元task要求・stateの整合を確認して本taskを再びACTIVEへ昇格し、Task 015より前にproduction dispatch gateを実装する。
