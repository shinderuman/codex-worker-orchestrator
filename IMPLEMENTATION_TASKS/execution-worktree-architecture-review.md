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

## Resolved references

- 旧Taskの原要求・Amendments・証拠参照の履歴locatorは `c6c47b6e9dd1c6e334c96515c99d95ba5f2813c4:IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md`。`git show c6c47b6e9dd1c6e334c96515c99d95ba5f2813c4:IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md` で取得する。上記の登録・引継ぎ時の旧Task参照はこの履歴へ解決し、旧Taskを再開・正常完了の前提や成功済みdependencyとしない。

- 本TaskはWorktree設計評価の独立登録指示と、2026-10-13の未達Workflow引継ぎ指示を正とする。旧Taskの通常semantic acceptance・Publication・Retire成功を本Taskの前提にしない。登録だけでは実行を開始しない。
- 実行順序はPlanだけを正とする。Original instruction中の「現在のACTIVE Task」は登録当時の旧Taskを指し、実行・再開権限を付与しない。

## Purpose

専用detached execution laneとprimary直接実行を、具体的不変条件・安全性・保守コストに基づいて比較する。現行維持と変更の双方を証拠から判断する。

## External feasibility

status: not-applicable

## Contract

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
