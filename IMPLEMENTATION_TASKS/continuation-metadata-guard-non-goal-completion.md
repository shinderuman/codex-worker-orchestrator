# Task: continuation metadata guard non-goal completion

## Original instruction

````text
それは不具合なの？不具合ならタスクとして起票するんじゃないの？
お前がやったことは2個問題がある
不具合をタスクとして起票しなかったこと
「不具合をタスクとして起票する」というルールを守らなかったこと
これら2件を起票しろ
````

## Amendments

none

## Resolved references

- GOAL節を持たない通常Planで、完了task file削除とNEXTのACTIVE昇格をstageしたcommitが`.githooks/pre-commit`の`glm-parent-action continuation-metadata-guard`により`state=unknown reason=continuation-scope-unbound completion_admitted=false stop_admitted=false`として拒否された
- `glm-parent-action complete`はmetadata commit前には`completed_task_file_still_tracked`、metadataをstageした後には`tree_not_clean`を返し、正規経路だけでは完了同期をcommitできない循環状態になった
- current implementationの`repositoryproject.PreparePostCompletion`はGOAL節がないPlanを`PostCompletionUnbound`へ分類し、post-completion scheduleに一意なACTIVEが存在してもcontinuation metadata guardがcommitを許可しない
- `--no-verify`によるguard回避は実施していない

## Purpose

GOAL節がない既存Planでも、ordinary taskの完了metadata同期を正規経路でcommitし、次のACTIVEを開始せずに確定可能にする。

## External feasibility

status: not-applicable

## Contract

- GOAL節がないPlanで、完了task file削除とNEXT先頭のACTIVE昇格からなる正当なpost-completion metadata transitionを判定可能にする
- pre-commit guard、`glm-parent-action complete`、canonical handoffの責務と順序を循環させず、parentがbypass用flagやhook無効化を使わず完了同期できるようにする
- userが「現在task完了後はNEXTを開始せず停止」と指定した場合も、metadata上のACTIVE昇格とruntime上のtask開始を区別し、完了同期自体を拒否しない
- GOAL modeのcontinue / blocked / terminal判定、active-task mismatch、inconsistent handoff、未承認stopに対する既存fail-closedを維持する
- non-GOAL continuationの公開CLI / handoff contractと停止admissionの意味は実装前に`NEEDS_SOL_DECISION`で確定する

## Must not

- `git commit --no-verify`、hook削除、環境変数によるguard bypassを正規解決にしない
- GOALなしPlanを無条件にterminalまたはcompletion-admittedとして扱わない
- NEXTのACTIVE昇格をNEXT taskの実行開始と同一視しない
- inconsistent schedule、欠損task、複数ACTIVEを成功へ縮退しない
- userの停止指示をtask fileのsemantic requirementへ混入させない

## Acceptance criteria

- GOALなしPlanのordinary completionで、完了task削除、NEXT先頭のACTIVE昇格、metadata commit、`glm-parent-action complete`が正規順序で成功するintegration scenarioを固定する
- 同scenarioで新ACTIVEのworker/model callが自動開始されないことを確認する
- GOALなしPlanでNEXTあり / BLOCKEDのみ / schedule不整合の各continuation結果とcommit admissionを固定する
- GOAL modeのcontinue / blocked / terminal、およびactive-task mismatch / inconsistent handoffの既存testが維持される
- pre-commit guardとcomplete commandが同一のcanonical projectionを使用し、相互にclean treeを要求してdeadlockしないことをtestで固定する

## Historical invariants

- ordinary task完了時は完了task fileを削除し、PlanのNEXT先頭をACTIVEへ昇格する
- metadata上の次ACTIVE確定は、そのtaskのruntime開始を意味しない
- continuation guardを迂回せず、machine-admittedな正規経路を使用する

## Dependencies

none
