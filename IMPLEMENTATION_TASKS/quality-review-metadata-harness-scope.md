# Task: 外部リポジトリのreview準備で固有名のmetadataを無条件に読まない

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

外部リポジトリのreview準備で固有名のmetadataを無条件に読まない。

## External feasibility

status: not-applicable

## Contract

workflow/review_flow.go の prepareReviewInputSnapshot は runQualityGate の前に state.CaptureParentFileStates を無条件に呼ぶ。state/parent_authority.go は固定名のRules・Plan・HistoryとTaskディレクトリを読む。opt-inしていない別プロジェクトで IMPLEMENTATION_RULES.md がディレクトリなら ReadFile が失敗し、review準備が停止する。正常な同名ファイルも毎回ハッシュされる。

## Must not

有効化判定は既存のruntime pinを使う。汎用runnerに新たなファイル名規則を追加しない。

## Acceptance criteria

harness無効で同名の通常ファイル・ディレクトリがあるfixtureを正規review入口に通す。無効時は固有metadataの読取を発生させず、有効時はfixer前後の変更を検知する。stop/resumeのboundary snapshotとinactive時の既存動作も照合する。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
