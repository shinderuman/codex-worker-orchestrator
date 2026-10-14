# Task: execution worktree方式の技術的必要性とトレードオフ評価

## Original instruction

````text
# 追加指示 — Worktree設計評価を独立Taskとして登録する

現在のACTIVE Task：
`IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md`

Worktree方式そのものの評価は、現在のACTIVE Taskから切り離す。

以下の独立Taskを新規作成し、`IMPLEMENTATION_PLAN.local.md`のNEXTへ登録する。

候補ファイル名：
`IMPLEMENTATION_TASKS/execution-worktree-architecture-review.md`

既存Taskに同等の要求が登録されていないか確認し、重複を避ける。

## 新Taskの目的

現在のControllerが通常のWorker実行で専用detached HEAD worktreeを使用する設計について、その必要性、利点、欠点、維持コスト、代替設計を評価する。

特に以下を対象とする。

1. primary worktreeではなく専用execution laneを使う技術的必然性
2. Controller authority、lease、Snapshot、Git index、branch管理との関係
3. primaryとexecution lane間の実装成果・証拠・metadataの移送
4. Git hook、Guard、commit、Publication、Retireとの整合性
5. 専用laneによって必要になっているcleanup、materialize、reenter、suspension等の状態管理
6. primaryで直接実装する場合の安全性・単純性・制約
7. 現行設計を維持する場合と変更する場合の具体的なトレードオフ

設計評価では、現行方式を既存実装という理由だけで正当化せず、反対に複雑だという印象だけで撤廃もしない。

実コード、既存テスト、Gitの仕様、過去の障害記録から判断する。

## 新Taskの終了条件

- 現行方式の必要性とコストを説明できる。
- primary直接実行との比較が具体的な不変条件に基づいている。
- 実際の問題と、単なる設計上の選択を区別できる。
- 変更が不要なら、Findingなし・コード変更なしで終了してよい。
- 変更が必要なら、影響範囲と安全性を確認し、リポジトリの通常実装・Review・Validation規則に従って修正する。
- リポジトリ共通の後方互換性禁止を遵守する。

## 現ACTIVE Taskへの影響

現ACTIVE Taskの実行を中断・切替しない。

直前の方針修正に従い、専用Worktreeの撤廃や実行アーキテクチャの再設計を現在Taskの完了条件としない。

ただし、現在の正規Workflowで実際に発生しているGit hook・Controller・Runtime間の不整合は引き続き現在Taskで修正する。

新Taskを登録したことを理由に、現ACTIVE Taskに必要な不具合修正を先送りしない。

新TaskのPlan上の位置は既存の優先順位・依存関係を考慮して決める。予定されている通常開発TaskとBundle再監査Taskの順序制約を壊さない。

**今回はTask定義とPlanへの登録のみ行い、新Taskの調査・実装は開始しない。**

登録後は現在のACTIVE Taskを継続し、正規Workflowを完遂する。
````

## Amendments

### 2026-10-11 登録位置の修正（原文）

````text
Bundle再監査の前だ
````

### 2026-10-13 未達Workflowの引継ぎ（ユーザー原文の本Task該当節）

````text
## 3. Worktree見直しTaskに未解決事項を引き継ぐ

`execution-worktree-architecture-review` の既存定義が現在の作業成果またはrepository authorityに存在するか確認する。

存在する場合は、そのTaskへ引き継ぐ。存在しない場合は、既存のTask管理規則に従って登録する。重複Taskを作らないこと。

引継ぎには少なくとも以下を含める。

- execution laneの生成、再利用、cleanup、再生成のLifecycle
- cleanup済みWorktreeをRuntimeが参照し続けるgeneration 713の不具合
- Runtime Snapshot取得先とController／Handoff／materializeのauthority境界
- cleanup後の正常な再開と、保存済み実装成果の継承
- 今回完走できなかったPublication・Retireを含む正規WorkflowのEnd-to-End検証
- 今回の実装で修正済みの事項と、未解決事項の明確な区別
- 再現手順、失敗証拠、関連する既存Test・Review・Validationの参照

Worktree全体の設計を見直し、局所的な対症療法を繰り返さなくて済むようにする。

ただし、今回のTaskではその設計・実装に着手しない。

````

最新指示の全原文は `IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` の2026-10-13 Amendmentを正とする。本Taskへの引継ぎは実行開始ではない。「正常Workflow修復を本Taskへ先送りしない」という以前の制約は、ユーザーが今回明示した未達事項の移管に限りoverrideする。

### 2026-10-13 Plan・Task整理と実行境界（ユーザー原文の本Task該当節）

````text
既存の実行順序は次のとおり維持する。

ACTIVE:

- `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md`

NEXT先頭:

- `IMPLEMENTATION_TASKS/execution-worktree-architecture-review.md`
- `IMPLEMENTATION_TASKS/dogfood-bundle-three-task-audit.md`

その他のNEXTは、既存の相対順序を維持する。

旧Task `IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` は、ユーザー判断によって実装成果を確定し、未達事項をWorktree見直しTaskへ引き継いでいる。

旧TaskはACTIVEへ復帰させず、BLOCKEDに保持する。通常のsemantic acceptance、Publication、Retireが成功したと扱わない。

