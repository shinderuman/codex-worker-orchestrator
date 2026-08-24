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

none

## Resolved references

- 「現在Codexが実際にどう中断処理しているか」について、2026-08-24に親Codexはrunning `glm-worker --resume`の外側cellをterminate後、repo lock PID 27479へSIGINTした。lockは解放されたがClaude child PID 27482はPPID 1・process group 27479で生存し、別task開始後もworking treeを書き換えて後続reviewのworker-end/review-start snapshot不一致を2回発生させた。最終的にprocess group 27479へTERMしPID消滅を確認した
- 当該元taskのproduction diffはmessage `external-feasibility-interrupted-before-safe-stop-followup`（先行partial）と`external-feasibility-orphan-final-after-process-group-stop`（停止直前までの最終diff）のtask固有stashへ可逆保全し、後続task-status follow-upのdiffと分離した。stash番号は将来変動するためmessageをidentityの正とする
- 「別の割り込みtask」はtask status machine enum external-review follow-up。元taskはexternal feasibility dispatch gate

## Purpose

親Codexがrunning glm-workerを正式なmachine interfaceで安全停止し、必要なら元taskの作業状態を別taskから隔離して後で再開できる最小contractを設計・実装する。

## Contract

- 実装前に、process停止能力とtask stateのsuspend/resume・別task隔離能力が同一問題か別問題かを現architectureから判定する
- 親Codexの実中断事例、repo lock、Claude child/process group、task/session/checkpoint、working tree、snapshot guard、StartNewTaskの破壊境界を一次証拠で調査する
- 安全停止はchild/orphanを残さず、terminal成功へ誤遷移せず、停止完了をtyped machine outputとauthoritative stateで確認可能にする
- 別task実行が元task stateを上書きするなら、停止interfaceへ無理に混ぜず独立したsuspend/restore contractとして最小設計する
- 元taskのworking tree・session・checkpoint・要求正本・snapshotを別taskから隔離し、復帰時に要求とGit stateを取り違えない
- parent orchestration instructionとproduction CLI/state/testを同時に更新し、手動PID killを正式手順として残さない

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
- 元taskを保持したまま別taskを完了し、元要求・session/checkpoint・working treeを復元して継続できることを検証。単一interfaceで成立しないなら分離設計を明示
- external Claude CLI成立性が前提なら実producer/process treeの最小PoCと親Go/No-Goを実装前に完了
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

task-status machine enum follow-up完了直後、external feasibility dispatch gate再開より前にACTIVE化する。最初は親Codexのread-only調査とSol設計判断であり、設計前にGLM implementationへdispatchしない。
