# Task: Quality toolchain preflight before model execution

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

none

## Resolved references

- 2026-09-05の隔離recovery taskではGLM workerが2 call、154 turns、18,741,435 input tokens、60,189 output tokensを消費してIMPLEMENTEDを返した後、harnesslintが`golangci-lint=2.13.1, required=2.7.0`で停止した
- repositoryには固定versionの正`quality-tools.yml`と正規配置入口`./install-quality-tools.sh`が既にあるが、通常workerの高コストmodel call前には同じversion preconditionを検査していなかった
- version不一致はrepository実装のsemantic判断を必要とせず、model call 0回で確定できるdeterministic environment failureである

## Purpose

後段で必ず失敗する固定quality tool不一致をGLM model call前に検出し、Codex/Sol品質を変えず無駄なmodel消費と復旧round tripを防ぐ。

## External feasibility

status: not-applicable

## Contract

- 通常worker/reviewer pipelineが必要とするquality tool versionを`quality-tools.yml`から読み、最初の高コストmodel call前にdeterministic preflightする
- 不一致時はmodel call 0回、working tree/state非変更でrequired/observed tool versionと正規修復入口をstructured errorへ返す
- version確認をharnesslintの既存validatorと共有し、install.sh・harnesslint・preflightで判定を重複実装しない
- 正規修復後は同じtask admissionを安全に再実行でき、事前失敗をtask実装失敗やreview findingとして計上しない
- preflight自体の時間・failure count・回避model callをbounded telemetryで追跡できる

## Must not

- version不一致をwarning化してmodel callへ進まない
- preflightでtoolを自動download・upgradeしない
- network probe、AI判断、shell自由文解析を追加しない
- GLM token節約のためにSol quality gateまたは必要testを省略しない

## Acceptance criteria

- golangci-lint、shellcheck、shfmt、Go toolchainの各不一致がmodel call 0回でfail closedするfixtureがある
- 全version一致時だけ既存worker/reviewer pipelineへ進む
- install後の実機scenarioでpreflight成功と後段harnesslint成功が同じversion authorityに基づく
- independent reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- 固定versionの正は`quality-tools.yml`、配置入口は`./install-quality-tools.sh`

## Dependencies

- `IMPLEMENTATION_TASKS/quality-surface-approval-review-continuation.md`

## Review findings

- 初回worker implementationはSolがquality基準を弱体化していないと確認済みだが、親Codexがreviewer前のquality-surface停止にterminal `accept`を誤適用し、task `ec36a7f1-10fa-46ab-b118-fb297a9cadc0`はworker 1件・reviewer 0件・validation 0件でfalse-completeになった
- lifecycle defect自体は`IMPLEMENTATION_TASKS/quality-surface-approval-review-continuation.md`へ分離した
- 復旧task `be2df76b-d921-4901-9636-dcb7aba14876`では初回implementation diffをdirty baselineとして保持したため、Sol承認後の`--approval-only`も`current diff is not covered by the parent-approved quality-surface scope`でfail closedし、reviewer未実行のまま正規継続不能になった

## Current boundary

初回implementation diff、復旧task/session/checkpointは保持されている。quality-surface変更はSol承認済みであり、基準値・lint rule・wrapperを変更しない。dirty implementation baselineでも承認後reviewへ正規継続できる機構が成立するまでNEXTで待機し、reset・再実装・terminal acceptを行わない。
