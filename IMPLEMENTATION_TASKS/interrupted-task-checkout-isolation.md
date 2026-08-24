# Task: 割り込みtask実行中の元task stateとcheckoutを隔離する

## Original instruction

````text
# Codex指示：glm-workerの安全な割り込み手段を追加する

最低限満たしたいこと:

- Codexが別の割り込みtaskへ移れる
- 元taskへ戻る必要がある場合、そのstateを別taskで破壊しない

特に、
「現在taskを安全に止められること」と
「そのtaskを保持したまま別taskを実行し、後から戻れること」
が現行architecture上同じ問題なのか別問題なのかを最初に確認すること。

汎用job scheduler、task queue、remote control plane、
Codex→GLMへの任意message injectionには拡張しない。
````

````text
親Codex設計判断（2026-08-24）:

現行はrepositoryごとにcurrent task stateが1 slotで、StartNewTaskがtask/session/status/snapshotを置換し、同一checkoutのworking treeも共有する。したがってprocessのsafe-stopだけでは別taskを安全に開始できない。

formal safe-stopとは独立した責務として、停止taskの要求正本・state/checkpoint/session・working treeを保持したまま割り込みtaskを隔離実行し、後から元taskへ戻る最小方式を設計する。第一候補は別checkout/worktreeによる既存repo-hash state分離であり、glm-worker内部へmulti-slot schedulerを作る前に成立性・統合コストを評価する。
````

## Amendments

### 2026-08-25: safe-stop prerequisite完了後のlifecycle同期

````text
safe-interruption-task-suspensionはproduction実装・review・Sol採用・親commitまで完了し、完了task fileをHistoryへ移行した。fulfilled dependency pathはDependenciesから除去し、成立済みsafe-stop invariantをHistorical invariantsへ保持する。
````

## Resolved references

- 実incidentでは同一checkoutで元Claude childと後続taskが同時に書込み、review snapshot mismatchを2回発生させた
- 元external feasibility gate diffはmessage identity付きstash 2件へ保全済みだが、元glm-worker current stateは後続task開始で置換された

## Purpose

formal safe-stop後、割り込みtaskが元taskのstateとworking treeを上書きせず、元taskへ意味的に復帰できる最小のcheckout/state隔離contractを実装する。

## Contract

- 同一checkoutの単一slot stateへmulti-taskを重ねず、別checkout/worktreeによる既存repo-hash分離を第一候補として評価する
- 元task checkoutは停止確認後も要求正本・session/checkpoint・working tree・snapshotを保持し、割り込みtaskから書込み不能な境界にする
- 割り込みtaskのcommitを元taskへ統合する時点と、元taskのdirty diff/conflict解決責任を明示する
- 別checkout方式が成立しない具体的根拠がある場合だけ、最小のsuspend/restore stateをSol設計へ戻す
- 親Codexがmachine-readableに元task・割り込みtask・復帰対象を取り違えない確認を持つ

## Must not

- generic job scheduler、task queue、複数task state DB、remote control planeを追加しない
- stash番号をtask identityとして固定しない
- safe-stop未完了のprocessを残したまま別taskを開始しない
- 元taskのstate/checkpoint/session/working treeをreset・破棄しない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- 元taskをformal safe-stopし、別checkoutで割り込みtaskを実行しても元checkout/stateがbyte不変
- 割り込みtask完了後、元taskの要求正本・session/checkpoint・working treeを復元ではなく保持状態から再開できる
- 同一repositoryのGit履歴統合とdirty conflict時のfail-closed境界をproduction-pathで検証
- parent machine interfaceで停止完了・隔離先・復帰対象を確認可能
- 関連test、全test/race/vet/build/gofmt、独立review、必要なSol gate、親Codex commit/install/source一致/smoke

## Historical invariants

- running taskの安全停止は、単一目的`glm-worker --stop` endpoint、Claude process-group cleanup、`interrupted` checkpoint/status、同一task `--resume`としてproduction成立済み
- repo lockとstateはrepository path/hashごとに分離され、別repo並列実行をglobal mutexで直列化しない
- parent-managed metadataは親Codex専有、GLMはcommit/pushしない

## Dependencies

none

## Review findings

- process停止とtask保持を単一commandへ詰め込むとstate/archive/worktree/scheduler責務が混在するため分離した

## Current boundary

safe-stop production実装・親commitを完了しACTIVE化済み。本配置・source一致・installed smoke完了後に、別checkout第一候補の設計を再確認して実装へ進む。
