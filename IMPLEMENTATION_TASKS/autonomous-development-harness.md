# Task: Goal起点のCodex + GLM長時間自律開発機能を実現する

## Original instruction

````text
TASK

将来検討用として、Codex + GLMによる長時間自律開発用Development Harness案をBLOCKED taskとしてPlanへ追加する。

PURPOSE

ユーザーが詳細なIMPLEMENTATION_PLANを手書きしなくても、

> 「何を作りたいか」

を提示すれば、

1. Codexが要求を理解する
2. Codexが永続的なPlan / Taskへ分解する
3. 実行可能taskをGLMへ委譲する
4. GLM Worker / Reviewerが実装・一次レビューする
5. Codexがsemantic review / acceptance / replanningする
6. findingや追加要求をPlanへ統合する
7. Codex 5h Limit、GLM Limit、session終了、compaction等を跨いで再開する
8. 完了までdevelopment loopを継続する

という上位Development Harnessを将来的に検討する。

PRODUCT DIRECTION

glm-worker本体は可能な限り小さく強いimplementation executorとして維持する。

planning / task scheduling / durable state / Codex supervision / resume / recovery / requirement amendment / project completion等は、原則として上位Harness側へ分離する方向を優先候補とする。

概念上は以下。

User Goal
↓
Codex Planner
↓
Durable Development State
├─ GOAL
├─ PLAN
├─ TASKS
├─ HISTORY / evidence
└─ machine state
↓
Task Scheduler
↓
glm-worker
├─ GLM Worker
└─ GLM Reviewer
↓
Codex Gate
├─ semantic review
├─ acceptance
├─ finding taskization
└─ replanning
↓
next runnable task

PLANNING MODEL

Planは最初に固定して最後まで消化する工程表にはしない。

Codexが管理するliving planとし、実装結果・PoC・finding・追加要求に応じて再構成できることを目標とする。

例:

PoCで方式不成立
↓
本実装taskをcancel / block
↓
代替方式調査taskを追加
↓
dependencyを再構成

ユーザーから途中で追加要求が来た場合も、

- ACTIVE taskを無条件に中断しない
- requirementをlosslessに保存する
- Codexが依存関係と適切な処理境界を判断する
- Planへ統合する

ことを目標とする。

DURABLE RESUME

Codex 5h Limit等をdevelopment終了として扱わない。

conversation historyだけへ依存せず、durable stateから必要contextを再構成して再開できる構造を検討する。

候補state:

- ACTIVE
- BLOCKED
- WAITING_PARENT_CAPACITY
- WAITING_WORKER_CAPACITY
- WAITING_DECISION
- COMPLETE

再開時に最低限復元できるべき情報:

- original goal
- current plan
- active task
- unresolved findings
- dependency state
- relevant evidence
- completion state

CURRENT EXPERIENCE TO GENERALIZE

現在の運用から、将来以下をgeneric mechanismへ昇格可能か再評価する。

- ACTIVE / NEXT task管理
- task requirementのlossless保持
- findingを現在ACTIVE taskへの即時割込みにしない
- dependencyに従ったtask scheduling
- GLM Worker → Reviewer → Codex gate
- Codex decision待ち
- GLM provider / rate-limit recovery
- Codex 5h Limit recovery
- session / compaction後の再開
- feasibility / PoC先行
- evidence reuse
- completed task history
- escaped findingの再task化
- user amendmentのPlan統合

INTERFACE CANDIDATES

現時点では確定しない。

候補:

- `glm-worker develop`
- 独立した`glm-develop`
- plugin / development profile / driverとして分離

責務分離上、glm-worker本体へ全部を統合せず独立Harnessとする案も強く検討する。

ユーザーがIMPLEMENTATION_PLANを手書きする方式を必須にはしない。

通常入口はGoalとし、CodexがPlan / Taskへ変換する方向を候補とする。

DURABLE DATA CANDIDATE

現行のIMPLEMENTATION_PLAN / IMPLEMENTATION_TASKS / IMPLEMENTATION_HISTORYの経験は参考にするが、その構造をそのまま製品仕様として固定しない。

