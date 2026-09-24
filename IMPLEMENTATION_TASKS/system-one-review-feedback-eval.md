# Task: System-One review取りこぼしのCost/Quality改善評価

## Original instruction

````text
現ACTIVE taskは現在のscopeのまま進めてよい。今回のtaskへ追加実装を混ぜないこと。

ただし今回のSystem-One Dogfood shadow evaluationがGo判定になった場合、その後続Taskではproduction adoptionだけでなく、今回観測されたGLM review → Sol review間の取りこぼしもCodex Reduction対象として考慮すること。

今回、GLM reviewerがfixable issueなしとして通した後にSolが追加で以下の重要findingを検出している。

- reduction candidateのsemantic条件不足
- provider/schema failure時にもreductionを成功値として計上できる問題
- external model invocationのbounded deadline欠如
- provider failure時のpartial structured output永続化

この差分を単に今回のworker fixで閉じず、「なぜGLM reviewerで捕捉できずSol判断まで残ったか」を後続改善のevidenceとして扱うこと。

Go判定後のfollow-upでは少なくとも以下を検討する。

- generic reviewer一段だけで十分か
- high-risk / cross-cutting / routing / lifecycle / metric変更等に限定してadversarial reviewerを追加する価値があるか
- 通常reviewerとadversarial/failure-path reviewerで責務を分けるべきか
- Solが一度発見してsemantic requirementが確定したfinding classをtest / lint / harness等のdeterministic gateへ移せないか
- second reviewer追加によるGLM消費増と、それによって削減できるSol/Codex消費を実測比較できるか

単純にreviewer数を常時増やすことを目的にしないこと。追加GLM costの方が大きければReductionにならない。

狙いは、

Worker\
→ deterministic gate\
→ GLM review\
→ 必要な場合のみadversarial review\
→ Sol semantic tail

のように、Solが現在拾っている反復可能なreview findingを順次machine / GLM側へ移し、Solを本当に曖昧・高risk・高レバレッジな判断へ集中させること。

今回のSystem-One PoCが成立した場合は、

1. Dogfood外への限定production adoption
2. Sol findingからGLM review flowへのfeedback
3. mechanize可能なfinding classのdeterministic化
4. reviewer追加コストを含む実際のCodex/Sol Reduction測定

を、必要に応じて独立したtracked follow-up Taskとして繋げること。

現ACTIVE taskのscopeをこれらの追加検討で膨らませないこと。
````

## Amendments

none

## Resolved references

- System-One shadow evaluator実装中、独立GLM reviewerのfixable issueなし判定後にSolが上記4件およびtyped schema required数値field欠損の受理を検出した。各findingの現物は該当taskのGit diff、review packet、fix packet、validation evidenceを正とする

## Purpose

GLM reviewの反復可能な見落としを、追加GLM costがSol/Codex削減を上回らない範囲でmachine gateまたは条件付きreviewへ移せるか評価する。

## External feasibility

status: not-applicable

## Contract

- 上記findingを起点に、通常reviewerで捕捉できなかった原因層を検証する
- deterministic gate化できるfinding classと、high-risk等に限定したadversarial/failure-path reviewerが必要なclassを分ける
- reviewerを常時増やす案と条件付き案のGLM追加消費、Sol/Codex削減、Quality Deltaを同じevidenceで比較する
- 実装が必要な独立責務はGo判断後に別Taskへ分ける

## Must not

- reviewerを常時追加すること自体を成果としない
- 追加GLM costが削減量を上回る案をCodex Reductionと呼ばない
- 現ACTIVEのSystem-One shadow evaluator実装へ変更を混ぜない

## Acceptance criteria

- 見落とし原因、機械化候補、条件付きreview候補、追加cost対Codex/Sol削減の実測比較が揃う
- Go/No-Goと、Goの場合の実装用tracked Taskが確定する

## Historical invariants

- Sol Highは曖昧・高risk・高レバレッジなsemantic tailへ集中させる
- 最上位EvalはCodex ReductionとQuality Delta

## Dependencies

none
