# Task: Watch terminal-error orphan exit

## Original instruction

````text
glm処理中というわけでもなく永久に返ってこないコマンドの結果を待っているように見えたので止めた
作業再開しろ
````

## Amendments

none

## Resolved references

- 2026-09-06、task `1c279537-b51f-4c33-b89a-3309408390ea`のexplicit-fixは実行環境のPATHに`claude`がなくexit 127で終端した
- recovery handoffは`task_status=active`・`required_action=none`・`last_material.outcome=error`を返した
- 契約に従ってread-only `glm-worker --watch`へattachしたが、model processが存在しないまま約23分終端せず、ユーザー中断が必要になった

## Purpose

terminal error後に実行processが存在しないactive stateへ`--watch`した際の無期限待機を機械的に防ぎ、Codexの不要な停止と手動介入を削減する。

## External feasibility

status: not-applicable

## Contract

- `--watch`は対象repositoryのtask/liveness/stateを機械判定し、実行processへattach可能な場合だけ待機する
- activeでも最新material outcomeがterminal errorでrepository lockがfreeかつlive ownerがない状態は、待機せずbounded machine errorまたは正規resume actionを返す
- raceがある場合は誤ってrunning processをstale扱いせず、既存repository lockとtask livenessを正として収束する
- handoff recoveryとwatchが同じ状態を矛盾して表現せず、親が自由言語でorphan判定しない

## Must not

- 経過時間だけのtimeoutで正常な長時間model処理を中断しない
- global process一覧や別repositoryのprocessを生存判定に使わない
- active stateを自動reset・deleteしない
- error後のworker/reviewer sessionとresume checkpointを破棄しない

## Acceptance criteria

- active・latest outcome error・repository lock free・live ownerなしfixtureで`--watch`がmodel call 0回かつbounded時間で終端する
- activeでlive ownerありfixtureでは既存watch attachを維持する
- lock取得/解放raceとstale ownerのtestがある
- terminal resultがcanonical next actionまたはrecovery locatorをmachine-readableに返す
- Codexが無期限blocking waitから手動中断する必要がないことをintegration testで固定する
- independent reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- 意味のある状態遷移がない通常のrunning処理は最大blocking waitで待ち、liveness報告のために親へ戻らない
- GLM worker/reviewerにGit remote write authorityを付与しない

## Dependencies

none

## Review findings

none

## Current boundary

未着手。2026-09-06のqsurface explicit-fix transport error後にproduction相当の`--watch`で再現した。
