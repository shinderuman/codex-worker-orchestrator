# Task: Generic command harness / repository harness boundary

## Original instruction

````text
`glm-worker`の「汎用command harness」と、このrepository固有の「repository harness / development policy」の境界を監査し、必要なリファクタを行ってください。

目的は、`glm-worker`を別repositoryで使ったときに、このrepository固有のPlan・Task・protected path・quality policy・self-protection等を誤って一般ルールとして適用しないようにすることです。

方針:

- model invocation、session、state、lock、lifecycle、retry/resume、packet、telemetry等の汎用mechanismは`glm-worker`側に残す。
- `IMPLEMENTATION_PLAN.local.md`、`IMPLEMENTATION_TASKS/`、`IMPLEMENTATION_RULES.md`、repository固有quality surface等の具体的policyはrepository harness側の責務として整理する。
- 現在production codeへ直接焼き付いているrepository固有知識を洗い出す。
- 別repositoryで偶然同名file/directoryが存在しても、このrepository固有protocolだと誤認しない設計にする。
- repository固有harnessを有効にする必要がある場合は、暗黙の名前検出より明示的なopt-in境界を優先する。
- 既存のfail-closed/self-protection等の機械的保証を弱めない。
- 汎用plugin frameworkや巨大な設定schemaを新設しない。必要最小限の境界整理にする。
- 現在このrepositoryで成立している挙動を維持する。

少なくとも、通常のforeign repository、このrepository、偶然同名のPlan/Task fileを持つforeign repositoryについて、repository固有policyが漏れないことをtestしてください。

まず現状の責務混在を調査し、root ownerと最小の設計変更を決めてから実装してください。
````

## Amendments

none

## Resolved references

none

## Purpose

`glm-worker`の汎用execution mechanismと当repository固有のdevelopment policyを明示境界で分離し、foreign repositoryの偶然の同名fileを根拠に固有policyが誤適用される経路をなくす。

## External feasibility

status: not-applicable

## Contract

- 実装前にproduction code内のPlan・Task・Rules・protected path・quality surface・self-protectionに関する固有知識と呼出し経路を列挙し、汎用mechanismとrepository policyの責務表を作る
- root ownerと最小のopt-in境界を決め、architecture・責務・公開CLI/stateに意味のある選択が生じる場合は実装前にSol判断へ戻す
- model invocation、session、state、lock、lifecycle、retry/resume、packet、telemetryの汎用mechanismはcommand harnessに保持する
- repository固有policyは明示opt-inされたharnessのみで有効にし、file/directory名だけで当repository protocolを起動しない
- 当repositoryのfail-closed、protected metadata、quality policy、self-protectionを現在の強さで維持する
- 新しい巨大plugin/config frameworkを作らず、既存構成に必要最小限の所有権・wiring・opt-inだけを追加する

## Must not

- foreign repositoryで`IMPLEMENTATION_PLAN.local.md`、`IMPLEMENTATION_TASKS/`、`IMPLEMENTATION_RULES.md`の名前だけを固有harnessのopt-inと扱わない
- 汎用session/state/lifecycle/retry/packet/telemetryを当repository固有実装へ移さない
- fail-open、保護path縮小、quality gate迂回、self-protection無効化で境界分離を実現しない
- 任意repository policyを動的に追加する汎用plugin systemへ拡張しない
- 当repositoryの現行の正常系と安全停止を意味的に変えない

## Acceptance criteria

- 固有知識のinventoryとroot owner判定がsource/call-site evidenceに紐付く
- 通常のforeign repositoryで汎用mechanismは利用できるが当repository固有policyは有効にならない
- 当repositoryでは明示opt-inにより現行のPlan/Task/protected path/quality/self-protection挙動が維持される
- 偶然同名のPlan/Task/Rules fileを持つforeign repositoryでも当repository固有policyが誤作動しない
- 上記3repository scenarioの直接regression testと、影響する既存fail-closed/self-protection testが合格する
- independent reviewer、Sol semantic review、relevant/full validation、runtime install/smokeを完了する

## Historical invariants

- GLM worker/reviewerにGit remote write authorityを与えない
- parent-managed implementation metadataは親Codexだけが編集する

## Dependencies

none

## Review findings

none

## Current boundary

先に責務混在とcall-siteを調査し、root ownerと最小境界をSol判断してから実装する。この境界を未確定のままprose-only controlの機械化を進めるとrepository固有policyを汎用command harnessへ追加するriskがあるため、現ACTIVE完了後は`prose-only-control-enforcement-audit`より先に実行する。
