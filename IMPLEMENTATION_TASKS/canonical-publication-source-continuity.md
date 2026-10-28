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

- 追加指示の「今回のFinding」: Dogfood Bundle `a7e5e47e-e8ed-4ce4-9e58-0226578059a8`の実経路。既存のaccepted candidateを保持したまま親が`git pull --ff-only`でprimaryをfast-forwardし、candidateのsource/base権限と現在のGit/remoteが不一致になった。親は古いsourceのまま候補採用とfull Go/race/installを進め、Promoteで初めて`local ref is outside candidate promotion authority`を受けた。既存の正規rebindが見つかるまで不要な探索とユーザー介入を生み、rebind後にReview・Validationのやり直しが必要になった。対象はPullそのものの禁止ではなく、**Pull後に不整合の候補で高コスト処理へ入れてしまう前段admissionの欠落**である。

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

### 2026-10-27 必須検証欠落の再発防止Taskとの責務境界

親Codexによる範囲の明確化:

````text
full/race等の必須検証集合がWorker宣言から欠落して未達のまま公開・完了した問題は、completion-required-validation-closure.mdで扱う。同Taskは検証集合の完全性と、未完了・復旧中の同一Task identity維持を担当する。本Taskはその正規ownerを再利用し、candidate/remote/source lineage、promoteと外部変更の識別、controller/runtime再入の整合に残る不備を扱う。未達阻止の要求は維持し、同じ検証義務判定や復旧状態を二重実装しない。
````

### 2026-10-29 Pull後の古いCandidateを最終検証前に扱う（ユーザー追加指示）

ユーザー指示（原文）:

````text
順序を直すだけじゃなくて今回のFindingも確実に解消するようにしたほうがいいんじゃないか
既存の文章で同様の問題を解消しようとしているなら問題ない
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

- 正当な`git pull --ff-only`等でprimary/local refが前進した場合、accepted/prepared candidate・Controller head・active Task/Planのsource binding・remoteとの関係を、**古い候補のaccept/completionおよび高コストな最終Go/race validation・install/smokeより前**に正規ownerで判定する。Task本文が変わらないMetadataだけのfast-forwardでも一致と決めつけない。stale sourceを認めてPromoteまで進めるのではなく、合法なrebind/authority同期をtyped next actionとして提示・実行可能にする（真に権限不足/衝突なら理由付きfail-closed）。推測でsourceをadoptしたり、Pull自体を禁止したりしない。
- rebindが必要な場合は元のcandidate/Review/Validation証拠を無条件に流用せず、rebindで更新したsource・candidate identityへReviewと必要なValidationを再束縛する。逆に候補がcurrent sourceと一致する通常経路では、追加の検証・モデル呼出を無意味に発生させない。既存`publication_rebind.go`とcanonical controller completion/projectionを再利用し、親CodexがCLIを探索して復旧できることを唯一のadmissionとしない。

## Must not

Remote Push後の未達を通常復旧にしない。Complete後の再開APIは追加しない。外部変更をown candidateとして認めず、Guard/独立Reviewを弱めない。

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

- primaryのfast-forwardによるsource不一致をPromoteで初めて見つける設計を、後段が正しく拒否したからという理由で正常扱いしない。古いsourceに対してfull Go/race/installを実行してから拒否・rebindする循環を残さない。

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

- **正規入口E2Eで今回のPull事故を回帰テスト化する**。未公開候補を保持している間にMetadataだけがfast-forwardする合法な`git pull --ff-only`を発生させ、(1) stale candidateのaccept/completionまたは最終Go/race/install着手**前**に不一致を検出し、(2) 正規rebind/authority同期への合法な操作が投影され、(3) 古いsourceのReview/Validationを新sourceへ黙って流用せず、(4) fresh Reviewer・必要なValidation・installと単一Publicationで正常収束し、(5) 不一致時はremoteを変更しないことを実証する。今回のようなPromote時だけの拒否はAcceptance未達とする。
- Pullなし・同一sourceの通常成功、真正外部remote進行、衝突や権限不明の拒否、rebind中断・再入を別々に検証する。時系列のsource-bound evidenceで、Pull後の不要なfull/race/install先行と親の手探り復旧が再発しないことを示す。削減効果は実測wall/tool/model roundtripで検証し、推測による時間短縮値で代用しない。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