ACTIVEへのCLI Task登録はスケジュールの確定だけであり、このセッションでのTask activation、GLM dispatch、Runtime再開を意味しない。

Worktree見直しTaskに、旧Taskからの未達事項が引き継がれていることを確認する。

特にgeneration 713で停止したcleanup後のRuntime Snapshot／Handoff／materialize、および未達のPublication・Retireが対象に含まれていることを確認する。

Task本文に残る旧ACTIVE前提の記述、現在のScheduleと矛盾するderived contractは確認する。必要なら正規のAmendment／derived contractの規則に従い整合させる。

ただし、Original instructionや既存Amendmentsの原文を削除・改変しない。

このセッションでは実Taskを開始しない。
````

今回の指示はmetadata整理だけを許可する。未達事項の移管は既存2026-10-13 Amendmentを維持し、登録時の旧ACTIVE継続・正常Workflow修復を先送りしない制約は、その移管に限り上書きされる。実行順序はPlanだけを正とする。

### 2026-10-13 旧Workflow Taskのスケジュール除外とMetadata Lint（ユーザー原文）

````text
# 追加指示 — 旧Workflow Taskの整理とMetadata Lint必須化

今回のPlan・Task整理について、以下の2点を追加で実施する。

1. `normal-workflow-snapshot-stop-regression.md`を実行スケジュールから正式に除外する。
2. Metadata LintのPASSを必須の完了条件とする。

このセッションは引き続きMetadata整理専用とし、実開発Taskは開始しない。

## 1. 旧Workflow Taskの整理

対象：

`IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md`

このTaskはユーザー判断で作業を打ち切り、実装成果をGitへ確定し、未達事項を`execution-worktree-architecture-review.md`へ移管している。

通常のPublication・Retireは成功していないが、旧Task自体を再開する予定はない。

したがって、BLOCKEDへ残して再開待ちTaskとして管理することは不適切である。

以下を実施する。

- `IMPLEMENTATION_PLAN.local.md`のBLOCKEDから旧Taskを除外する。
- Worktree Taskへの未達事項の移管が成立していることを確認する。
- 旧Taskの原要求、変更履歴、必要な証拠参照がGit履歴や既存証拠から追跡可能であることを確認する。
- 上記を確認したうえで、旧Taskファイルをcurrent treeから削除する。
- 他TaskのDependencies、Resolved references、derived contractなど、旧Taskへの参照を調査する。
- 旧Taskの正常完了を前提とする参照は、実際の引継ぎ先と現在の要求に整合させる。
- 旧Taskを成功済みdependencyとして扱わず、`Fulfilled dependencies`へ機械的に移動しない。
- Git履歴上の旧Taskを参照すべき箇所は、必要に応じて実在するcommitをlocatorとする。

旧Taskの削除をsemantic acceptance、Publication、Retireの成功と扱わない。

また、旧Taskの内容をWorktree Taskへ無条件に全文複製しない。既存の移管内容に不足がある場合だけ、その不足を解消する。

## 2. Metadata Lintを必ず通す

**MetadataのLint違反0件を今回の必須完了条件とする。**

実装コード側のテスト失敗は今回に限り許容するが、Metadata Lintの失敗・未実施は許容しない。

- 既存の正規repository lint（`./harnesslint`）を実行する。
- ACTIVE、NEXT、BLOCKEDのSchedule構文と一意性を検証する。
- Task corpusの参照閉包を検証する。
- TaskのDependenciesと参照先を検証する。
- Task contract、関連するMetadata規則の違反を確認する。
- Metadata関連の違反があれば修正し、Lintを再実行する。
- **修正後の最終状態に対してLintがPASSしたことを確認する。**

Lintを通すための検査無効化、例外追加、Guard迂回、対象ファイル除外は認めない。

既存Lintが検出できないMetadata上の意味的な不整合も、確認できたものは今回の整理範囲で解消する。

production codeやtestの修正が必要になった場合は勝手に作業を拡張せず、STOPして報告する。

## 3. Git確定

今回の変更は新規コミットにせず、既存のMetadata整理コミットへAmendする。

- 最初にcurrent HEADとAmend対象を照合する。
- 想定したMetadataコミットでなければSTOPする。
- 整理対象のMetadataだけを変更する。
- 差分を確認し、意図しない変更を含めない。
- 最終Metadata LintがPASSした後に`git commit --amend`する。
- Amend後のHEADで最終差分とLint結果の対応を確認する。
- 新規コミットは作らない。
- **Push・Force Pushは一切行わない。ユーザー自身が行う。**

Git操作の制約により正常なAmendができない場合は、状態を捏造したりGuardを無効化したりせず、具体的な障害を報告してSTOPする。

## 4. 完了報告

以下を報告してSTOPする。

- 旧TaskのPlanからの除外とcurrent treeからの削除結果
- Worktree Taskへの未達事項の移管確認
- 他Taskの参照・Dependenciesの整理結果
- 最終PlanのACTIVE／NEXT／BLOCKED
- Metadata Lintの実行コマンド、終了コード、違反件数
- Amend後のcommit SHA
- Push未実施の確認

