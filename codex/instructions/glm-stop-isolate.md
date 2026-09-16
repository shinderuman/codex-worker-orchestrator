# GLM workerの安全停止・隔離・park

user interruption時のstop/resume、保持taskからのisolate、親判断待ちtaskのpark/unparkにだけ適用する。rate limit / provider recoveryはauto-resume契約を使う。

## semantic choice

親が判断するのは操作の意味だけである。

- 実行中taskをuser interruptionで止める: `stop`
- 同じ停止taskを続ける: `resume`
- 停止taskを保持して別taskを走らせる: `isolate`
- 親判断待ちtaskを保持して優先taskへ切り替える: `park` / `unpark`

これらを相互のfallbackとして使わない。どのtaskを止める・隔離する・parkするか、成果をどう統合しconflictを解決するかは親のsemantic責任である。

## machine-owned lifecycle

status admission、process cleanup、snapshot/dirty/ref検証、checkpoint/session保持、isolation/park provenance、idempotency、stale state、restore ordering、resume/unpark admissionは`control:stop-isolate-park-lifecycle`とcurrent production state machineを唯一のprocedure authorityとする。

親Codexはsignal順、PID、state field、retry matrix、branch/worktree照合条件をMarkdownから再構成しない。machineが拒否・stale・cleanup pending・provenance mismatchを返した場合はstateを保持し、返されたbounded evidenceに対応するsemantic/外部問題だけを直してcanonical actionへ戻る。

手動`kill`/`pkill`、checkpoint/stateの削除、branch/worktree recordの書換えでmachine admissionを迂回しない。

## residual external responsibility

- user interruptionで保持されたtaskは新規taskとして作り直さず、machine-owned retentionから再開する。
- untracked fileはmachineがidentity/hashを検証できても本文原本の復元元ではない。停止時内容そのものを外部に保持・復元する必要がある場合は親が所有する。
- isolate/parkした成果の統合、conflict解決、外部branch/worktree resourceの最終削除時機は親が判断する。ただしmachine lifecycleが保持を要求している間は削除しない。
- Plan/task authority、parent metadata、risk判断はrepository固有authorityを正とし、generic lifecycle stateから意味を推測しない。
