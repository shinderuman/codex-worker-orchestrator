# Task: System-One の限定production adoptionに向けた実測gate

## Original instruction

````text
今回のSystem-One Dogfood shadow evaluationは、PoCを作って終了すること自体を目的にしない。

このTaskの結果がGo判定に足る場合は、Codex Reductionへ実際に寄与させるため、直後に通常経路への限定的なproduction adoptionへ進めること。

方針:

- 現Taskの「PoCからproduction filtering/routingへ自動昇格しない」という安全境界は守る。現Taskの中で勝手にproduction化しない。
- ただしGo判定だった場合、「後で検討する」で止めない。production adoptionを明示的な後続Taskとしてtracked化し、可能ならNEXTとして直ちに進められる状態にする。
- 最初から全面適用する必要はない。実測上効果があり、低risk・高頻度で、失敗時の影響範囲を限定できるdecision classまたは経路を最初のadoption対象に選ぶ。
- production adoption後も、canonical Sol判断との比較、Quality Delta、false negative、coverage、Codex/Sol token reduction、failure modeを計測可能にする。
- 品質を維持できてReductionが実測できた範囲から、次のdecision class / execution pathへ段階的に拡張する。
- shadowを長期間眺め続けること自体を目的化しない。十分なevidenceが得られたら、限定production → 実測 → 拡張のループへ移る。
- 全体展開より限定展開が適切なら限定展開を優先する。ただし「限定だからそこで終了」ではなく、次に拡張できる判断材料を残す。
- Sol Highは曖昧・高risk・高レバレッジなsemantic tailへ集中させ、低価値で反復的なsemantic judgmentをSystem-One側へ移す、というCodex Reductionの最上位目的を維持する。

現Task終了時には最低限、

1. Go / No-Go判断
2. 根拠となるQuality Delta / reduction実測
3. Goなら最初にproductionへ伝播させる具体的な経路
4. そのadoptionを行う後続Task\
   を残すこと。

PoC成功なのにDogfood専用shadow evaluatorを作っただけで終了し、Codex/Solの通常実消費削減へ繋がらない状態にはしないこと。
````

## Amendments

none

## Resolved references

- `IMPLEMENTATION_TASKS/system-one-dogfood-evidence-shadow-eval.md` の2026-09-24隔離PoCは実Z.aiで10件のtyped schema出力を得た一方、既知Sol labelは2件、評価した閾値の削減候補は0件。production adoptionは現時点でNo-Goであり、本taskはこの不足証拠を解消する限定的な次段である

## Purpose

高頻度で低riskのsemantic decision classを見つけ、System-Oneが品質を維持しながら通常のCodex/Sol実消費を減らせるかを、期限と試行上限のあるshadow実測で判定する。

## External feasibility

status: observation
assumption: 代表的な通常経路で、正解label付きの高確信noise候補が十分発生し、false negativeを許容範囲に保ったままCodex/Sol token削減を実測できること

## Contract

- 実行開始時に対象decision class、代表bundle、試行上限と終了条件をSol/親が固定する
- canonical Sol判断をreferenceとして、false negative、Quality Delta、coverage、latency/cost、追加GLM消費、Codex/Sol inputと実消費の差分を計測する
- 未知labelを正解扱いせず、十分なsignalが得られなければNo-Goで終了する
- Goの場合だけ、最初の低risk・高頻度・影響範囲限定の通常経路、失敗時のfallback、監視方法を定め、実装用の独立Taskをtracked化する

## Must not

- shadow出力へproduction filter/routing authorityを与えない
- 単発のschema成功や自己申告confidenceをQuality Delta維持の証明にしない
- Goにならないまま観測を無期限に継続しない

## Acceptance criteria

- 代表的な実Dogfood evidenceとcanonical Sol labelで、選定したdecision classのQuality Deltaと実削減可能量が計測される
- Go/No-Goと根拠が明示され、Goの場合は通常経路の限定adoption実装TaskがPlanへ登録される
- No-Goの場合は試行上限内で撤退理由が確定する

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta
- Sol Highは曖昧・高risk・高レバレッジなsemantic tailへ集中させる

## Dependencies

none
