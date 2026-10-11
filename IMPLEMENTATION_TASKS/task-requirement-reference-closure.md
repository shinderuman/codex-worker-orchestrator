# Task: Task原要求の参照閉包とretirement

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
未完了Taskが共通原要求を削除済みTaskへ委譲したままになる問題を修正する。current requirementとhistorical locatorを区別し、必要原文の欠落を推測で埋めない。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の A5。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

未完了Taskが共通原要求を削除済みTaskへ委譲したままになる問題を修正する。current requirementとhistorical locatorを区別し、必要原文の欠落を推測で埋めない。

## External feasibility

status: not-applicable

## Contract

- cli-positive-task-start-admission.mdが参照するdogfood-bundle-controller-export.mdの必要原文を、実在するGit履歴・保存済み一次証拠から回収する。取得できない内容はunknownのまま明示し、実装前に親が必要なユーザー確認を判断する。
- immutable Original instructionを保持し、Resolved references/Amendmentsで必要原文または検証可能な固定sourceへ結び付ける。CLIが先行完了した場合も、要求を失わず完了した根拠を確認し、同じ欠落をretirementで再発させない。
- current requirementへの参照を持つ未完了Taskがあるとき、retirementは参照移管または明示された固定sourceの成立を確認する。通常historical locatorや完了記録までlive dependencyとして復活させない。
- 参照の機械判定可能部分を正規taskcontract/harnesslint側で扱い、自然言語の意味完全性は親が判断する。

## Must not

- 存在しない原文を生成してlossless回収と称さない。完了Taskの互換placeholderを復活させない。
- task名を含む全記述を一律hard dependency化しない。ordinary completionをHistoryへ複製しない。
- CLIのpositive admission contractをこのTaskで再設計しない。

## Acceptance criteria

- CLIの実装・reviewに必要な共通要求が独立に回収可能である。回収不能なら親判断待ちを明示し、成功扱いしない。
- current requirementの未解決参照を残すretirementを検出し、参照移管済み・正しく固定されたsource・historical-only参照を区別するfixtureがある。
- immutable source保持、tracked Markdown closure、related testsとquality gateを確認する。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
