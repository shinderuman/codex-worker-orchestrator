# Task: テスト時間の退行判定を集約する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B11. Focus test-timing regression derivation owner — #1627
> - **Owner/problem:** generic `controller/evidence_export_analysis.go` also contains ~326 lines of independent timing regression matching/ranking/comparability while timing-specific tests already exist.
> - **Historical rationale:** performance/test timing was added later to an expanding analysis-index assembler.
> - **Implement/minimum boundary:** same-package timing derivation/schema/selection helpers to a focused owner; leave generic index composition and shared validation-run authority as is.
> - **Preserve/risk:** exported index version/fields, run-ID collisions, pass-only eligibility, toolchain equality, baseline selection, threshold/order, missing evidence and deterministic results.
> - **Acceptance:** exact output and negative-case tests, timing-only read/review surface localized; no framework or LOC-only splitting.

## Purpose

テスト時間の退行判定を集約する。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

`analysisTestTimingRegression`以降の時刻比較・選別をindex組立から切り出す。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

同じrun ID、比較不能なtoolchain、欠損、閾値と順位の出力一致を確認。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
