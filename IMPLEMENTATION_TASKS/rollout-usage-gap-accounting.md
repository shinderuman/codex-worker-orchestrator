# Task: 親Codex消費量の中間欠測を集計に残す

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

親Codex消費量の中間欠測を集計に残す。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

**親Codex消費の集計で観測欠損が消える。** `bundleanalysis/bundle_analysis_tokens.go::analysisTokenSegments` はanchorのない中間ファイルを飛ばす。独立counterの3ファイルをreaderへ入力。中間にはtool callがありtoken観測がなく、前後にのみanchorがある。結果は `available`、total 13、unknownなしだった。

## Must not

実装時の現行入口で確認し、旧schemaの推定変換や二重の判定元を導入しない。

## Acceptance criteria

実行区間をまたぐ未観測メンバーを部分観測として返す。その区間の全量を復元できるcounter継続証拠がある場合だけknownとする。中間欠損、リセット、継続、単一ファイルを比較する。実消費値や削減率を推定で補わない。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
