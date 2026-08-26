# GLM workerの安全停止・中断task保持・割り込みtask実行

`glm-worker --stop`で実行中taskを停止する、user interruption後の中断taskを`--resume`で再開する、または停止中の元taskを保持したまま同じrepoで割り込みtaskを実行する(`--isolate`)場合だけ適用する。通常のrate limit・provider障害によるresumeは`glm-auto-resume.md`・`glm-execution.md`側の契約を使う。

## GLM workerの安全停止 (`--stop`)

- 実行中のglm-worker taskを止めるときは`glm-worker --stop`だけを使う。手動でのPID特定・`kill`・`pkill`・process groupへのsignal送出を正式手順として使わない。停止authorityは単一目的local control endpointへの接続とowner側ackであり、lock fileのPIDは診断値である。
- 停止要求はrepo lockを待たず対象repositoryのcwdから実行する。ownerはClaude CLIのprocess groupへTERMを送り、bounded猶予後に残存childだけへKILLへ昇格し、group非残存とinterrupted状態保存を確認してからackする。
- ackのmachine JSONは`{"result":"interrupted","task_id":...,"task_status":"interrupted","resume_available":true}`。停止より先に自然終端していたときは`result: terminal|exited`へ現在のauthoritative statusで応答し、確定済みの成功結果を停止扱いへ書き換えない。`result: interrupted_cleanup_residual`はinterrupted保存済みだが停止後のprocess group残存が観測された状態で、group非残存を確認した安全停止authorityではない。このackのときは残存processを確認・回収してからpreempt・resumeし、`cleanup_warning`を診断に残す。
- `kind: stop_endpoint_absent`は現在running ownerが不在、`kind: stop_endpoint_stale`はsocketが残ってもackが得られない状態である。どちらもstale PID推定・手動killへ切り替えず、`--status`で現在状態を確認してから扱う。
- 停止された主呼出はstderrへ`kind: interrupted`のerror JSONを出してnon-zero exitする。これは失敗ではなく再開可能停止であり、`worker_error`扱いしない。
- 中断taskのworking tree・task state・session・resume checkpointを破棄・resetせず、新規taskとしてやり直さない。再開は同じcheckoutで`glm-worker --status`の`task_status: interrupted`を確認してから`glm-worker --resume`で行う。

## 停止保存の保持基準と中断taskの再開

- 停止保存時に停止時点の元checkout保持基準(git 3軸snapshotと親管理metadata除外dirty/untracked列挙)がcheckpointへ、停止時tracked diffのbinary patch(`stop-worktree.patch`・`stop-index.patch`)がstateへ固定される。patchにuntracked file本文は含まれず、untrackedは保持基準のhash検証だけであるため、自分が別途保持する原本なしには停止時内容へ復元できない。
- user interruption後の`--resume`はこの基準へ現在状態を機械照合し、保持対象の変化・停止後の新規dirty・停止時HEADを含まないHEAD移動を`kind: worker_error`でfail closedする。fail closedでもstate・checkpoint・sessionはinterruptedのまま残るため、保持対象を停止時内容へ復元して(tracked diffはpatchを、untrackedは自分が保持する原本で)同じ`--resume`を再試行する。

## 停止taskを保持したまま割り込みtaskを実行する (`--isolate`)

- 停止中の元taskと同じrepoで別taskを実行するときは、同一checkoutで新規taskを投入せず`glm-worker --isolate`で割り込みtask実行checkout(git worktree + branch `glm-worker/isolation/...`)を作る。ackの`worktree`をcwdとして割り込みtaskを通常どおりglm-workerへ投入する。隔離先のstate・lock・sessionはpath由来のrepo hashで分離され、`--status`の`isolation_origin`(元repo・元task ID)で元taskと取り違えを機械確認する。
- `--isolate`はuser interruptionによる`interrupted`停止中だけ受け付ける。task不在・別status・rate limit/provider障害による停止はfail closedする。
- 元repo側`--status`の`isolation`に現在の隔離先(worktree・branch・作成HEAD)が出る。記録は現在の隔離先を指す単一pointerで、隔離済みstateへの再`--isolate`はworktree・branch・隔離側出自記録の生存を確認して同じmachine結果を冪等に返す。worktree・branchが既に無いstale記録や破損記録は上書きせずfail closedする。記録は新規task開始で消える。
- Plan(`IMPLEMENTATION_PLAN.local.md`)を持つrepoでは、隔離worktreeのPlan ACTIVEも元task fileを指したままcheck outされるため、USER_REQUESTだけでは割り込みtaskの要求正本を特定できない。割り込みtask開始前に隔離worktree側で(a)割り込みtask fileを`IMPLEMENTATION_TASKS/`配下へ置いて明示pathだけでcommitし、(b)worktree側Plan ACTIVEを割り込みtask fileへ切り替える(こちらは未commitのまま)、という手順を取る。Plan切替をbranchへcommit・mergeすると元repoのACTIVE参照が壊れるため統合commitへ含めない。task fileをcommitせずに実行すると実効riskがimplementation-tasks critical pathでHIGHに固定されreviewer PASSがrisk floorで拒否される。Plan編集を含む割り込みtaskは実risk HIGHでwaiting-sol-reviewへ昇格するのが正常終端であり、packetを確認してから統合する。
- 隔離taskのcommitの統合とconflict解決は自分の責務である。統合は元taskがworker/auto-fix段階で`interrupted`の間か終端status後に行う。reviewer段階中断taskのreview resumeはHEAD移動を許さないため統合後に再開できない。
- 統合は元repoでbranchを通常mergeで行う。割り込みtask fileなど成果物は明示pathだけcommitし、Plan ACTIVE切替をbranchへ含めない。統合後の元task`--resume`保持照合は、隔離記録の実質検証(記録の元task・元repo・作成HEADが現在task・repo・停止時HEADと一致、記録branchが解決可能でそのtipが現在HEADへ統合済み、統合後のbranch tipから現在HEADまでが親管理metadata更新だけ、隔離worktree側stateの出自記録と対称)を通過した場合だけ非親管理file変更を承認する。squash/cherry-pick統合はbranch tipの照合ができないため通常mergeで統合する。隔離worktree側state dir(出自記録)は元task完了まで削除しない。保持対象と統合が触れたconflictでfail closedしたときは`stop-worktree.patch`・`stop-index.patch`(tracked diffのみ)を基準に解決し、元checkoutを確定させた上で同じ`--resume`を再試行する。統合後の元task resumeも、統合済み割り込みtask fileがdiffへ載るため実risk HIGHとなりwaiting-sol-reviewへ昇格し得る。
- 統合後のworktree・branch削除は`git worktree remove`で自分が行う。glm-workerはworktreeの寿命を管理しない。
