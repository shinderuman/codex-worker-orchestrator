# Task: Codex wake inventoryの所有scope分離

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
wake準備は全automationへwake専用readerを適用し、無関係なautomationの表示名とdirectory名の差やtarget_thread_id欠落だけで停止する。候補選別とwake固有identity検証を分離する。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F15。実装ownerは `glm-worker/internal/autoresume/codex_wake_transaction.go の resolveCodexWakeAutomation、coalesce.go の readWakeTOML、toml.go の validateAutomationFields`。
- 再現資料: `review-evidence/wake_audit_test.go.txt`。対象test: `TestAuditUnrelatedAutomationDoesNotBlockWake`.

## Purpose

無関係なユーザーautomationによるLimit後の再開準備の停止を防ぐ。

## External feasibility

status: not-applicable

## Contract

- 汎用inventoryから対象threadの候補を選別し、その候補に対してだけwakeの厳密な命名・identity検証を行う。
- 無関係なcronや別threadのheartbeatを許容し、同じtargetの重複・曖昧な所有権・不整合は引き続き拒否する。
- 読取不能な候補と無関係と確定したentryを区別し、安全性と無用な全体停止を両立させる。

## Must not

- 無関係なautomationをrename・削除・書換えしない。
- 旧automation schemaを推測で補完する互換層を作らない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- daily-checkに表示名Daily checksがある別threadのautomationがwake準備を妨げない。
- target_thread_idを持たない無関係なcronを置いても正常に候補を解決する。
- 同一targetの複数候補・誤ったwake ID・対象file破損の拒否と、正しい既存wakeの再利用を検証する。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
