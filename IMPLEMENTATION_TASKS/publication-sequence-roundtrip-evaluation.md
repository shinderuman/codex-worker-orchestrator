# Task: publication定型stageの親往復削減評価

## Original instruction

````text
特にCodex Reductionに貢献できそうな施策とか考えてほしい
````

親Codexが採用した独立評価要求:

````text
publicationsequenceは次actionを型付きで示すprojectionでありexecutorではない。意味判断が不要な隣接stageの連続実行で親往復を減らせる可能性があるが、現時点で実task cohortによる削減効果は未証明である。boundedな既存証拠で費用対効果と採否を決める。
````

## Amendments

none

## Resolved references

- 原要求全体・GLM不使用・後方互換性禁止・全件計画化は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の2026-09-22以後のAmendmentsを正とする。今回の成果物は計画化とPushであり、この評価の実行開始ではない。
- 対象: glm-worker/internal/publicationsequence/sequence.go と parentactioncmd/publication*、既存parent usage/turn evidence。
- `Review.md` の12観点・採否表・Codex Reduction優先判断が調査背景。本taskの契約はこのfile、実行priorityはPlanだけを正とする。

## Purpose

publication定型stageの親往復削減評価をboundedな一次証拠で行い、品質を落とす最適化と効果不明の実装を避ける。

## External feasibility

status: not-applicable

## Contract

- 親Codex自身が既存artifactのread-only projectionで評価し、GLM・他モデルへレビュー/意味判断を委譲しない。production code・設定を編集せず、追加AI callを行わない。
- 同一taskのpublication stage・parent turn・tool output bytes・input/cache/outputの既知量を紐付け、modelの意味判断と機械手順の待機を区別する。
- 連続処理の候補を既存typed projectionの範囲で示し、snapshot・中断/retry・remote write authority・最終採否の境界を維持できるか評価する。
- 結果はGo/No-Go/unknownと根拠・実装owner・品質条件を明記する。Goの場合だけ独立実装Taskを作る。

## Must not

- raw log全文やrepository全体を再探索・再投影しない。具体的な不足questionのexact locatorだけ追加確認する。
- old schemaのmigration・alias・推定fallbackを追加しない。
- tool call数をそのままmodel turn数やquota消費として数えない。
- 巨大なfinalize API、新しいdaemon、remote writeの自動許可を先行実装しない。

## Acceptance criteria

- 比較対象と収集区間、stageごとの親介入理由、既知/未知のusageとexact locatorが明示される。
- 具体的な連続化候補について品質境界・費用対効果・採否を判断する。cohort不足ならunknownと必要観測だけを記録する。
- 採用案は独立実装Taskへ固定し、不採用/unknownは根拠と再評価条件を残す。中間checkpointは本評価の結果を参照し同じ調査を反復しない。

## Historical invariants

- 最上位EvalはCodex ReductionとQuality Deltaであり、GLM token単独の削減を目的にしない。
- 実証されていない削減量を確定せず、後方互換性を持たせない。

## Dependencies

none
