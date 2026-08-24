# Task: glm-workerの安全な割り込みとtask保持を設計・実装する

## Original instruction

````text
# Codex指示：glm-workerの安全な割り込み手段を追加する

現在、親Codexが作業中に
「現在のGLM作業をいったん止め、別の割り込みtaskを先に処理するべき」
と判断した場合、それを安全に実行する正式なinterfaceがない。

これを解決する。

単なる人間向けSafe Stopではなく、
親Codex自身がorchestration上の判断として利用できることを目的とする。

具体的なcommand名、signal、control socket、state構造、resume方式はこちらから指定しない。
現行のglm-worker実装、Codexの現在の中断運用、task/session/checkpoint/lock/process lifecycleを調査し、
最小で自然な方式をCodexが設計すること。

最低限満たしたいこと:

- 実行中のGLM作業を安全に止められる
- working treeや必要な作業状態を失わない
- child process等を不正に残さない
- complete/PASS等へ誤遷移しない
- Codexがmachine interfaceから停止完了を確認できる
- Codexが別の割り込みtaskへ移れる
- 元taskへ戻る必要がある場合、そのstateを別taskで破壊しない

特に、
「現在taskを安全に止められること」と
「そのtaskを保持したまま別taskを実行し、後から戻れること」
が現行architecture上同じ問題なのか別問題なのかを最初に確認すること。

現在Codexが実際にどう中断処理しているかはこちらから仮定しない。
Codex自身で確認すること。

既存の --reset / --resume / rate-limit / provider failure 等との意味を混同しない。

汎用job scheduler、task queue、remote control plane、
Codex→GLMへの任意message injectionには拡張しない。

外部Claude CLIの挙動に依存する成立性がある場合は、
人工fixtureだけで成立したことにせず、既存のexternal feasibility gate方針に従う。

調査 → 設計判断 → Task/Plan反映 → GLM実装 → review → 必要なgate
の既存lifecycleで進める。
````

## Amendments

### 2026-08-24: 親read-only調査と責務分離

````text
親Codexが現行productionを調査し、ClaudeRunnerはexec.Command(...).Run()でClaude CLIを通常の子processとして起動し、signal handler・context cancellation・process group設定を持たないことを確認した。repo lock fileのPIDは診断値に留まり、停止authorityではない。実incidentでは親PIDだけへのSIGINT後にClaude childがPPID 1へreparentされ、元process groupで書込みを継続した。

StateStoreはrepositoryごとに単一のcurrent task slotを持ち、StartNewTaskがtask.id、worker/reviewer session、status、snapshot等を置換する。working treeも同一checkoutで共有される。このため「現在processを安全停止する能力」と「停止taskを保持したまま別taskを同じrepositoryで実行する能力」は別責務である。

本taskは正式なsafe-stop machine interface、process tree cleanup、停止用checkpoint/session保持、非成功終端、停止完了確認までに限定する。別task実行中の元task state/working tree隔離と復帰はIMPLEMENTATION_TASKS/interrupted-task-checkout-isolation.mdへ分離する。

safe-stop production実装は実Claude CLIのprocess group停止・descendant消滅・partial session resume成立性に依存するため、IMPLEMENTATION_TASKS/claude-interrupt-feasibility.mdの実producer PoCと親Go判断を先行させる。
````

### 2026-08-24: external feasibility Go後のproduction設計

````text
実Claude CLI 2.1.226でprocess group停止後のdescendant消滅・後続書込みなし・同一partial session IDの--resume成功を確認し、production safe-stopへGoとする。

最小設計:

- machine commandはglm-worker --stopとし、repo lockを待たずrunning ownerの単一目的local control endpointへ固定stop requestを送る。任意message injectionやgeneric control planeにはしない
- endpoint接続とowner側ackを停止authorityとし、lock file PIDは引き続き診断値に限定する。stale endpoint・owner不在・既にterminalのcaseをtyped JSONで区別する
- ownerはClaude CLIを独立process groupで起動し、stop request時にgroupへTERM、bounded cleanup後も残る場合だけKILLし、direct child waitとgroup非残存を確認してからackする
- workflowはmodel call前に保存済みのcheckpoint、role session ID、要求正本、snapshotを保持し、generic worker errorのcheckpoint/session clearへ流さない。初回call途中でも同一sessionをresumableとしてmarkする
- checkpointへuser interruptionを表す明示field、task statusへinterruptedを追加し、既存rate-limited/provider-unavailableと混同しない。--resumeはinterrupted checkpointを同一stage/phase/sessionから再開する
- external task status enumは既存6値+nullからinterruptedを含む7値+nullへ意図的に更新し、status/stats/timeline/convergenceを単一helperで同期する。resume_availableもinterruptedでtrueとする
- stop requesterはcleanup完了後にtask_id、status=interrupted、resume_available=trueをJSONで受ける。停止された元invocationはkind=interruptedのstructured error + non-zeroで終端し、PASS/completeを出さない
````

