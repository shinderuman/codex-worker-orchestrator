# Task: observation準備処理への取消伝播

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
observationの取消/deadlineをpreflightとmodule copyへ伝播し、取消済み要求で新subprocessを起動しない。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の A6。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

observationの取消/deadlineをpreflightとmodule copyへ伝播し、取消済み要求で新subprocessを起動しない。

## External feasibility

status: not-applicable

## Contract

- RunIsolatedGoTest入口、confinement preflight、copy loop、target実行を同じcontext/deadlineで制御する。
- subprocessとその子processの停止・回収、copy途中の一時artifact cleanupをboundedにする。file/byte上限をcontextの代替にしない。
- supported platformごとの実装を確認し、DarwinのcontextなしCombinedOutputだけ直して他の準備段階を残さない。

## Must not

- sandboxの緩和、host cwdでの代替実行、取消無視の再試行を追加しない。
- 実際のtarget実行前の失敗をtarget test結果へ昇格しない。旧取消動作との互換モードを作らない。

## Acceptance criteria

- 取消済みcontextではcontrolled preflight helperのmarkerも作られない。
- preflight中、copy中、target実行中の取消/deadlineでboundedに戻り、対象processが残らないことをcontrolled fixtureで検証する。
- 正常実行とconfinement失敗の意味・evidenceを保持し、related testsとquality gateを通す。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
