# Task: 機械用Git差分のraw content契約

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
監査用exportとsnapshot/baseline/taskdiffで、外部diff driver・textconvによる副作用や実変更の欠落を除去する。機械証拠を表示用driverへ委譲しない。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の A1/A2。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

監査用exportとsnapshot/baseline/taskdiffで、外部diff driver・textconvによる副作用や実変更の欠落を除去する。機械証拠を表示用driverへ委譲しない。

## External feasibility

status: not-applicable

## Contract

- controller exportの外部diff実行と、state snapshot/baseline・taskdiff・controller identity・retentionのtextconv許可を同じraw patch契約で修正する。共通化は依存方向を崩さない最小ownerとし、汎用Git実行基盤の新設を目的にしない。
- 並行Bundle修正がexport側を解消済みなら、その正規経路のregression coverageを確認して重複変更を省く。snapshot/taskdiff側の未解決を同時に解消したと推定しない。
- trustedなrepositoryの通常textconv設定でも発生する同一性違反として扱い、悪意ある設定だけの問題へ狭めない。

## Must not

- external diff/textconvへの互換経路、旧snapshotの推定再利用を追加しない。
- ユーザーのGit設定を削除・書換えして回避しない。read-only exportへ別の副作用を追加しない。
- 単体再現をend-to-end誤acceptの実証と説明しない。

## Acceptance criteria

- GIT_EXTERNAL_DIFFとrepository driverのcontrolled fixtureで、正規exportがdriverを起動せず実変更の標準patchを残す。
- 定数を返すtextconvを設定してtracked bytesを変更しても、snapshotが変化しtask patchがavailable=true/空へ縮退しない。staged/unstagedとbaselineからの差分を検証する。
- 機械用Git差分の利用ownerを確認し、同じcontractの抜けを残さない。変更対象のregression testとrepository quality gateを通す。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
