# Execution Worktree方式の評価

## 採否

専用detached execution laneを維持する。Gitそのものが要求する構成ではないが、primaryの利用者作業・index・branchを保持しながら、Controllerが実行権限と候補を管理する現在の責務に適合する。primary直接実行へ変更する根拠となる、安全性を維持した簡素化は今回確認していない。既存実装やテストの存在だけを維持理由にはしない。

generation 713型の障害は、laneの物理削除後もRuntimeの保存pathをSnapshot取得元としていたowner間の接続不備であり、worktreeを使うこと自体の必然的な失敗ではない。現在の`parentcontinuation.quiescentSnapshotRoot`はlive Attempt/leaseのないactive Controllerでprimaryを取得元とする。`controller.MaterializeExecution`とappの`reenterMaterializedRuntimeCompletion`は保存Suspensionと新Attemptを結び、Runtimeを正規に再束縛する。未知pathの復元や旧candidate evidenceの再有効化は採用しない。

## 比較する不変条件

| 評価軸 | 専用laneの成立条件・利点 | 維持コスト | primary直接実行との差 |
|---|---|---|---|
| 技術的必要性 | 同一object storeを使い、実行用HEADとindexをprimaryから分離する | lane生成・占有・削除の管理 | primary直接実行もGitとして可能。ただし利用者と同じindex/worktreeを排他的に扱う必要がある |
| authority / lease / Snapshot | Workspace nonce、Task、Attempt、generation、Snapshotへmutationを束縛する | pathだけではidentityにならず、再生成後の旧権限を拒否する必要 | primaryでもleaseとSnapshot検証は残る。worktree削除がなくなるだけではこれらを撤去できない |
| 成果・証拠・metadata | Suspensionのindex/worktree treeとsealで成果を保持し、metadataを親専有とする | rebind時のmetadata合成・成果保全の検証 | primaryなら一部移送を省けるが、利用者の既存dirty dataと実装成果を区別する必要がある |
| hook / commit / Publication / Retire | 実行はdetached、最終候補だけを検証してprimary/remoteへ公開する | final OID、Guard、Runtime終了の接続 | primaryへ直接書いても公開前の候補固定、source binding、Guard、remote照合は必要 |
| cleanup / materialize / reenter / suspension | seal閉包を確認して削除し、保存成果から同Taskの新Attemptを生成する | quiescent時の取得元選択、削除済pathからの再入、失敗recover | primaryを消す操作は不要。ただし中断成果・失敗履歴・別Task切替の保存は引き続き必要 |
| primaryの安全性と制約 | 利用者のbranch/indexを通常実装の書込先にしない | worktree間の参照解決と共通Git状態へのGuard | primary方式は利用者編集との排他を強めるか、変更検知で中断する必要があり、現在の分離を失う |
| 維持・変更の総合判断 | 現ownerでauthorityと公開境界を維持できる | lane固有状態の回帰検証が不可欠 | 撤廃にはprimary排他・dirty成果保全・候補生成の再設計が必要。今回その追加設計を正当化する利益は未検証 |

Gitの[worktree一次仕様](https://git-scm.com/docs/git-worktree)では、linked worktreeごとにHEADとindex等の管理fileを持ち、多くのrefとobjectを共有する。detached HEADも指定できる。この分離は別repositoryによるsecurity境界ではない。[update-ref一次仕様](https://git-scm.com/docs/git-update-ref)の旧OID照合・transactionだけでも、利用者のworktreeやindex全体を占有から保護することはできない。

## 現物の対応箇所

- `glm-worker/internal/controller/execution_lane.go`: materialize時のTask/Project/generation、Suspension identity、終了済実行の拒否、成果tree再束縛。
- `glm-worker/internal/controller/execution_materialize.go`: laneのGit directoryとmaterializeの成立条件。
- `glm-worker/internal/controller/retired_lane_cleanup_test.go`: staged/unstagedの保全、ignored dataと未知nonceの拒否、cleanup中の競合、再生成後の旧権限拒否。
- `glm-worker/internal/parentcontinuation/projection.go`: quiescent Snapshot取得元とcanonical Taskの投影。
- `glm-worker/internal/app/controller_execution.go`: cleanup/materializeの型付き入口とRuntime再束縛。
- `glm-worker/internal/workflow/controller_workflow_reentry.go`: predecessor Attemptの一致、既存sessionの失効、停止Checkpointの再構成。
- `glm-worker/internal/app/controller_reentry_workflow_test.go`: stale generation・別candidateの拒否、旧candidateのPublication拒否、同TaskのFix入口。

## 検証の範囲と限界

`TestCanonicalValidationCadenceWithoutRuntimeRepair/cleanup-reentry`は既存cadenceシナリオへ追加した。実Git repositoryとbare remote、正規CLI、既存Controller/Runtime ownerを使い、accept/seal → reenter → lane cleanup → primaryからのHandoff/bootstrap → 保存Suspensionからのmaterialize → 同TaskのWorker/独立Reviewer → 新candidate → final validation → Publication/Retire → cleanupを検証する。cleanup後の追加State修復は行わない。保存成果、stale generation拒否、seal閉包、同じRuntime Task identity、最終candidate OIDとremoteの一致、完了Task削除、後続Task未開始も検証する。

このシナリオのmodelとGo quality processはfixture代替である。実Gitとowner接続の証拠にはなるが、実model・実full suite・本配置・本repositoryのPublication/Retireの成功証拠にはしない。実full suiteと公開結果はcanonical validation/operation recordを正とする。

過去Taskの一時復旧・例外的Git保存・古いPASSを今回の成功へ転用しない。generation 713の記録はACTIVE Taskの原要求と既存保全locatorに由来する歴史上の障害として扱い、現在generationと同一とは仮定しない。旧Taskを復活せず、既存immutable evidence・失敗履歴を保持する。

## 過去validationとの境界

同じsemantic Taskを新しいRuntime Task IDで実行すると、同じstoreに以前のgate/smoke記録が残る。Bundle収集では、現在Taskの記録を採用し、同じstoreのcurrent-schema archiveが以前のTask identityを証明する記録だけを現在Taskから除外する。以前のrunやarchive自体は削除・変更しない。未知Task、欠損・破損・identity不一致のarchiveは拒否し、他TaskのPASSを現在Taskへ昇格しない。

この選択は`controller.evidenceLiveCollector.taskStoreGateRunBinds`がgate/smoke共通で行い、archiveの成立性は既存の`state.ArchivedTaskStatsEvidence`を使う。新しいschema・互換reader・履歴storeは追加しない。`TestExportEvidenceRetainsAndExcludesProvenHistoricalValidation`と既存のforeign Task拒否回帰で、過去実行の保全と現在実行の証拠帰属を同時に確認する。
