# Task: 正規Lifecycleの実装・親metadataコミット集約

## Original instruction

````text
現在のTaskとは別に、次の目的のTaskをNEXTへ登録する。

1 Task＝1実装コミット、関連する親metadata更新も合理的に1コミットへ集約できる正規Lifecycleの整備。

このTaskでは、GitHub PRを必須にせず、既存のController／GitによるPublication・Retireを前提に、最適なコミット構成と運用を評価・実装する。

不要な中間コミットを生成しないこと、ユーザーによる事後的なSquash・Force Pushを必要としないことを目的とする。

今回のACTIVE Taskへその改善実装を混ぜない。既存NEXTの順序も勝手に変更しない。
````

## Amendments

### 2026-10-13 Plan・Task整理と実行境界（ユーザー原文の本Task該当節）

````text
`normal-lifecycle-commit-aggregation.md`についても、その登録根拠と現在のユーザー要求との整合性を確認する。今回の整理を口実に実装したり、優先順位を引き上げたりしない。削除すべきか判断できない場合は、登録を維持して報告する。

**Taskの整理に便乗して、新しいTask、追加仕様、設計課題、改善案を増やさない。**

このセッションでは実Taskを開始しない。
````

既存の独立Task登録要求を維持する。今回のmetadata Amendは、この後続Taskの評価・実装の開始や通常Lifecycleでの集約成功を意味しない。

## Resolved references

- Original instructionの「現在のTask」は登録当時の旧Taskを指し、Git履歴 `c6c47b6e9dd1c6e334c96515c99d95ba5f2813c4:IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` を `git show c6c47b6e9dd1c6e334c96515c99d95ba5f2813c4:IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` で取得する。旧Taskの削除・スケジュール除外は正常完了や成功済みdependencyを意味しない。未達Workflowの引継ぎ先は `IMPLEMENTATION_TASKS/execution-worktree-architecture-review.md` であり、本Taskへ移管しない。
- 既存の独立登録要求を維持し、実行順序はPlanだけを正とする。今回のmetadata整理を調査・実装開始と扱わない。

## Purpose

実装・親metadataの意味とauthorityを維持し、不要な中間コミットとユーザーによる事後的なSquash／Force Pushを必要としないコミット運用を正規Lifecycleへ整える。

## External feasibility

status: not-applicable

## Contract

- 既存Controller／GitのPublication・Retireを前提とし、GitHub PRを必須化しない。
- 1 Task＝1実装コミットと、関連する親metadataの合理的な1コミットへの集約を、authority・snapshot・evidence・Publication・Retireの不変条件に基づいて評価・実装する。
- metadata編集とGit確定を分け、準備・状態更新・整形・検証・後処理による不要な中間コミットを生成しない。
- 現行の不可避なコミット境界と不要な分割を実コード・履歴・正規transitionで区別し、必要な変更だけを実装する。
- GLMへ調査・実装・検証と独立Reviewを委譲し、architecture／永続状態／公開API／authorityの意味判断をSolへ戻す。
- current schema／contractのみを扱い、共通の後方互換性禁止を遵守する。

## Must not

- Worktree Taskへ引き継いだPublication・Retire等の機能的欠陥を本Taskのコミット集約へ先送りしない。登録のみを調査・実行開始として扱わない。
- 本Taskの登録・整理を理由にPlanの実行順序を変更しない。
- commit数の削減のためにController authority、immutable evidence、Guard、Publication・Retireを迂回しない。
- 既存成果・歴史的candidate・証拠を破壊せず、履歴書換えやForce Pushを通常運用の前提にしない。
- GLMによるGit commit／remote write／parent-managed metadata編集を許可しない。
- 旧schemaの互換fallback・alias・migrationや第二authorityを追加しない。

## Acceptance criteria

1. 実装・metadataのコミット境界を、その役割・不可避性・数と正規authority遷移で説明できる。
2. 1 Task＝1実装コミットと、関連metadataの合理的な集約が正規Lifecycleで成立する。
3. 不要な中間コミットや事後的なSquash／Force Pushを要求しない。
4. GitHub PRなしでも既存Controller／GitのPublication・Retireを完遂できる。
5. 必要な実装・独立Review・Validationを実施し、正常・拒否・中断再開の不変条件を検証する。

## Historical invariants

- 旧Taskの一回限定復旧や例外Git操作、今回のmetadata Amendを、本Taskの正常経路の成功証拠へ転用しない。

## Dependencies

none
