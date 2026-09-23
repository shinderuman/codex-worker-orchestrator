# Task: A/B評価の欠落token値の拒否

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
CodexUsageはsourceがあればknownとし、欠落/nullのinput/outputを整数0へdecodeするため、未観測のorchestrated usageがactual 100%削減として報告される。明示0と不明を区別する。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F12。実装ownerは `glm-worker/internal/abeval/abeval.go の CodexUsage / Known、validate.go の LoadRecord / validateCodexUsage、compare.go の codexReduction`。
- 再現資料: `review-evidence/usage_audit_test.go.txt`。対象test: `TestAuditMissingUsageCannotBecomeActualReduction`.

## Purpose

最上位EvalのCodex Reductionが未観測値を改善として計上することを防ぐ。

## External feasibility

status: not-applicable

## Contract

- current schemaのknown usageは必要数値fieldの存在・非nullを検証し、不完全な記録は拒否またはunknownとして扱う。
- input/output各々の欠落を扱い、明示0を欠落と混同しない。baselineの分母0も定義不能として扱う。
- reader・validation・reportの間でunknownがactualへ変換されない契約を統一する。

## Must not

- 旧記録の欠落を0で補う互換処理を入れない。
- cache/reasoning fieldの追加やquota換算へ本修正を拡張しない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- sourceだけ、両値null、片方欠落/nullの入力からactualの削減率が生成されない。
- 明示0を含む有効な観測、正常な削減率、増加時の負率、未知sourceの拒否を確認する。
- production LoadRecord→ValidatePair→BuildReportを通して再現fixtureの誤計上を解消する。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
