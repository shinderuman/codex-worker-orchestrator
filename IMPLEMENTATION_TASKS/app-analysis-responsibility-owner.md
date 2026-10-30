# Task: appの履歴集計を分析ownerへ集約する

## Original instruction

2026-10-29 親Codex採用要求:
`telemetry_summary.go`のschema cohort選択・outlier集計・parent usage合成を既存report/bundleanalysisの責務へ閉じ、appをquery入力・JSON出力へ限定する。新指標追加のたびにappへ分析処理と巨大fixtureを追加する構造を解消する。

## Amendments

none

## Resolved references

- 評価根拠: `review-evidence/codex-efficiency-checkpoint-2026-10-29.md`のinternal/app節。
- bundleanalysisへの既存移管は`review-evidence/workflow-performance-result.md`を参照し、再実装しない。

## Purpose

CLIと分析の責務を分離し、指標追加の変更範囲と検証費用を抑える。

## External feasibility

status: not-applicable

## Contract

- report/bundleanalysis/stateの現ownerを確認し、集計policyを一つのownerに置く。appへcallbackや同じ判定を残さない。
- query/filter、schema別除外、unknown、outlier上限、parent usage帰属、単一JSON/error契約を保持する。
- 集計単体testはownerで軽量な入力を使い、appにはparse/dispatch/出力と必要な接続testを残す。

## Must not

- ファイル移動、test削減数、行数だけを改善としない。新schema、互換層、集計cache、別stateを増やさない。
- 自己確認型テストの全体整理やSystem-One機能改変を混ぜない。

## Acceptance criteria

- 指標・cohort policy変更がapp実装の変更を要求しない責務境界を示す。
- current schema、異schema、欠損usage、期間境界、上限・errorの挙動が移管前後で一致する。
- appに重複集計とowner単体testの重複が残らず、実CLI接続を検証する。

## Historical invariants

- unknownを0や改善へ変換しない。parent semantic gateを維持する。

## Dependencies

none
