# Task: Publication前の未達阻止とCandidate/Remote source identityの継続

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「そして最終的なFindingはCodexに実装させようと思う」。

## Resolved references

初期の未達Complete・Remote Pushと撤回（#1356 F01 / #1376 P01）、promote後の修正不能停止（#1376 P02）、Metadata/index不一致（#1359 G08/#1360 H08/#1363 K07）、本Auditで自己promote済みcommitを外部進行と誤認した停止（#1392 V05）。今回のTaskで修正された経路もあるため、必要条件の再現と現行Owner照合が先。

- PR #1345 / comment 4254066693: fresh successorの一部guardはあるがWorkflowConfigが先行する。materialize確定後のruntime failureで結果を捨て、正規recoverはmaterialization reentryを再実行しない。controllerとruntimeの中断整合を対象にする。
- PR #1345 / comment 6136663377: inline materialization指摘と同じcontroller/runtime境界。別Taskを作らず、割込み・再実行でのidempotent reconcileを同じacceptanceに含める。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/app/controller_execution.go:executeControllerMaterialize`
- `glm-worker/internal/app/controller_execution.go:reenterMaterializedRuntimeCompletion`
- `glm-worker/internal/app/controller_reentry_review_repair.go:executeControllerExecutionRecovery`

- PR #1349 / comment 4269331505: Previousを無条件に遡り、変更前BaseOID/TreeOIDのParentSourceを返す。後段archive照合は古いsourceを拒否するため正当なrebind後materializeが停止し得る。current candidateと同じbase/treeの区間だけを継承する。

- `glm-worker/internal/controller/materialization_parent_source.go:materializationParentSource`
- `glm-worker/internal/controller/materialization_parent_source.go:validateMaterializationParentSource`
- `glm-worker/internal/controller/publication_rebind.go:reboundCandidate`

- PR #1381 / comment 4362202676: pendingは先頭のapplyPendingControllerMaintenanceHandoffで救済されるためその部分は除外。no-live-leaseかつcurrent Task改訂・accepted candidateなしではsync拒否がquiescent ownerを遮断する残余範囲だけを対象にする。

- `glm-worker/internal/app/parent_handoff_controller.go:applyCanonicalControllerHandoff`
- `glm-worker/internal/app/parent_handoff_controller.go:applyTaskAuthoritySyncHandoff`
- `glm-worker/internal/app/parent_handoff_reentry_source.go:applyReentrySourceHandoff`
- `glm-worker/internal/controller/task_authority_sync.go:taskAuthoritySyncExecutionReason`

- PR #1390 / comment 4370316242: StateStore.ReadはI/O errorで空文字を返す。empty taskID分岐がtaskErrより先で、非ErrNotExistをno-open-reviewへ変換する。errorを先に評価し状態不変で伝播する。

- `glm-worker/internal/state/store.go:Read`
- `glm-worker/internal/state/parent_review_state.go:parentReviewResolutionState`
- `glm-worker/internal/state/parent_review_state.go:resolveParentReviewStateWithCompletion`

## Amendments

### 2026-10-25 External Reviewから同一責務へ統合: materialization-runtime-reentry-recovery

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
controller materialization確定とruntime再入を正規recoverで整合させる。
fresh successorは不要なpredecessor runtime再入処理とconfig検証へ入れない。predecessor付きattemptではcontroller確定後のruntime失敗・中断を遷移identity付きで回収する。
既存journal/recovery ownerでidempotentにruntime binding・accepted completion・review状態を整合し、現行controllerにないtransitionを推測採用しない。
materialization結果がerrorで失われる場合でもcanonical handoff/recoverから同じ操作を識別し、安全に続行できるようにする。
````

### 2026-10-25 External Reviewから同一責務へ統合: materialization-parent-source-lineage

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
current candidateと異なるbase/treeのParentSourceをmaterializeへ継承しない。
Previous lineageを歩く際にcurrent BaseOID/TreeOIDとの一致区間を境界とし、rebind等で変わった旧sourceを返さない。transition名だけから継承可否を推定しない。
一致するreenter/revalidate/promote/adoptの正当な継承と、cycle/破損/archive不一致の拒否を維持する。
````

### 2026-10-25 External Reviewから同一責務へ統合: task-authority-sync-quiescent-handoff

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
no-live-lease状態の同期拒否で合法なquiescent操作を隠さない。
pending transitionは現行applyPendingControllerMaintenanceHandoffの正規recoveryを維持し、その修正済み範囲を再実装しない。
Task改訂かつlive leaseなしでsyncをadmitできない場合、正当なquiescent materialize等へ進む操作をcanonical ownerから提示する。current要求を古いTaskで実行しない。
accepted terminal candidateのsource-bound metadataと真正Task改訂を区別し、同期不能をConsistent=trueへ偽装しない。
````

