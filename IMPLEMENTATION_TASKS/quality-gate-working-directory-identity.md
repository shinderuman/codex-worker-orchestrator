# Task: quality gate共有実行のWorkingDir identity

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
同時quality gateの共有identityにWorkingDirがなく、同一repository/snapshotのmodule-b要求がmodule-aのgo testへattachして誤ったPASSを返す。検証対象が同じ実行だけを共有する。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F14。実装ownerは `glm-worker/internal/app/quality_gate_execution.go の startQualityGate と quality_gate_persistence.go の findRunningQualityGateRun / sameQualityGateSnapshot`。
- 再現資料: `review-evidence/gate_identity_audit_test.go.txt`。対象test: `TestAuditGateCoalescingPreservesWorkingDirectory`.

## Purpose

重複実行を減らしながら対象moduleの検証漏れと後段の拒否・再実行を防ぐ。

## External feasibility

status: not-applicable

## Contract

- 正規化したWorkingDirを実行identityへ含め、form・repository・snapshot・対象が一致するrunning gateだけを共有する。
- symlink経由など同じ実体と、別module/packageなど異なる実体を区別する。identity解決失敗を成功扱いしない。
- worker側の返却WorkingDir再検証を残し、共有runの出力・保存record・待機対象を一致させる。

## Must not

- 共有を全廃して正規の重複実行を増やすだけにしない。
- WorkingDirの欠落を推測して既存runへattachしない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- 同一repo/snapshotのmodule-a実行中にmodule-bを要求すると別runnerでBが検証される。
- 同じWorkingDirの並行要求は引き続き1つのrunning gateへ合流する。
- 別form・別snapshot・不正WorkingDirの拒否を維持し、BへAのPASSを返さない。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
