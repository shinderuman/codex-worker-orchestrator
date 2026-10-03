# GLM workerの安全停止とblocker切替

user interruption時のstop/resumeと、canonical controllerによるblocker切替に適用する。rate limit / provider recoveryはauto-resume契約を使う。

## semantic choice

親は停止理由、findingの採否、blocking boundary、再開するTaskを判断する。

- 実行中のmodel callをuser interruptionで止める: `stop`
- 保持した同じTaskを続ける: `resume`
- blockerを先に処理する: controllerのfinding dispositionとepisode scheduleを確定し、typed suspend/materializeを使う。

旧`isolate`・`park`・`unpark`は退役済みである。手動のPlan ACTIVE書換え、stash/reset、新たな復旧Taskやworktreeを代替経路にしない。

## machine-owned lifecycle

checkpoint/session保持、ExecutionLease admission、suspension、one-lane materialize、publication、cleanup、stale authority拒否は`control:stop-isolate-park-lifecycle`とcurrent production controllerをprocedure authorityとする。

親Codexはsignal順、PID、state field、retry matrix、branch/worktree照合条件をMarkdownから再構成しない。machineが拒否・stale・cleanup pending・provenance mismatchを返した場合は保持されたstateとbounded evidenceから問題を判断し、同じtyped operationへ戻る。

手動`kill`/`pkill`、checkpoint/stateの削除、branch/worktree recordの書換えでmachine admissionを迂回しない。

## residual external responsibility

- 停止Taskを新規Taskとして作り直さず、保存された要求とcanonical attempt lineageから再開する。
- controllerのRoot/Focus TaskとExecution Taskを区別する。Plan ACTIVEはfocusを保持し、worker/reviewerはcontrollerが束縛したExecution Taskを読む。
- restore conflictの解決内容とpublication evidenceの採否は親のsemantic責任とする。lane削除や再利用の時機と証拠保存はtyped cleanupのpostconditionで確認する。
- Plan/task authority、parent metadata、risk判断はrepository固有authorityを正とし、generic lifecycle stateから意味を推測しない。
