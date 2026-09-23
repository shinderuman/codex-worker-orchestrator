# Task: CLI installerの更新中断復旧

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexが採用した独立修正要求:

````text
CLI installerはbinary置換後・ownership state保存前の中断で、自身の更新を外部変更と判定し正規再実行を拒否する。current transactionの中断を安全に復旧できるようにする。
````

## Amendments

none

## Resolved references

- 共通の追加要求は `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md` の `2026-09-23 Finding保存の優先と計画化・Pushの要求` を参照する。今回は計画化とPushまでで、実装修正の開始ではない。
- 調査根拠は `Review.md` の F6。実装ownerは `glm-worker/internal/cliinstall/install.go の applyInstall / stageActions / commitActions / ownership state保存`。
- 再現資料: `review-evidence/cli_audit_test.go.txt`。対象test: `TestAuditCLIInterruptedUpgradeCanRetry`.

## Purpose

CLI更新の手動修復と親Codexの復旧介入を減らす。

## External feasibility

status: not-applicable

## Contract

- binary・backup・ownership stateの各書込境界を調べ、current transactionの適用済み範囲を復旧可能な形で扱う。
- 排他lockと中断復旧の責務を分け、復旧前に現物のownershipを検証する。復旧方式と永続状態の意味は実装前に親が確定する。
- Codex設定installerの別Taskと混同せず、このCLI配置ownerの正常retryとrollbackを成立させる。

## Must not

- manifest削除やownership照合の無効化でretryを通さない。
- OS電源断耐性をprocess終了testだけで証明したとしない。
- 旧version/schemaのmigration・promotion・alias・推定fallbackを追加しない。

## Acceptance criteria

- binary置換後・manifest保存前の子process終了に対し、通常Installの再実行で整合した配置へ到達できる。
- 途中のbinaryだけ更新済み、backup残存、外部編集が競合する場合を検証し、外部変更を上書きしない。
- 既存の排他・owned/unowned判定・通常Install/Retireの契約を維持する。

## Historical invariants

- current schemaだけを正規入力とし、後方互換性を持たせない。
- Codex ReductionとQuality Deltaを同時に扱い、未測定の削減率を確定しない。

## Dependencies

none
