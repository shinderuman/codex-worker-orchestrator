# Task: Claude settingsの同時編集保護

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
Claude settings mergeは読取後・write直前の管理外key編集を古い全体で上書きする。durable journalがあってもnormal applyの競合保護は成立しないため、このownerでユーザー編集を保存する。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F8。実装ownerは `glm-worker/internal/settingsmerge/merge.go の mergeFilesWithWriter と transaction.go の applyMergeTransactionPlans`。
- 再現資料: `review-evidence/merge_audit_test.go.txt`。対象test: `TestAuditMergePreservesConcurrentUnmanagedEdit`.

## Purpose

Claude設定消失と、それに伴う手動復旧・親介入を防ぐ。

## External feasibility

status: not-applicable

## Contract

- plan作成時の入力とnormal apply時の現物を照合し、競合はmutation前に拒否するか外部編集を保存する。
- 同一targetへの並行mergeを整合させ、normal apply・rollback・recoveryで同じ外部編集保護を維持する。
- 既存journalを活用し、lockでは排除できない任意editorとのraceと残る限界を明示する。

## Must not

- Codex TOML installerの修正だけで本ownerを解決済みとしない。
- journalの存在だけを編集保護の証明にしない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- 管理外user_keyをinitialからconcurrentへ変更する再現で、concurrentが保存されるか書込前に明示拒否される。
- rollback中・recovery前の追加編集を消さず、競合しないmanaged fragment更新は正常に完了する。
- 並行mergeとprocess中断の合成でtargetとjournalを不整合のまま成功扱いしない。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
