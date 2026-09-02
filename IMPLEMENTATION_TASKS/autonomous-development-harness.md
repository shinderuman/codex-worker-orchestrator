# Task: Codex + GLM長時間自律開発Harnessを将来検討する

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

## Resolved references

- 「現在のglm-worker既存機能、contract、mechanical guard、production behavior」は、このtaskを将来再評価する時点のproduction実体と実運用証拠を指す。現時点のtask番号や固定commitへ束縛しない。
- 「十分固まること」はこのtask単独で自動判定しない。ユーザーによる明示unblockと、その時点の親Codexによるproduction hardening状況の再評価を両方必要とする。
- 2026-09-02時点で021由来の新規採用taskはないため、最新指示の条件付き優先対象は存在しない。「022」は`IMPLEMENTATION_TASKS/022-final-verification.md`を指す。
- 親CodexはPlan記載のF1-F10 hardening、parent action / handoff、typed parent-capability validation、repo-search、analysis bundle、lean context、wait instruction削減、containment denial、parent finalization、commentlint launcherのcurrent main統合をproduction hardening成熟の一次証拠とし、unblock条件1を満たすと判断した。最新指示をunblock条件2の明示許可として扱う。
- 2026-09-03指示により、過去のPRODUCT DIRECTION、INTERFACE CANDIDATES、DURABLE DATA CANDIDATEおよび「将来の第一候補」は候補の記録に留まり、architecture評価の既定値・採用要件・実装仕様として扱わない。

## Purpose

Goalを入口としてCodexがliving planとdurable stateを管理し、glm-workerを小さく強いexecutorとして利用しながら、quota・session・compactionを跨いで長時間のdevelopment workflowを完了する上位Harness案を、将来の再検討対象として保持する。

## External feasibility

status: not-applicable

## Contract

- このtask自体は親Codexのarchitecture / decomposition gateとし、umbrellaのままGLM implementationへdispatchしない
- current production実体、実運用、escaped bug、quota、telemetry、resume architecture、hardening状況を再評価し、builtinか独立Harnessか、責務境界、canonical state model、planning / scheduling、replanning、amendment、quota recovery、crash recovery、concurrency、repository配置、Git ownership、telemetry、A/B評価を親Codexが決定する
- 大規模な一次調査だけread-only GLM workerへ委譲できる。高レバレッジなarchitecture、公開interface、state model、永続化、依存方向、互換性、Go / No-Goは親Codexが確定する
- 採用したPoC・production implementation・validationは、semantic filenameと単一review可能責務を持つ個別taskへ分割し、Plan上で`IMPLEMENTATION_TASKS/022-final-verification.md`より前へ置く
- architecture evaluationで成立性または費用対効果を支持できない場合は、実装taskを作らずNo-Goまたは追加evidence待ちの境界を明示する
- 現状維持、既存機能の限定拡張、新規構成、No-Goを同列の候補として比較し、独立Harness、新しいPlanner、state model、schedulerの導入を前提にしない。過去の「外側へ分離する第一候補」は2026-09-03 Amendmentにより評価上の優先を失う
- Planは固定工程表ではなく、PoC、finding、追加要求、実装結果に応じてCodexが再構成するliving planとして検討する
- conversation historyを正本にせず、goal、plan、task、unresolved finding、dependency、evidence、completionをdurable stateから復元できることを検討対象とする
- 現行のPlan/Tasks/History運用は一次経験として再評価するが、そのfile構成やMarkdown中心設計を製品仕様として固定しない
- KPIは常時稼働時間ではなく、Sol High相当品質を可能な限り維持しながら親Codex/Solの実消費を削減し、長時間workflowを完了できることとする
- unblock時は保存済み構想を直接実装せず、その時点のproduction実体、実運用、escaped bug、quota、telemetry、resume architecture、hardening状況から要件を再レビューする

## Must not

- 現在のACTIVE taskを中断しない
- architecture / decomposition gateをumbrella implementationとして直接dispatchしない
- 親architecture決定前にPoC、公開interface、state schema、production implementationを開始しない
- 最新指示による優先変更を、021の中断または021由来でないtaskの無条件優先へ拡張しない
- 現在のrepository固有運用をそのまま製品化しない
- Markdown追加だけ、固定Plan消化、conversation context依存、giant agent化で解決しない
- planning、scheduling、recovery、worker executionの責務を無検討に混在させない
- GLMへproject-level semantic decision authorityを移さない
- interface候補やdata候補を確定済み仕様として扱わない
- 独立Harness、新しいPlanner、state model、schedulerを導入前提として評価を進めない
- GLMにcommit/pushさせない

## Acceptance criteria

- 2026-09-02の優先変更指示がAmendmentsへlosslessに保存され、021由来の新規採用taskがないことと022参照が解決されている
- production hardening成熟の再評価根拠とユーザーの明示許可がtracked metadataから回収できる
- 責務分離、state model、planning / scheduling、replanning、amendment、quota / crash recovery、concurrency、repository配置、Git ownership、telemetry、A/B評価の採否とrollback境界が一次証拠付きで決定される
- 現状維持、既存機能の限定拡張、新規構成、No-Goが同列に比較され、過去構想を既定解としない親Codex判断が記録される
- 採用範囲はsemanticな個別taskへ分割され、022より前の実行順としてPlanへ反映される。No-Goの場合は実装taskを作らない
- architecture gate中はproduction source、scheduler、runtime stateを変更せず、GLMへproject-level semantic decision authorityを移さない

## Historical invariants

- 現行の手動Plan/Task/History運用は将来Harnessの参考経験であり、製品仕様ではない
- glm-workerは可能な限り小さく強いimplementation executorとして維持する方向を優先候補とする
- 最上位評価はDirect Codex対Codex + glm-workerのCodex ReductionとQuality Deltaである

## Dependencies

- 現在のglm-worker既存機能、contract、mechanical guard、production behaviorが十分固まったと親Codexが一次証拠から再評価すること
- ユーザーが本案を再レビューし、明示的に着手を許可すること

## Review findings

none

## Current boundary

2026-09-03の親architecture gateで、新規独立Harness・Planner・state model・schedulerはNo-Go、運用は現状維持と決定した。既存機能の限定拡張は、親の反復作業が主要コストである一次証拠を得た場合だけ個別semantic taskとして再評価する。production source・scheduler・runtime stateは変更せず、本taskから022より前へ追加するimplementation taskはない。独立review受理済みで、task完了metadata同期を残す。
