# Task: Quality surface approval review continuation

## Original instruction

````text
この件に限らず改善タスクは積んでいけって言ってるだろ
````

````text
そもそも自由言語でハーネスにしようとしているのが間違いなんじゃねえのか
他に今までやった、これからやるものが自由言語で防ごうとしているものがあるんじゃねえのか
お前にはルールを守る能力がないんだから自由言語で防ぐことは不可能
````

## Amendments

### 2026-09-05

````text
Sol承認済みcurrent diffに`glm-parent-action fix <token> --accepted-scope current-diff --approval-only`を適用したが、復旧taskのbaselineに既存implementation diffが含まれていたため`current diff is not covered by the parent-approved quality-surface scope`でfail closedした。dirty implementationを保持したfalse-complete復旧でも、承認scopeを安全に固定してworker再実行なしでreviewerへ進める必要がある。
````

## Resolved references

- 2026-09-05、task `ec36a7f1-10fa-46ab-b118-fb297a9cadc0`はworkerがquality policy surfaceを変更したため、reviewer実行前に`waiting-sol-review`へ停止した
- canonical handoffは`required_action=parent-review`に対して`allowed_actions=[accept,fix]`を返し、structured fieldではquality surface承認後にreviewへ継続するactionを一意に示さなかった
- packetの自由文だけが`current diffを明示承認して同一taskのfix/review経路へ進める`と説明していた
- 親Codexがhandoff上許可された`glm-parent-action accept`を選ぶとtaskは直ちに`complete`となり、telemetry上worker call 1件・reviewer call 0件・validation 0件のままterminal化した
- task `be2df76b-d921-4901-9636-dcb7aba14876`では既存implementation diffを保持して再開始したためbaseline statusがproduction dirtyとなり、現行`acceptedFixScopeBaselineSafe`が承認scope生成を拒否して承認専用actionも正規継続不能になった

## Purpose

quality surface変更のSol承認とtask最終acceptをmachine lifecycle上で区別し、独立review未実行のfalse-completeを自由言語解釈に依存せず防ぐ。

## External feasibility

status: not-applicable

## Contract

- self-protectionによりreviewer前の`waiting-sol-review`へ停止したstateでは、quality surface承認後に同一task・同一worker結果からreviewerへ継続する専用actionをcanonical handoffへ一意に返す
- 同stateではterminal `accept`をadmission段階でfail closedし、承認専用actionだけがaccepted scopeを固定してreviewへ進める
- parent-action CLIはpacket自由文を読まなくても実行可能なmachine-readable required/allowed actionと必要parameterを返す
- reviewer PASSと必要validation後だけ通常の最終acceptを許可する
- false-complete等から既存implementation diffを保持してtask lifecycleを復旧した場合も、baseline時点のpre-existing diffと承認対象のtask diffを機械的に区別し、現在diffを過大承認せずreviewerへ継続できる
- 既存のSol最終accept、明示fix、reviewer差戻し、quality surface非変更taskのlifecycleを壊さない

## Must not

- 親Codexの自由言語判断だけで`accept`とquality surface承認を区別しない
- reviewer未実行をtask completeとして扱わない
- worker/reviewer sessionを不要に再起動しない
- quality policy surface guard自体を弱体化しない

## Acceptance criteria

- reviewer前のquality surface停止でterminal acceptがmodel call 0回で拒否されるtestがある
- 専用承認actionからworkerを再実行せずreviewerへ進み、review結果に応じて通常lifecycleへ戻るintegration testがある
- handoff/parent-action admission/CLI helpが同じaction contractを返す
- 現在taskで発生したworker 1件・reviewer 0件・validation 0件のfalse-complete形をfixtureで再現し、再発を防ぐ
- production dirty baselineを保持した復旧fixtureで承認scope生成が不可能にならず、baseline以前の変更を新規承認scopeへ混入させない
- independent reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- GLM worker/reviewerにGit remote write authorityを付与しない
- Sol Highがquality surface変更を判断し、GLM reviewerが承認後のimplementationを独立検証する

## Dependencies

`IMPLEMENTATION_TASKS/post-worker-quality-gate-recovery.md`

## Review findings

- quality surface承認の正規操作がpacket自由文にしかなく、machine handoff上はterminal acceptも許可されるため、親Codexの選択ミスでreview未実行のfalse-completeが成立する

## Current boundary

task `1c279537-b51f-4c33-b89a-3309408390ea`はworker結果を保持してSol承認まで進んだが、pre-review harnesslintのquality tool version mismatchで既知の`active/stale + required_action:none`へ停止した。post-worker quality gate recoveryを先行実装して同一task/sessionを復旧し、その後に保存済みpreflight taskを正規reviewへ戻す。
