# Task: A/Bと親usageの測定粒度・比較可能性評価

## Original instruction

````text
特にCodex Reductionに貢献できそうな施策とか考えてほしい
````

親Codexが採用した独立評価要求:

````text
A/BのCodexUsageはinput/output中心だがparent usageはcache/reasoning/unknownと区間帰属を持つ。同じ最上位Evalへ使う前にfieldの意味・比較境界・欠損の扱いを対応付け、粒度統一の要否を決める。
````

## Amendments

none

## Resolved references

- 原要求全体・GLM不使用・後方互換性禁止・全件計画化は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の2026-09-22以後のAmendmentsを正とする。今回の成果物は計画化とPushであり、この評価の実行開始ではない。
- 対象: glm-worker/internal/abeval と parent usage/report owner、F12の欠落値拒否Task。
- `Review.md` の12観点・採否表・Codex Reduction優先判断が調査背景。本taskの契約はこのfile、実行priorityはPlanだけを正とする。

## Purpose

A/Bと親usageの測定粒度・比較可能性評価をboundedな一次証拠で行い、品質を落とす最適化と効果不明の実装を避ける。

## External feasibility

status: not-applicable

## Contract

- 親Codex自身が既存artifactのread-only projectionで評価し、GLM・他モデルへレビュー/意味判断を委譲しない。production code・設定を編集せず、追加AI callを行わない。
- Direct Codexとorchestratedの同じ要求・source snapshot・model/reasoning・計測開始終了条件を比較条件とし、独立run identityを保持する。
- input/output/cached/reasoning/totalの包含関係、counter reset、execution/finalization区間、unknown/ambiguousの表現を一次証拠に基づき整理する。
- 現行fieldで比較可能な指標と不足分を区別し、最小の観測/schema変更が必要なら親が意味を確定する独立Taskへ分離する。

## Must not

- raw log全文やrepository全体を再探索・再投影しない。具体的な不足questionのexact locatorだけ追加確認する。
- old schemaのmigration・alias・推定fallbackを追加しない。
- F12の欠落値を0にする不具合修正を本評価の大きなschema拡張待ちにしない。
- token数を利用枠/料金へ推測換算したり、旧schemaからcache/reasoningを補完しない。

## Acceptance criteria

- 各metricのsource・単位・重複計上の有無・欠落条件・比較可否を示し、unknownが改善へ混入しない。
- 粒度統一のGo/No-Goと費用対効果を判断する。既存証拠不足なら不足箇所を限定し、新しい本番A/Bを勝手に実行しない。
- 採用案は独立実装Taskへ固定し、不採用/unknownは根拠と再評価条件を残す。中間checkpointは本評価の結果を参照し同じ調査を反復しない。

## Historical invariants

- 最上位EvalはCodex ReductionとQuality Deltaであり、GLM token単独の削減を目的にしない。
- 実証されていない削減量を確定せず、後方互換性を持たせない。

## Dependencies

none