### 2026-10-25 External Reviewから同一責務へ統合: parent-review-task-id-read-errors

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
task.idのI/O errorをno-open-reviewへ変換しない。
ErrNotExist、他のread error、成功した空taskID、成功したTaskIDを順に区別し、非ErrNotExistを元のerrorとして伝播する。
accept/no-go/completion resolutionが読取失敗時にopen reviewやcompletion stateを変更しないことを既存ownerで保護する。
````

## Purpose

Controllerのaccepted candidate、terminal Task/Plan、primaryの既知base/index、remote upstream、真の外部変更を同一source authorityで扱う。

## External feasibility

status: not-applicable

## Contract

Controllerのaccepted candidate、terminal Task/Plan、primaryの既知base/index、remote upstream、真の外部変更を同一source authorityで扱う。Acceptance/実Bundle観測を修正可能段階で検査する。promote後のown commitと外部Pushを区別し、失敗時は正規reentryでsource-bound Review/Validationへ戻す。

External Reviewで確認した同一責務の具体条件（materialization-runtime-reentry-recovery）:

- fresh successorは不要なpredecessor runtime再入処理とconfig検証へ入れない。predecessor付きattemptではcontroller確定後のruntime失敗・中断を遷移identity付きで回収する。
- 既存journal/recovery ownerでidempotentにruntime binding・accepted completion・review状態を整合し、現行controllerにないtransitionを推測採用しない。
- materialization結果がerrorで失われる場合でもcanonical handoff/recoverから同じ操作を識別し、安全に続行できるようにする。

External Reviewで確認した同一責務の具体条件（materialization-parent-source-lineage）:

- Previous lineageを歩く際にcurrent BaseOID/TreeOIDとの一致区間を境界とし、rebind等で変わった旧sourceを返さない。transition名だけから継承可否を推定しない。
- 一致するreenter/revalidate/promote/adoptの正当な継承と、cycle/破損/archive不一致の拒否を維持する。

External Reviewで確認した同一責務の具体条件（task-authority-sync-quiescent-handoff）:

- pending transitionは現行applyPendingControllerMaintenanceHandoffの正規recoveryを維持し、その修正済み範囲を再実装しない。
- Task改訂かつlive leaseなしでsyncをadmitできない場合、正当なquiescent materialize等へ進む操作をcanonical ownerから提示する。current要求を古いTaskで実行しない。
- accepted terminal candidateのsource-bound metadataと真正Task改訂を区別し、同期不能をConsistent=trueへ偽装しない。

External Reviewで確認した同一責務の具体条件（parent-review-task-id-read-errors）:

- ErrNotExist、他のread error、成功した空taskID、成功したTaskIDを順に区別し、非ErrNotExistを元のerrorとして伝播する。
- accept/no-go/completion resolutionが読取失敗時にopen reviewやcompletion stateを変更しないことを既存ownerで保護する。

## Must not

Remote Push後の未達を通常復旧にしない。Complete後の再開APIは追加しない。外部変更をown candidateとして認めず、Guard/独立Reviewを弱めない。

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

正規入口E2Eでpreaccept未達、正当なbase、own promote、真正external advance、post-promote evidence mismatchを別々に再現する。未達時Remoteは不変、修正後はfresh検証・install・Bundle固定・単一公開・Retireが成立する。既存修正で有効なら重複実装しない。

- controller確定直後、runtime binding更新中、review準備前後の中断から正規recoverが収束する。
- fresh successor成功、predecessor再入、stale/別Task/binding不一致の拒否を実証する。
- 新規Attemptの二重生成や過去PASSの流用なしに独立Reviewへ戻る。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

- parent-source revision後にbaseまたはtreeを変更するrebindを挟んでも、正当なmaterializeが古いarchiveのために停止しない。
- 同じbase/treeでは必要sourceを継承し、cycle/偽sourceは拒否する。

- no-live-lease+Task改訂の正当状態でcontroller-evidenceだけの行き止まりにならない。
- pending/recover、sealed candidate、idle live lease、別Task/破損の拒否を維持する。

- task.idのdirectory/I/O失敗がerrorとして返り、open reviewは解消されない。
- 不存在/空IDの既定挙動と正常accept/no-goが維持される。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