例:

.development/
GOAL.md
PLAN.md
TASKS/
HISTORY.md
STATE.json

Markdownとmachine stateの責務分離も再検討する。

- Markdown: requirement / plan / human-readable evidence
- machine state: lifecycle / dependency / resume cursor / execution state

PRIMARY KPI

24時間動き続けること自体を目的にしない。

最終目標は、

> Sol High相当品質を維持しながら、GLMへ実装作業を委譲し、親Codex/Solの実消費を可能な限り減らした状態で、長時間のdevelopment workflowを途切れず完了できること

とする。

NON-GOALS

- 現在のcodex-worker-orchestrator専用開発運用をそのまま製品化しない。
- Markdownを増やすだけのworkflowにしない。
- conversation contextを永続状態の正本にしない。
- giant autonomous agentをglm-worker本体へ押し込まない。
- planning / scheduling / recovery / worker executionの責務を混ぜない。
- 最初に作ったPlanを固定して最後まで消化するだけの方式にしない。
- GLMへproject-level semantic decision authorityを移さない。
- 既存glm-workerを固める作業より先に本案を実装しない。

BLOCKED

このtaskは現時点で着手禁止。

以下を両方満たすまで自動unblockしない。

1. 現在のglm-worker既存機能、contract、mechanical guard、production behaviorが十分固まること。
2. ユーザーが改めて本案をレビューした上で、明示的に着手を許可すること。

BEFORE UNBLOCK

着手許可を受けた時点で、保存した本案をそのまま実装仕様にはしない。

その時点の、

- glm-worker実装
- 実運用結果
- escaped bug
- Codex/GLM quota behavior
- token telemetry
- existing resume architecture
- production hardening状況

を踏まえて要件から再レビューする。

特に以下を改めて決定する。

- glm-worker builtinか独立Harnessか
- canonical Plan / Task / State model
- Codex Plannerの起動条件
- Codex semantic review頻度
- task粒度
- dependency model
- replanning条件
- user amendment semantics
- Codex 5h recoveryとの統合
- GLM limit recoveryとの統合
- crash / state corruption recovery
- concurrent execution可否
- repository管理file配置方式
- Git ownership
- token / cost telemetry
- Codex消費削減のA/B評価方法
- glm-worker内へ残す責務と外へ分離する責務

EXECUTION

- 現在ACTIVE taskを中断しない。
- NEXTへ昇格しない。
- PoC、詳細設計、実装を開始しない。
- BLOCKED taskとして要求をlosslessに保存するだけに留める。
- ユーザーの明示unblockなしに着手しない。
````

## Amendments

- 2026-09-02 user instruction:

````text
autonomous-development-harness.mdを022の前にやるようにしてくれ&#x20;
いまやってる021で発生したタスクがあるようであればそちらを優先で構わない
````

- 2026-09-03 user instruction:

````text
- `autonomous-development-harness.md`は過去の構想をそのまま実装仕様として扱わず、current implementationと実運用を踏まえて再評価する。
- 独立Harnessや新しいPlanner / state model / schedulerの導入を前提にせず、現状維持・既存機能の拡張・新規構成・No-Goを含めてCodex自身が採否を判断する。
````

- 2026-09-03 user clarification:

````text
- 直前のNo-Go判断は、「実現方法を自由に判断する」という指示を「機能自体を採用するか判断する」と誤って解釈したものなので撤回する。
- このtaskの目的は、Goalだけを入口として、人間がPlanを逐次管理しなくても、Codexがtask生成・選択・再計画を行い、GLMへ実装を委譲しながらプロジェクト完了まで継続できる機能を実現すること。
- 現在の実装で既に存在するstate / resume / review / recovery等は再利用し、具体的な設計・責務分離・実装方法はcurrent implementationと実運用を踏まえてCodex自身で判断する。
- 直前のNo-Goを記録したGit / Plan / task metadataも、この修正された要求に整合するよう扱う。
````

## Resolved references

