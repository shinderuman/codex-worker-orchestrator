# GLM workerの安全停止・中断task保持・割り込みtask実行

このinstructionは、user interruptionによる安全停止、同じ中断taskの再開、元taskを保持した割り込みtask、親判断待ちtaskの一時退避だけを扱う。通常のrate limit・provider障害によるresumeは`glm-auto-resume.md`・`glm-execution.md`を正とする。

## 操作選択とmachine owner

- 実行中taskをuser interruptionで安全停止するときは`glm-worker --stop`、同じ中断taskの再開は`glm-worker --resume`、中断taskを保持したまま別taskを実行するときは`glm-worker --isolate`、`waiting-sol-review` / `waiting-decision`の判断を保留して優先taskへ差し替えるときは`glm-parent-action park` / `unpark`を使う。これらの意味を混ぜない。
- stop / resume / isolate / park / unparkのstatus admission、ack/error schema、process cleanup、snapshot/dirty/ref照合、isolation/park provenance、idempotency・stale record、fail-closed transitionは`control:stop-isolate-park-lifecycle`とcurrent production StateStore / app / workflow / parent-actionを正とする。親はexact field・signal順・retry matrixを自由言語から再構築しない。
- 手動PID推定、`kill` / `pkill`、stateやcheckpointの破棄、branch/worktree記録の書換えを正規lifecycleの代替にしない。machine resultが拒否・cleanup残存・staleを返した場合はその状態を保持し、canonical commandのevidenceから復旧する。

## user interruption後の継続

- user interruptionはprovider/rate-limit recoveryと別の意味を持つ。machineがuser interruptionとして確定した停止はtask failureではなく再開可能なcontrol outcomeとして扱い、停止済みtaskを新規taskとしてやり直さず、working tree・task state・session・resume checkpointを保持して同じcheckoutから再開する。
- `--resume`の保持基準・HEAD / dirty / untracked / parent-metadata照合はmachine ownerへ委ねる。fail closedしたときもinterrupted stateを壊さず、machine evidenceが示す不一致だけを修復して再試行する。
- stop retentionはtracked diffのrecovery patchを保持するが、untracked fileは内容hash/identityだけで本文原本を保存しない。untrackedを停止時内容へ戻す必要がある場合、その原本保持・復元は親の責任であり、hashから復元できると扱わない。

## 割り込みtask (`--isolate`)

- `--isolate`はuser interruptionで保持中の元taskと、別の割り込みtaskを同じcheckoutへ混在させないための境界である。隔離先の作成、元taskとのprovenance、再実行時の整合性、元task resume admissionはmachine ownerを正とする。
- Planを持つrepositoryでは隔離worktreeにも元Plan / ACTIVEが残る。USER_REQUESTだけで元ACTIVEを別taskの要求正本へ読み替えず、割り込み要求のtracked authority・parent-managed metadata・risk floorは各canonical ownerへ従う。
- 割り込み成果をいつ・どう統合するか、conflictをどう解決するかは親判断である。統合後に元taskを再開できるかはmachine provenance / retention checkへ委ね、親が検証条件を推測して迂回しない。
- `隔離branch`と隔離worktree側stateは、`元taskのresume保持照合が完了し元taskが完了するまで削除しない`。成果統合とこれらresourceの寿命管理は親所有であり、glm-workerが自動cleanupする前提にしない。

## 親判断待ちtask (`park` / `unpark`)

- `park`は親判断待ちtaskを保留したまま優先taskへ切り替えるための操作であり、running taskのstopやuser-interrupted taskのisolateへ意味を混ぜない。どのtaskを保留・優先するかは親が判断する。
- parked stateのsnapshot / restore、worktree・branch provenance、unpark admission、失敗時のrecoverable orderingとcleanup pendingはmachine ownerを正とする。`unpark`が拒否された場合はparked stateを保持し、統合・保持条件を修正してcanonical pathを再実行する。
- park側の成果統合・conflict解決と、task完了後のpark / isolation resource cleanupは親所有である。machine lifecycleが完了する前に手動削除して安全条件を迂回しない。

## 境界

- machine controlはlifecycle state・retention・process cleanup・provenanceを強制するが、「今止めるべきか」「割り込みtaskを走らせるべきか」「親判断をparkすべきか」「成果をどう統合するか」という意味判断は親が行う。
- repository固有のPlan / task authority / parent metadata / riskと、genericなprocess・session・worktree lifecycleを同一authorityとして扱わない。