## Resolved references

- 「現在Codexが実際にどう中断処理しているか」について、2026-08-24に親Codexはrunning `glm-worker --resume`の外側cellをterminate後、repo lock PID 27479へSIGINTした。lockは解放されたがClaude child PID 27482はPPID 1・process group 27479で生存し、別task開始後もworking treeを書き換えて後続reviewのworker-end/review-start snapshot不一致を2回発生させた。最終的にprocess group 27479へTERMしPID消滅を確認した
- 当該元taskのproduction diffはmessage `external-feasibility-interrupted-before-safe-stop-followup`（先行partial）と`external-feasibility-orphan-final-after-process-group-stop`（停止直前までの最終diff）のtask固有stashへ可逆保全し、後続task-status follow-upのdiffと分離した。stash番号は将来変動するためmessageをidentityの正とする
- 「別の割り込みtask」はtask status machine enum external-review follow-up。元taskはexternal feasibility dispatch gate

## Purpose

親Codexがrunning glm-workerを正式なmachine interfaceで安全停止し、必要なら元taskの作業状態を別taskから隔離して後で再開できる最小contractを設計・実装する。

## Contract

- process停止能力とtask stateのsuspend/resume・別task隔離能力は別問題として扱い、本taskは前者へ限定する
- 親Codexの実中断事例、repo lock、Claude child/process group、task/session/checkpoint、snapshot guardの一次証拠を設計入力にする
- 安全停止はchild/orphanを残さず、terminal成功へ誤遷移せず、停止完了をtyped machine outputとauthoritative stateで確認可能にする
- 停止要求のauthorityをlock file PID単独へ置かず、running invocation自身が要求を受理してClaude process treeを終了し、cleanup後にackする最小local control境界を設計する
- 停止時点のpre-call checkpoint、session ID、要求正本、Git snapshotを保持し、generic worker error経路でclearしない。再開可否は実PoCで確認したClaude session semanticsに従う
- parent orchestration instructionとproduction CLI/state/testを同時に更新し、手動PID/process-group killを正式手順として残さない

## Must not

- `--reset`、rate-limit、provider-unavailableをユーザー割り込み停止の代用にしない
- 汎用job scheduler、task queue、remote control plane、任意message injectionへ拡張しない
- artificial child fixtureだけでClaude CLI/process treeの停止成立性を証明しない
- 中断した元taskのstash/session/stateを破棄しない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- 親Codexが実際に行った外側cell terminate＋lock PID SIGINTでchild書込みが継続した原因をprocess/state境界で確定
- 安全停止と別task中の元task保持が同一または別能力かをSolが実装前に判断
- running worker/reviewer、tool実行中、停止race、停止後status、orphan非残存、成功誤遷移なしをproduction-pathで検証
- 実producer PoCで確認したprocess group cleanupとpartial session resumeをproduction pathへ接続し、fixtureはerror/race境界の決定論的testに限定
- `--stop` request/ack、停止された元invocationのstructured error、`--status`のinterrupted/resume_available、`--resume`の同一stage/phase/session継続を一連のprocess testで確認
- stop endpoint不在・stale、停止requestと自然terminalのrace、TERM無視childのbounded KILL、初回worker/reviewer/tool実行中を確認
- task statusの7値+nullをstatus/stats/timeline/convergence、README、raw JSON testで同期
- 関連test、全test/race/vet/build/gofmt、独立review、必要なSol gate、親Codex commit/install/source一致/smoke

## Historical invariants

- parent-managed Plan/TASK/Historyは親Codex専有、GLMはcommit/pushしない
- repo lockだけを対象repoのrunning判定に使う既存contractを維持し、PID値だけをliveness authorityへ昇格させない
- 最上位目的はSol High相当品質を維持しながらCodex/Sol実消費を大幅削減すること

## Dependencies

none

## Review findings

- 2026-08-24実事故ではrepo lock解放後も中断元taskのproduction filesが別task reviewer実行中に変化し、snapshot guardがreviewを停止した。guardは混在reviewを防いだが、安全停止・task隔離interfaceの欠如は解消していない

## Current boundary

親read-only調査・責務分離・実Claude feasibility PoC・Sol Go判断を完了。PoC task lifecycle完了後にACTIVEへ復帰し、このAmendmentの最小設計を要求正本としてGLMへproduction実装を委譲する。
