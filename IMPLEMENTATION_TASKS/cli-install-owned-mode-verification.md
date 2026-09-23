# Task: CLI owned executableのmode検証整合

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
正規Installしたowned binaryを0777へ変更するとVerifyは成功するがInstallは拒否する。Verifyを正規ownership条件と一致させる。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F7。実装ownerは `glm-worker/internal/cliinstall/verify.go の verifyExpectedBinary と install.go の requireOwnedTarget`。
- 再現資料: `review-evidence/cli_audit_test.go.txt`。対象test: `TestAuditCLIVerifyRejectsOwnedModeDrift`.

## Purpose

更新不能なCLI配置をruntime検証の成功として扱わない。

## External feasibility

status: not-applicable

## Contract

- owned binaryのfile種別・mode・hash条件をInstall/Verify/Retireで整合させる。
- 同一内容のunowned executableを許容する既存契約とowned stateの厳密検証を区別する。

## Must not

- Verifyでmodeを黙って修復しない。
- 観測していない第三者の悪用を実証済みとしない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- 正規Install後のchmod 0777をVerifyが明示拒否し、0755の正常owned binaryは成功する。
- 非regular・symlink・content変更と既存unowned同一内容の扱いを対象別に確認する。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