- 「現在のglm-worker既存機能、contract、mechanical guard、production behavior」は、このtaskを将来再評価する時点のproduction実体と実運用証拠を指す。現時点のtask番号や固定commitへ束縛しない。
- 「十分固まること」はこのtask単独で自動判定しない。ユーザーによる明示unblockと、その時点の親Codexによるproduction hardening状況の再評価を両方必要とする。
- 2026-09-02時点で021由来の新規採用taskはないため、最新指示の条件付き優先対象は存在しない。「022」は`IMPLEMENTATION_TASKS/022-final-verification.md`を指す。
- 親CodexはPlan記載のF1-F10 hardening、parent action / handoff、typed parent-capability validation、repo-search、analysis bundle、lean context、wait instruction削減、containment denial、parent finalization、commentlint launcherのcurrent main統合をproduction hardening成熟の一次証拠とし、unblock条件1を満たすと判断した。最新指示をunblock条件2の明示許可として扱う。
- 2026-09-03指示により、過去のPRODUCT DIRECTION、INTERFACE CANDIDATES、DURABLE DATA CANDIDATEおよび「将来の第一候補」は候補の記録に留まり、architecture評価の既定値・採用要件・実装仕様として扱わない。
- 直前の「自由に採否を判断する」は実現方法の採否を意味し、Goal起点の自律開発機能自体をNo-Go候補に含めない。2026-09-03 user clarificationが、同日先行instructionと親No-Go判断をこの点でoverrideする。
- 公開済みcommit `00f5a0d`と`ea7fb1a`は過去判断の履歴としてrewriteせず、本taskの再open、最新Amendment、Plan訂正、後続commitにより撤回を明示する。
- 2026-09-03 parent architecture decision: canonical Goalは`IMPLEMENTATION_PLAN.local.md`のoptional `## GOAL`節、read-only machine surfaceは`glm-worker --project-state`とする。既存task lifecycleとparent semantic authorityを維持し、dependency未知参照・self dependency・cycleはfail closed、Goal進行中は単一ACTIVE、親acceptance後のterminal Goalだけcompleted GOALと空scheduleを許可する。新daemon・scheduler・state DB・第二正本は追加しない。

## Purpose

Goalだけを通常入口とし、人間によるPlanの逐次管理を要求せず、Codexがtask生成・選択・再計画とsemantic gateを担い、既存glm-workerへ実装を委譲しながら、quota・session・compactionを跨いでproject completionまで継続できる機能を実現する。

## External feasibility

status: not-applicable

## Contract

- Goal起点の自律開発機能の採用は確定とし、No-Go判断の対象は個別の実現方式だけに限定する
- current production実体、実運用、escaped bug、quota、telemetry、resume architecture、hardening状況を再評価し、公開interface、責務境界、planning / scheduling、replanning、amendment、project completion、永続化、依存方向、互換性を親Codexが確定してから実装する
- 現在存在するtask state、checkpoint、resume、parent action、worker/reviewer、Codex gate、rate-limit/Codex-limit recovery、telemetry、Plan/task guardを再利用し、同じ意味のstate model・scheduler・recovery layerを重複導入しない
- 通常利用者はGoalを提示すれば開始でき、Codexがdurable requirementと実行単位を生成し、依存関係・実行可能性・priorityから次taskを選択し、結果・finding・追加要求に応じてliving planを再構成する
- 実装はGLM worker/reviewerへ委譲し、architecture、project-level semantic decision、acceptance、replanning authorityは親Codexに保持する
- 人間によるPlan fileの手編集、task選択、通常の再開操作をproject completionの必須手順にしない。意味のあるユーザー判断・権限・外部状態が必要な場合だけ安全に停止する
- quota、provider停止、Codex limit、session終了、compaction、process interruptionから、goal、current plan、active task、unresolved finding、dependency、evidence、completion stateを復元して同じprojectを継続する
- user amendmentはlosslessにdurable requirementへ統合し、ACTIVEを無条件に破棄せず、影響範囲と依存関係に応じて継続・再計画・安全停止を選ぶ
- completionは単一taskの成功ではなく、Goal acceptanceを満たし、未解決の実行可能task・findingがなく、必要なquality gate・install・Git同期を終えたproject-level terminal stateとして判定する
- 過去のHarness、Planner、state model、scheduler、file配置候補は実装仕様にせず、current implementationに対する最小の責務追加で実現できる構成を優先する
- 最上位KPIは、Sol High相当品質を可能な限り維持しながら親Codex/Solの実消費を削減し、長時間workflowを完了できることとする

