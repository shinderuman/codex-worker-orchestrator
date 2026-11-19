# Task: 読取が中断したstatusで古いイベントを最新と表示しない

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

読取が中断したstatusで古いイベントを最新と表示しない。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

**statusが途中のイベントを最新として返す。** `taskview/taskview.go::ReadLastTaskEvent` はscanner終了理由を確認しない。seq=1、上限4MiBを超える行、seq=3を順に置くとseq=1を成功として返す。`app/output.go::fillStatusTaskDetail` はそのphase/roleから進捗も投影する。

## Must not

実装時の現行入口で確認し、旧schemaの推定変換や二重の判定元を導入しない。

## Acceptance criteria

読取中断を呼出側に伝え、最新と確認できない値はpartial/unknownとする。長い行とその後の正常行、正常末尾、読取失敗を検証する。表示の不具合として扱い、実行状態を変更しない。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
