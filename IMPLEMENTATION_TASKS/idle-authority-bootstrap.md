# Task: 新規依頼のauthority取得を正当なidle状態から始められるようにする

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

新規依頼のauthority取得を正当なidle状態から始められるようにする。

## External feasibility

status: not-applicable

## Contract

README.md は全Task完了後からも登録可能とし、repositoryproject/plan.go はValidateIdleを許容する。codex/AGENTS.md は開始時のauthority bootstrapを必須にするが、authoritybootstrapcmd/run.go のloadは必ずActiveTask()を要求し、ACTIVEとNEXTが空の監査対象HEADで失敗した。controller/registered_task.go の登録済みTask開始はACTIVE登録後の入口。

## Must not

壊れたscheduleは拒否し、ACTIVEがある場合のrules・plan・activeの同一snapshot検証を保つ。

## Acceptance criteria

idleと壊れたスケジュールを区別し、前者で登録のために読む必要のあるauthorityを取得できる契約を定める。既存ACTIVEでの同一snapshot束縛を保持し、空の架空Taskや既存ファイルの直接読取による代用を導入しない。新しい依頼の通常入口から再現する。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