## Must not

- 現在のACTIVE taskを中断しない
- Goal起点の自律開発機能自体をNo-Goとしない
- 親architecture決定前に公開interface、state schema、production implementationを開始しない
- 最新指示による優先変更を、021の中断または021由来でないtaskの無条件優先へ拡張しない
- 現在のrepository固有運用をそのまま製品化しない
- Markdown追加だけ、固定Plan消化、conversation context依存、giant agent化で解決しない
- planning、scheduling、recovery、worker executionの責務を無検討に混在させない
- GLMへproject-level semantic decision authorityを移さない
- interface候補やdata候補を確定済み仕様として扱わない
- 独立Harness、新しいPlanner、state model、schedulerを導入前提として評価を進めない
- 既存state、resume、review、recovery、Plan/task guardと同じ責務を持つ第二の正本・daemon・schedulerを追加しない
- 通常flowで人間にPlanの逐次編集・task選択・resume操作を要求しない
- GLMにcommit/pushさせない

## Acceptance criteria

- 2026-09-02の優先変更指示がAmendmentsへlosslessに保存され、021由来の新規採用taskがないことと022参照が解決されている
- production hardening成熟の再評価根拠とユーザーの明示許可がtracked metadataから回収できる
- current implementationと実運用の一次証拠から、公開interface、責務分離、durable data ownership、planning / task selection / replanning、amendment、quota / crash recovery、completion判定、Git ownership、telemetry、互換性、rollback境界が親Codexにより確定される
- Goalだけからdurable project requirementと最初の実行可能task群を生成し、人間がPlanを逐次管理せずに実行を開始できる
- dependency、blocked reason、priority、worker/reviewer結果、finding、user amendmentに基づいて次task選択とliving plan再構成を行える
- GLMへの実装委譲、独立review、Codex semantic gate、必要なfix/replanを反復し、Goal acceptanceまで継続できる
- rate limit、provider停止、Codex limit、session/compaction/process interruption後に既存state/recoveryを使って同じprojectを再開でき、二重実行・要求喪失・誤completeを防ぐ
- project completionはGoal acceptance、未解決実行可能task/finding、quality gate、必要なinstall/Git同期を含めて判定され、単一worker PASSだけで完了しない
- Goal開始、task選択、再計画、user amendment、停止・再開、project completionのproduction wiringをtestで固定し、既存`glm-worker` command/APIとPlan-managed workflowの互換性を維持する
- No-Go撤回が最新Amendment、Plan、後続Git履歴に反映され、旧No-Goをcurrent decisionとして参照しない

## Historical invariants

- 現行の手動Plan/Task/History運用は将来Harnessの参考経験であり、製品仕様ではない
- glm-workerは可能な限り小さく強いimplementation executorとして維持する方向を優先候補とする
- 最上位評価はDirect Codex対Codex + glm-workerのCodex ReductionとQuality Deltaである
- Goal起点の自律開発機能自体は採用済みであり、親Codexの設計判断はその実現方式を対象とする

## Dependencies

- 現在のglm-worker既存機能、contract、mechanical guard、production behaviorが十分固まったと親Codexが一次証拠から再評価すること
- ユーザーが本案を再レビューし、明示的に着手を許可すること

## Review findings

none

## Current boundary

2026-09-03 user clarificationにより直前のNo-Goを撤回して再open。親architectureはoptional Plan GOAL + read-only `--project-state` + existing lifecycle reuseへ確定し、production implementation milestoneへ進む。旧No-Go commitはrewriteせず訂正履歴として残す。