**Metadata LintがPASSするまで完了扱いにしない。**

本セッションではGLM実行、Controller／Runtime操作、通常Taskの開始、実装、Review、Publication・Retireを行わない。

Metadata整理を完了したらSTOPする。
````

今回の指示は親metadata整理だけに適用し、本Taskの実開発を開始しない。旧TaskをBLOCKEDに保持する以前の指示は上書きされる。既存Amendments内の旧Task参照はcurrent treeの要求sourceではなく、下記Git履歴locatorの原文を指す。旧Taskの削除は通常semantic acceptance・Publication・Retireの成功を意味せず、既存の未達Workflow引継ぎを維持する。

### 2026-10-13 実行順序の変更（ユーザー原文）

````text
| - \`IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md\`       |
| --------------------------------------------------------------------- |
| - \`IMPLEMENTATION_TASKS/execution-worktree-architecture-review\.md\` |
| 下をActiveに、上をNEXTの先頭にしろ                                                |
````

本指示は以前のCLI先行指定を上書きするschedule変更であり、実行順序はPlanだけを正とする。今回のmetadata整理専用・GLM実行禁止・通常Task開始禁止・Guard迂回禁止・Push禁止の境界を維持する。

### 2026-10-13 新規セッションの設計評価・修復要件（ユーザー原文のsemantic該当節）

````text
旧Task `normal-workflow-snapshot-stop-regression.md`を復活・再実行しない。

旧Taskの履歴参照`c6c47b6`は現在のmainから到達不能なので、その参照を使用する前に実際の取得可能性を確認する。現在のTask本文、到達可能なGit履歴、保全済みimmutable evidenceを使い、存在しない証拠を推測で補完しない。

## 2. 設計評価と原因調査

TaskのOriginal instruction、Amendments、Contract、Review findings、Acceptance criteriaを確認し、専用Execution Worktree方式の必要性と代替設計を評価する。

特に、旧Taskから引き継いだgeneration 713の障害を対象とする。

- cleanup後にRuntimeが削除済みWorktreeを参照する問題
- Controller、Runtime、Snapshot、Handoff、materializeのauthority境界
- execution laneの生成、seal、reenter、cleanup、再生成
- 保存済み実装成果とimmutable evidenceの継承
- Git hook、Publication、Retire、Task終了後の正常状態

専用Worktreeの維持・変更・撤廃の選択肢を、実コードと再現証拠に基づいて比較する。

現在の方式を無条件に維持せず、複雑だという理由だけで撤廃もしない。

重要なarchitecture／authority変更の採否は、既存のSol判断規則に従う。

## 3. 実装と検証

必要な変更が確認できた場合は、既存の正規Workflowに従ってGLMへ調査・実装・検証を委譲する。

局所的な対症療法を繰り返すのではなく、確認された原因とownerに対応する修正を実施する。

既存の実装成果、Git履歴、Controller State、Checkpoint、immutable evidenceを破壊しない。

Taskの主要Acceptance criteriaである、以下の実経路を検証する。

accept/seal → reenter → cleanup → materialize → Runtime再束縛 → Worker／Review／Validation → Publication → Retire

特に、cleanup後の正常状態から保存済み成果を引き継ぎ、正規のPublication・Retireまで到達できることを確認する。

既存の一時復旧、例外Git操作、過去のPASS記録を新しいEnd-to-End成功の代用にしない。

必要な独立Review、Go tests、repository lint、build、install-smoke等は既存規則に従って実施する。

**Metadata Lintの失敗・未実施を許容しない。** Metadataを変更した場合は最終状態に対するLint PASSを必須とする。

コミット、Push、親Metadataの更新は既存の責務境界に従い、GLMにGit remote writeを行わせない。

## 4. 作業範囲

今回扱うのはWorktree設計評価と、そこへ移管された正規Lifecycle未達事項である。

CLI positive Task-start admission、Bundle再監査、コミット集約の後続Taskには着手しない。

問題がなければ不要なコード変更を作らない。問題がある場合は、実際の原因に対応する修正を行う。

過去セッションの判断を自動採用せず、最新の一次証拠を根拠に作業する。
````

### 2026-10-13 起動不能の限定復旧（ユーザー原文のsemantic該当節）

````text
# Worktree Task起動不能の限定復旧

現在のSTOPを確認した。

これは旧Workflow Taskをユーザー判断で中途終了し、Planから削除した一方、Controller／Runtimeがgeneration 713の旧Task authorityを保持しているために生じた不整合である。

**今回の目的は、旧Taskを復活・再実行することではなく、現在のACTIVEである `execution-worktree-architecture-review.md` を正規に開始できる状態へ復旧すること。**

## 1. 今回限定の調査許可

canonical authority bootstrapが失敗しているため、その失敗原因の診断に限り、primary repositoryのGit現物・tracked source・Rules・Plan・Taskと、Controller／Runtimeの保存状態をread-onlyで直接参照することを許可する。

これはbootstrap成功の代用ではない。

修復後は必ず正規bootstrapへ戻り、同一authorityからRules／Plan／ACTIVEを取得できることを確認する。

旧セッションの内部判断は正としない。

## 2. 確認する不整合

次の境界を確認する。

- `authoritybootstrapcmd/run.go`でControllerの旧Task参照がPlan ACTIVEを上書きする条件
- `parentcontinuation/projection.go`で削除済みexecution laneがGit Snapshot取得元になる条件
- `parent_handoff_controller.go`で不整合時に合法actionが提示されない条件
- generation 713のController head、accepted candidate、lease、pending transition
- RuntimeのTask binding、`repo-root`、status
- 最新mainのProjectSnapshotと、旧Controllerが保持するProjectSnapshotの関係
- 保存済みimmutable evidenceと実装成果の参照整合性

現在のheadやcandidateが有効であることを、存在するという理由だけで推定しない。

## 3. 復旧方法の決定

まず既存のController／Runtime ownerに、今回のようなユーザー判断によるTask中途終了と後続Taskへの移行を、成功Retireと区別して扱える正規transitionが存在するか確認する。

存在すれば、そのpreconditionと効果を検証して使用する。

存在しない場合は、既存ownerに復旧経路を実装する。ただし、旧Taskを正常完了したように見せるtransitionは作らない。

修正は少なくとも次の整合を成立させること。

1. 旧Taskのgeneration 713、終了理由、accepted candidate、immutable evidenceを保持する。
2. 旧Taskの終了を通常のsemantic acceptance／Publication／Retire成功へ変換しない。
3. 現在のPlan ACTIVEとControllerの新しい実行authorityを、正規の状態遷移で接続する。
4. 削除済みWorktreeへのRuntime Snapshot参照を解消する。
5. 新ACTIVEのbootstrapとhandoffを成功させ、合法なTask開始操作を提示する。

Stateファイルの直接書換え、Controller headの捏造、旧Taskの再作成、既存Guardの無効化、別Controllerの新設はしない。

## 4. 修正・検証の範囲

今回の復旧に限り、必要な既存ownerのソースと回帰テストについて親Codexによる直接編集を許可する。既存の直接編集規則には従うこと。

通常のWorktree設計評価・大規模リファクタリングは、この復旧に混ぜない。

変更が必要なら、少なくとも以下の回帰を確認する。

- ACTIVEが削除済み旧Taskを指さず、現在のPlanと一致する。
- 旧Taskの中途終了後も旧証拠を保持する。
- cleanup済みWorktreeをRuntimeが参照し続けない。
- 異なるTask、stale generation、不正なauthorityによる移行を拒否する。
- 正規bootstrapが成功する。
- handoffが`consistent:true`となり、合法な次Actionを提示する。
- Metadata LintがPASSする。

実装コードの品質検証も変更範囲に応じて実施する。失敗・未実施をPASSとして報告しない。

今回の復旧が実際に必要とした変更をWorktree Taskの実装・検証対象として扱い、以降は通常のGLM／独立Review／Publication／Retireの規則へ戻る。
````

今回の直接編集例外は起動不能の限定復旧にだけ適用する。以前の登録・metadata整理専用・実開発開始禁止は当時のセッションに限る。今回の開始・復旧後の継続権限は現在のユーザー指示を正とし、旧Taskの成功を推定しない。

### 2026-10-13 Handoffのaction_specs欠落の恒久修正（ユーザー原文のsemantic delta）

````text
今回の目的は、Handoffの不具合を恒久修正し、正規の`resubmit-report`で同一Taskの処理を再開すること。

現行コードでは、`glm-parent-action resubmit-report`とRuntimeのadmission処理は実装済みだが、`glm-worker/internal/parentactiongrammar/grammar.go`の`Project()`に`resubmit-report`の定義が欠落している。

まず実コードで原因を確認し、必要な恒久修正を実施する。

想定する修正は、`state.ParentActionResubmitReport`を既存のdirect actionとして登録し、以下のaction specを生成できるようにすること。

- `kind: direct`
- `command: ["glm-parent-action", "resubmit-report"]`

併せて回帰テストを追加する。

- full handoffで`resubmit-report`のaction specが提示される。
- recovery handoffでも同じaction specが提示される。
- `required_action`と`allowed_actions`との対応が成立する。
- 既存のone-shot制約やRuntime admissionを変更しない。
- その他のaction specを破壊しない。

原因が想定と異なる場合は、実際の原因に沿って修正する。

修正対象はHandoff／action grammarの既存ownerと必要なテストに限定する。

既存のRuntime install規則に従って修正済みバイナリを適用し、実際に使用する`glm-worker`／`glm-parent-action`が修正内容を反映していることを確認する。

変更に必要なテストとMetadata Lintを実施する。Metadata違反は許容しない。

作業中のTaskのGit Snapshot、Controller authority、Runtime binding、保存済み証拠を壊してまで修正を適用しないこと。

通常の修正・配置手順が現在の停止状態と衝突する場合は、その条件と合法な適用方法を調査する。Guardを迂回したり、Stateを直接書き換えたりしない。

正規の適用方法が存在しなければ、その時点でSTOPする。

`resubmit-report`は通常の正規操作であり、ユーザーによる特別な実行許可や回数制限の緩和は不要である。

実行失敗時に勝手に再試行しない。one-shot制約を変更しない。

`resume`、新規Task開始、GLMの初回実装呼出しによって代替しない。

Handoffを正しく修正することを優先し、手動コマンド指定やGuard迂回を恒久対策にしない。
````

本AmendmentはHandoff修復の要求を追加する。既存のWorktree設計評価と正規Lifecycle完了の要件を維持する。

### 2026-10-13 結果補正成功後のRule Activation接続修復（ユーザー原文のsemantic delta）

````text
今回の目的は、この状態遷移の不具合を恒久修正し、既存の成果と実行証拠を保全してWorktree Taskの通常Workflowを再開すること。

## 2. 恒久修正

結果補正packetの受理と、後続のRule Activationの責務境界を正しく分離する。

- `ResultCorrection`は結果packet補正の実行中だけ有効とする。
- 補正が成功した後の通常Worker継続へ、補正専用の状態を持ち越さない。
- Rule Activationは通常のCheckpointとして動作させる。
- Rule Activationに必要なRule context、Task identity、Snapshot、既存Contractを保持する。
- `ReadOnly`などの実行権限は、後続Phase自身の正規Contractから決定する。単純なflag解除によって書込権限を拡大しない。
- 補正失敗時のterminal判定、違反回数、identity検証、one-shot上限は維持する。
- 結果補正状態の削除を無条件に遅延させたり、存在しないStateを許容するfallbackを追加したりしない。

今回の直接原因に対応する既存ownerのみ修正する。

## 3. 既存Runtimeの復旧

現時点で`resubmit-report`を再実行しないこと。

まず次の証拠を照合する。

- 前回の再提出が実際に有効な結果packetを返したか
- packetの内容とTask／Session／Attempt identity
- そのpacketの保存先と再利用可能性
- 現在の`result-correction`記録とrecovery checkpoint
- one-shot再提出回数の実際の消費状況
- 失敗後のRuntime status
- generation 723のController authorityとの整合

既に受理されたpacketが正規の証拠から復元可能であれば、追加のモデル呼出しを避け、既存ownerが保証できる正規の継続経路を検討する。

ただし、欠落したpacketを推測で作成したり、成功済みであると偽装したりしない。

既存の再提出回数を0へ戻さない。terminal record、resume checkpoint、Controller headを直接書き換えない。

新しい回復操作が必要な場合は、元の成功結果、正確なidentity、Snapshotを照合する限定的な正規transitionとして設計する。一般的なGuard解除や無制限retryを導入しない。

保存証拠から安全に継続できない場合は、追加のGLM呼出しを行う前にSTOPし、何が失われて何を回復できないのか報告する。

## 4. 回帰テスト

実際の失敗経路を再現するテストを追加する。

最低限、以下を確認する。

1. 結果packetの形式不正でresult-correctionへ入る。
2. 補正上限到達後、正規`resubmit-report`が1回だけ許可される。
3. 再提出された有効な`IMPLEMENTED` packetが受理される。
4. Rule Activationが必要な実diffを用意し、補正成功後に追加Rule Activationを実行する。
5. 後続Phaseが削除済みのresult-correction状態を参照しない。
6. 正しいWorker権限とRule contextを維持する。
7. 独立Reviewへ正常に接続する。
8. 不正なidentity、Snapshot不一致、再提出回数超過は従来どおり拒否する。
9. 結果補正自体が失敗する経路の挙動も維持する。

モデルの実呼出しを使わないdeterministicな回帰テストを優先する。

## 5. Lintの修復

今回の報告ではrepository lintがFAILしている。

- Metadata違反：0件
- complexity違反：2件
- unused違反：1件

これらの正確なファイル、行、違反内容を再取得する。

今回の復旧コードに由来する違反であれば、既存ownerの責務を維持しながら修正する。

未使用コードの放置、Lint対象の除外、閾値変更、検査無効化はしない。

既存Lintの全違反を確認し、修正後は`./harnesslint`のPASSを必須とする。

修正が今回の責務を明確に超える場合は、その理由と対象を報告し、無断で別Taskの実装を開始しない。

必要なテスト、build、Lint、installを正規の規則に従って実施する。

修正済みsourceと本配置バイナリの一致を確認する。

ただし、Runtimeの停止状態を破壊する操作や、検証されていないController transitionを実施しない。
````

本Amendmentは結果補正と後続Phaseの境界修復、証拠に基づく限定復旧、および復旧コードのLint修復を追加する。旧Taskを復活せず、本Taskの既存Lifecycle完了要件を維持する。

### 2026-10-13 第2milestoneの正規Decision経路復旧（ユーザー原文のsemantic delta）

````text
第一目標は、GLM → Codexの意味判断 → GLMへの再委譲という正規ルートを成立させること。

これに必要な修正は後回しにしない。それ以外の実装・テスト・Lint修正・追加改善は、正規経路が通ってからGLMに担当させる。

第2milestoneでは、GLMが`NEEDS_SOL_DECISION`を繰り返し提出している。

しかし提出packetに`parent_validation=go-test`、`parent_validation_working_dir=glm-worker`が残っており、現在のWorker Schemaに違反している。

その結果、StructuredOutputが繰り返し拒否され、`error_max_structured_output_retries`へ到達している。

これを単なるpacketバイト数超過やSchema未対応と決めつけないこと。

まず、現行の`packet/schema.go`、`packet/validate.go`、Worker instruction、Decision routing、execution milestone resumeの整合を確認する。

特に、Workerが`NEEDS_SOL_DECISION`を返す場合、parent validationのtyped pairを空にする必要があることを確認する。

`IMPLEMENTED`で必要なparent validation義務は消さず、後続の通常実装報告で維持する。

GLMは専用Execution Worktree方式の設計評価を行い、Codexによるarchitecture採否を求めている。

これはTask Contractに沿った意味判断であり、通常のGLM実装結果として無理やり`IMPLEMENTED`へ変換してはならない。

既存の`NEEDS_SOL_DECISION`経路が使用できるようにする。

- current schemaを変更する前に、正しい既存packetで通るか確認する。
- Worker instructionとschemaに矛盾がある場合は、現在のContractに合わせて修正する。
- GLMが正しいpacketを出せるようにすることを優先する。
- 同じ不正packetを繰り返させない。
- 上限緩和、Schema弱体化、validator迂回によって成功扱いしない。
- 未受理packetを受理済みの証拠として登録しない。

Codexによる直接コード修正は、GLMへの正常な委譲・復帰経路に不可欠な部分だけに限定する。

それ以外の修正はGLMへ委譲する。

Checkpointや停止状態を直接書き換えない。GLM Taskを新規開始しない。第1milestoneを再実行しない。

Decision packetが正常に到達したら、Codexは保存済み設計評価と一次証拠を確認し、未確定architectureについて意味判断を行う。

その判断を既存の正規Decision actionで渡し、同じWorktree TaskのGLM処理を継続させる。

GLMに委譲するもの：

- 採用されたarchitectureに必要な実装
- 回帰テストの追加・修正
- 既存テストFAIL 2件の調査・修正
- Lint・build・Validationに必要な修正
- Task Acceptanceを満たすための残作業

Codexはこれらを直接実装しない。

独立Reviewや最終Validation等は、それらの正規Phaseに到達してから実施する。

最上位目的はCodex Reduction。

Codexによる作業介入を増やすのではなく、機械的な委譲・復帰経路を成立させる。

後方互換性は禁止。旧Schema対応、migration、dual protocol、互換fallback、廃止経路の復活は追加しない。

今回の停止状態だけを救済する使い捨て機構も、必要性が証明されない限り追加しない。

既存のTask／Attempt、execution lane、受理済み成果、immutable evidence、Controller authorityを保護する。

新規TaskやIssueを増やさず、現在のTaskの実行経路復旧として扱う。
````

本Amendmentは正規Decision経路の復旧を最優先とし、既存schemaの正しいpacketを先に確認する。親の直接修復は委譲・復帰に不可欠な範囲へ限定し、それ以外の実装・テスト・Lint修正はGLMへ委譲する。本Taskの既存Acceptance criteriaと通常Lifecycle完了要件を維持する。

### 2026-10-14: 1 Task 1 Commitと終了MetadataのAmend

ユーザー原文:

> 実装完了のコミット→次タスクの昇格のときにまとめてMetadataをAmendもしくは単一コミット

> 俺は1Task 1Commitにしたい
> 逆言えばそれ以上に分ける理由が見つからないからだ
> お前は以前実装を1度コミットしたあとMetadataはAmendするって運用にしていただろ
> いつからか変えていたが
> 前のその運用でいいんじゃねえの

このTaskの終了は、実装完了コミットへ終了・次Task昇格のMetadataをAmendし、最終Publicationを単一コミットで行う。従来のMetadata先行確定やPublication後の別Metadataコミットを必要とするownerの接続は、既存Guardと証拠の責務を維持して修正する。後続Taskの開始は行わない。

### 2026-10-14: GLM独立Reviewの再開とTask完了

残作業はなに？GLMのReview？もうGLMのLimitは解けているからReviewが必要なら実施してTaskの完了まで持ってけ

GLM独立Reviewを正規に再開し、最新sourceへのReview帰属、Validation、Publication、Retireまで完了する。Codexによる直接実装の許可は継続し、GLMへ許可する今回の処理は独立Reviewとする。1 Task 1 Commit、既存証拠・authorityの保全、後続Taskを開始しない制約を維持する。

### 2026-10-14: 区切りでの例外的Git保存と独立不具合の優先

ユーザー原文:

````text
いまの修正状況は割と切りが良いところではあるのか？
であれば一度ここでコミットしてPushしろ
それを妨げる場合はこの場限りの特例を許す
その不具合は次のActiveに積め
````

この指示に限り、親rollout証拠関連付けにより正規Publicationが停止している実装成果と関連Metadataを単一コミットで保存・Pushする。必要なGit hookの例外は当該commit/pushプロセスのみに限定し、永続設定・Guard実装・Controller Stateを変更しない。この保存・公開を通常のcandidate accept/seal、Publication、Retireの成功へ変換しない。

親rollout証拠関連付けの独立修復Taskを先にACTIVEとする。本Taskは未達の通常Lifecycle検証を保持して再開対象に残す。正規経路の復旧後は保存成果を継承し、実装・第1milestone・報告再提出をやり直さず残るAcceptanceを満たす。この例外は今後のGuard回避・検証省略・公開済みcommitのAmendを許可しない。

## Resolved references

- 旧Taskの原要求・Amendments・証拠参照の履歴locatorは `c6c47b6e9dd1c6e334c96515c99d95ba5f2813c4:IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md`。`git show c6c47b6e9dd1c6e334c96515c99d95ba5f2813c4:IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` で取得する。上記の登録・引継ぎ時の旧Task参照はこの履歴へ解決し、旧Taskを再開・正常完了の前提や成功済みdependencyとしない。

- 本TaskはWorktree設計評価の独立登録指示と、2026-10-13の未達Workflow引継ぎ指示を正とする。旧Taskの通常semantic acceptance・Publication・Retire成功を本Taskの前提にしない。登録だけでは実行を開始しない。
- 実行順序はPlanだけを正とする。Original instruction中の「現在のACTIVE Task」は登録当時の旧Taskを指し、実行・再開権限を付与しない。

## Purpose

専用detached execution laneとprimary直接実行を、具体的不変条件・安全性・保守コストに基づいて比較する。現行維持と変更の双方を証拠から判断する。

## External feasibility

status: not-applicable

## Contract

- 2026-10-14の区切り指示に従い、今回の単一Gitコミット・Pushは例外的な成果保存として扱う。通常Publication・Retireの実経路Acceptanceは維持し、その成功と区別する。既に公開した保存コミットはAmendせず、再開時のGit基準と公開証拠の扱いを正規ownerで確定する。

- このTaskの実装とTask終了・次Task昇格のMetadataは、未公開の実装コミットへのAmendを経て、main上の単一コミットとしてPublicationする。ControllerのPublication・Retire証拠はその最終OIDへ束縛する。

- 旧Taskのスケジュール除外・current treeからの削除後も、既存の未達Workflow引継ぎを維持する。旧Taskの原文参照はResolved referencesのGit履歴locatorへ解決し、削除を通常semantic acceptance・Publication・Retire成功と扱わない。

- 最新Amendmentで引き継いだcleanup後の再入、Runtime SnapshotとController/Handoff/materializeの境界、保存実装継承、Git hook/Guard、Publication・Retireを含む正規Workflowを評価・必要な範囲で修正する。通常経路によるEnd-to-End証明を必要とし、過去の一時State復旧・Git例外による確定をその成功証拠へ転用しない。
- lane生成・再利用・seal・cleanup・再生成を一つのLifecycleとして評価する。削除済みlaneの無条件復元、未知Snapshotのadopt、authorityの第二正本、旧candidateの証拠有効化で迂回しない。

- 調査・設計案・必要な実装・検証はGLMへ委譲し、未確定のarchitecture/authority/永続状態/公開APIの採否はSol判断を通す。
- 原文の7評価軸を、実コード・既存テスト・Git一次仕様・過去の保全証拠で評価する。印象による正当化・撤廃をしない。
- authority、lease、Snapshot、index、branch、成果/証拠/metadata移送、hook/Guard、commit/Publication/Retire、中断復旧を具体的な不変条件と再現ケースで比較する。
- 専用laneのcleanup/materialize/reenter/suspensionの必要性とコスト、primary直接実行の制約・安全性を明示する。
- 変更が不要ならFindingなし・実装変更なしで終了できる。変更が必要なら影響範囲と安全性を確定し、通常の実装・独立Review・Validationを行う。
- current schema/contractだけを正規入力とし、共通規則の後方互換性禁止を遵守する。

## Must not

- metadata整理・Task登録・schedule変更だけで調査・実装・Lifecycleを開始しない。旧Taskの追加実装・再実行を本Taskの前提にしない。
- 最新Amendmentによる未達事項の移管を扱う。それ以外の無関係な修正を本Taskへ集約しない。
- 抽象的な隔離や単なる複雑さ・ファイル数だけで設計を採用・撤廃しない。
- 既存の未コミット成果やimmutable evidenceを破棄しない。Guardを迂回せず、任意authorityを採用しない。
- 旧schema migration、alias、互換fallback、第二State正本を追加しない。
- GLMにGit remote write・commit・parent-managed metadata編集を許可しない。

## Acceptance criteria

- accept/seal → reenter → cleanup → materialize → Runtimeの正規再束縛 → Worker/Review/Validation → 新candidate → Publication → Retireを実Git・既存ownerで検証する。cleanup後の正常なquiescent状態から、一時State修復なしで保存実装を継承できることを確認する。
- generation 713型の削除済みworkspace参照と合法action欠落を回帰で固定し、旧証拠・Checkpoint・失敗履歴の保全、stale generation／別Task／不正Workspace／未知mutation拒否を維持する。

1. 原文7軸について必要性・利点・欠点・維持コスト・代替案を一次証拠で説明できる。
2. primary直接実行との比較を具体的不変条件で示し、実障害と設計選択を区別できる。
3. Solの採否とその根拠を確定する。不要な変更を成果の条件にしない。
4. 変更が必要なら、通常規則に従い実装・独立Review・変更に適切なValidationとPublication/Retireを完了する。実装なしの場合は評価成果を既存artifactへ保全して正常終了する。
5. 成果・証拠保全、中断復旧、Git hook/Guard、authority、後方互換性禁止の要件を欠落させない。

## Review findings

### cleanup後のRuntime Snapshot参照とHandoff欠落

- 再現経路は、同じTaskの実Controller accept/seal後、正規reenterでcandidate evidenceを無効化し、sealed laneを正規cleanupする経路である。cleanup transition `05307c8c-8431-462d-acdf-1141ddbf580c` はgeneration 710→713でfinalizedとなった。cleanup CLIのexitは128であり、CLI全体の正常終了とは区別する。
- lane削除後もRuntimeの`repo-root`がそのlaneを参照し、Handoffが `current repository snapshot is unavailable: resolve git snapshot HEAD: git rev-parse --git-dir: exit status 128` を返す。Controllerにlive Attempt／lease／pending transitionがない正常なquiescent状態で、通常materialize・継続actionが提示されない。Runtimeは旧Taskの `awaiting-parent-completion` に残る。workspaceの実在とSnapshot取得先、Controller authority、Runtime binding、Handoff投影を既存ownerの責務で接続する。
- 一次証拠: `/Volumes/CodexVault/normal-workflow-24989cdb/history-import/quiescent-cleanup-handoff-stop-713/stop-summary.json`、同directoryのController head、execution operation、seal、cleanup archive、Runtime保存、transport。ユーザー区切りの照合・保全は `/Volumes/CodexVault/normal-workflow-24989cdb/history-import/user-directed-close-713/preflight.json` と `manifest.json` を正とする。
- parent metadataのcommit/push境界でも、pending Controller Publicationがないため既存pre-push Guardが拒否した。Guardを無効化して正規成功と扱わず、要求更新・成果確定・Publication authorityの境界をE2E確認する。今回の例外的Git確定の内容と最終OIDは同保全先のclosure結果を参照する。

### 既存実装と検証を継承する境界

- 保存済み成果のsource treeは `2c61029a7e73421eba7d289c586577569a302a13`。実装101パス（従来tracked変更77、untracked新規24）で、親metadataや診断用operational executorを含まない。candidate・seal・cleanup archiveとReview／Validationのsource proofが同じtreeを参照する。旧candidateやimmutable evidenceは書き換えない。
- 実装済みの対象は、Retire後のbootstrap/Handoff/親metadata同期、sealと実データに基づくlane保全cleanup、canonical Runtime identityと重複Handoff、Git authorityのスコープ、Controller再入とRuntime再束縛、accept/seal後Handoff、Packet bounded設計と報告再提出、quality fixer mutationと削除済みuntracked承認、rebind remote observation、明示的Task amendmentのauthority継承、Rate Limit等でのauto-fix pending-decision接続である。コードと個別回帰の成果であり、実機Lifecycle全体の成功を意味しない。
- 独立Reviewの生結果は `NEEDS_SOL_REVIEW / HIGH`、model call `1a4e7259-c2e1-4449-a7c6-bf622e51c53d`。親の意味判断を経て受理した。full Go run `d2b7f6e0a0ef75be96cb1afad0a261de` とrepository lint（Fixed=0）はPASS。install-smoke run `7142c9ccccd96e1335040eab8ad6d245` はPASSだが、旧run recordのSnapshot欄は空であり、前後の実Snapshot一致による補助bindingを持つ。通常installはlive leaseのないgeneration 707で行われた。詳細とexact source locatorは上記保全先の `source-proofs/` を参照する。
- reenterによりcandidate EvidenceValidはfalseとなっている。既存検証は同一ソースの実績として継承し、新base・新snapshot・新candidateの公開用Review／Validationは正規に確定する。正規Publication、Retire、親metadata同期、次Task開始可能状態は未達である。
- 関連回帰: `glm-worker/internal/app/controller_reentry_workflow_test.go`、`glm-worker/internal/app/retired_workflow_test.go`、`glm-worker/internal/controller/retired_lane_cleanup_test.go`、`glm-worker/internal/controller/publication_rebind_observation_test.go`、`glm-worker/internal/controller/publication_amendment_rebind_test.go`、`glm-worker/internal/state/parent_reentry_test.go`、`glm-worker/internal/workflow/parent_validation_live_controller_test.go`、`glm-worker/internal/workflow/quality_mutation_test.go`、`glm-worker/internal/workflow/quality_surface_early_stop_test.go`、`glm-worker/internal/workflow/report_only_snapshot_test.go`。既存coverageを利用し、不足しているcleanup後の実経路だけを追加する。
- 再開時は最新Git/remote/Controller/Runtime/Planをbootstrapで再確認する。今回のTaskを新しいGLM実行として作り直したり、generation 713を無条件に現在値と仮定したりしない。ユーザー方針による区切りと正常semantic acceptanceを区別し、残ったauthority・終了状態の扱いを検証してから通常操作を選ぶ。

## Historical invariants

- canonical Controller authorityと親の意味判断、GLM実装/独立Review分離を維持する。
- 既存の例外復旧は設計安全性や正常経路の成功証拠へ転用しない。

## Dependencies

none
