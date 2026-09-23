# Task: 親session rotationの再読・cache費用評価

## Original instruction

````text
特にCodex Reductionに貢献できそうな施策とか考えてほしい
````

親Codexが採用した独立評価要求:

````text
親session rotationのpending recommendationとclaim済みtransactionは異なる。rotationを減らせばtokenが減るとは断定できないため、trigger理由・再bootstrap・cache・同一task継続時の比較可能性から採否を決める。
````

## Amendments

none

## Resolved references

- 原要求全体・GLM不使用・後方互換性禁止・全件計画化は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の2026-09-22以後のAmendmentsを正とする。今回の成果物は計画化とPushであり、この評価の実行開始ではない。
- 対象: 既存session rotation projection、parent usage、rolloutのrotation前後境界。所有packageは実行時のcurrent source locatorで確認する。。
- `Review.md` の12観点・採否表・Codex Reduction優先判断が調査背景。本taskの契約はこのfile、実行priorityはPlanだけを正とする。

## Purpose

親session rotationの再読・cache費用評価をboundedな一次証拠で行い、品質を落とす最適化と効果不明の実装を避ける。

## External feasibility

status: not-applicable

## Contract

- 親Codex自身が既存artifactのread-only projectionで評価し、GLM・他モデルへレビュー/意味判断を委譲しない。production code・設定を編集せず、追加AI callを行わない。
- 既存evidenceを使い、同一要求・model/reasoning・作業種別の比較可能なcohortと独立session identityを明示する。
- rotation回数/trigger、直後のauthority/bootstrap再読量、cached/input/output、handoff失敗/再試行とQuality Deltaを分けて評価する。
- recommendationを断る判断とclaim後の保護を区別し、Go/No-Go/unknownを決める。閾値変更は評価だけで自動採用しない。

## Must not

- raw log全文やrepository全体を再探索・再投影しない。具体的な不足questionのexact locatorだけ追加確認する。
- old schemaのmigration・alias・推定fallbackを追加しない。
- 103-compaction-threshold-change.mdのBLOCKED条件を解除しない。
- claim済みhandoff保護やauthority bootstrapをtoken削減のために省略しない。

## Acceptance criteria

- rotationあり/なしの比較条件・交絡要因・欠落fieldを示し、比較不能ならunknownを保つ。
- 削減候補がある場合は責務・品質条件・必要な許可を示す独立実装Taskにし、効果未測定の閾値変更を行わない。
- 採用案は独立実装Taskへ固定し、不採用/unknownは根拠と再評価条件を残す。中間checkpointは本評価の結果を参照し同じ調査を反復しない。

## Historical invariants

- 最上位EvalはCodex ReductionとQuality Deltaであり、GLM token単独の削減を目的にしない。
- 実証されていない削減量を確定せず、後方互換性を持たせない。

## Dependencies

none
