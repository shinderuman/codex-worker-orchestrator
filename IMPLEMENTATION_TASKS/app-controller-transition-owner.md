# Task: appのController・runtime遷移調停を単一ownerへ集約する

## Original instruction

2026-10-29 親Codex採用要求:
appがController更新とruntime更新の順序・証拠採取・失敗回収を所有する構造を解消する。既存のjournalと正規遷移ownerを利用し、appはtyped CLI入力と結果出力を担当する。初期化前runtime、同期、復旧、handoffを同じ状態契約へ接続し、今後の機能追加でもappへ個別の復旧分岐を積み増さない。

## Amendments

### 2026-11-03 Codexの先行採否

採否原文: https://github.com/shinderuman/codex-worker-orchestrator/pull/1483#issuecomment-6588601674

````text
Codexの設計採否です。確認対象はhead `65b55e7f284d58beb34fcc353436384d7ef5039f`、基点は `e3dcec1525adbee32e33078928510c94dcdbb404` です。

**workflowを遷移調停ownerとする構成と共有admissionの方向は採用します。現headの完成認定は保留です。** 下記の責務・復旧条件を確定し、Lint違反と検証不足を本PRで解消してください。

### 1. 遷移調停owner：workflow案を採用

依存は `app → workflow → controller / state` とし、`controller → workflow` は追加しません。

- app：typed commandのdecode、既存の実行/lock境界、ownerへの単回委譲、結果/errorのencode。
- workflow：同期前証拠の採取・照合、Controller操作とruntime completionの順序、部分失敗後の既存completionへの再接続。
- controller.Store：generation/task/source/lease/workspaceのadmission、既存journal、prepare/commit/recover、canonical execution authority。
- stateおよび既存runtime owner：runtime binding、milestone、review checkpoint等の保存と既存の冪等な更新。

app内のaction別順序・復旧・重複判定は残しません。ファイル移動だけでなく、新しい同期条件がworkflow側で閉じる構成としてください。別journal、汎用遷移framework、旧経路へのfallbackは不要です。

### 2. 共有admission：controllerへの配置を採用

`controller.CurrentSemanticAdmission(config.AppConfig, *controller.Store)` を共通入口にする案を認めます。controllerには既にconfigを受けるStore/WorkflowConfig/ParentContinuationConfig等があり、この追加自体は新しい逆依存ではありません。

既存のcanonical binding解決、MutationAuthorityFromHead、workspace/source snapshotの取得、AdmitMutationOrFailClosedを再利用してください。渡されたconfigとStoreが異なるrepositoryやTaskを指しても採用されず、stale generation/lease/attempt/sourceを拒否することが条件です。runtime evidenceのschemaやreviewer checkpointの意味判定をcontrollerへ移す理由にはしません。

appに共有関数を一度呼ぶだけのprivate forwarding wrapperを残す必要はありません。canonicalな型・ownerを直接参照し、旧関数名維持を目的とする互換wrapper/aliasは追加しないでください。

### 3. 部分成功と復旧：既存transition IDへの再接続を採用

(a) Controller commit後のruntime失敗：
commit済みのtransition IDとjournalを保持し、同じ `recover(transition_id)` で未完のruntime completionへ接続します。再materialize、新規attempt生成、Controller側の成功を消して最初からやり直す処置はしません。

(b) runtime binding更新途中：
既存completion ownerがold/中間/newの正当な状態を識別して残りを完了し、異なるTask ID・attempt・Task path/digest・sourceを誤adoptしないこと。証拠一致の判定を「何かbindingがある」「Task IDが空でない」だけで代替しません。

(c) review checkpoint保存後：
既存のretained reviewer stop判定に加え、当該canonical attempt/Task contract/sourceへの帰属が一致するときだけ再準備を省略します。単にstatusがinterruptedという理由で成功扱いしません。欠損・不一致・別sourceのcheckpointはfail closedです。正常なrecoverの繰返しでattempt、review checkpoint、停止境界を増殖・上書きしないこと。

(d) runtimeを持たないController操作：
journalのtyped operationと保存証拠によりruntime completion不要と確定できる場合は、そのままControllerの復旧結果を返します。無関係なWorkflowConfigの読取りや架空runtimeの初期化を要求しません。逆に、runtime証拠が必要な操作の証拠欠損を「controller-onlyだから完了」としてはいけません。

recoverの権限確認は既存journal ownerへ委ね、親に現在generationの推測やState補正を要求しない構成を維持します。失敗時もtransition IDと原因を既存error contextに残し、canonical handoffから合法な復旧へ進めることを確認してください。error本文から親にcommandを組み立てさせず、失敗stdoutを成功JSONへ変更しません。

handoffの意味状態はController/stateとcompletion結果から既存canonical ownerが投影します。appに新たな状態推定policyを積み増さず、遷移後の実副作用とhandoffの一致をCLI接続testで確認してください。

### 4. scope・検証・現在のLint結果

`controller-execution` の統合を中心とするscopeを採用します。先行完了Taskの不具合再実装、Publication全般、transport統一は本PRへ混ぜません。新unstarted条件をworkflow内で扱う例も、本Taskの責務改善を示す方向として妥当です。

ただし、追加された `TestControllerMaterializationRecoveryReplaysRuntimeCompletion` は、正常materialize後のrecoverとtransition ID/generationの比較です。これだけでは「Controller commit直後にruntime completionが失敗した」境界を検証したことになりません。既存fixtureを使い、実commit→未完runtime→typed recoverを再現し、binding・checkpoint・handoffの意味までassertしてください。同期前、Controller更新後、runtime更新後の重要境界を分け、State直接補正で復旧を代替しないこと。重いfixtureは共有できるsetupを共有し、assertをhelperへ隠して行数だけ減らさないでください。fixture削減の根拠は変更前後のscenario/fixture起動数等で示し、コード行数削減をCodex削減率へ外挿しません。

[push run #41524687492](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41524687492)を確認したところ、記載の「実行中」から既に結果が進んでいます。qualityのRepository Lintは**8件でfailure**、app/controller/test-2はcancelled、vet/buildとinstall-smokeはskippedです。test-1のsuccessを全体PASSと扱わないでください。

具体的な8件：

- gofmt：`app/controller_execution.go`、`workflow/controller_execution_transition.go`
- thin-wrapper-proliferation：`app/controller_execution.go:60`、`app/controller_semantic.go:152`
- test-size-limit：`app/controller_task_authority_sync_test.go:500`
- cyclop：`completeMaterializedRuntime`、`VerifyTaskAuthoritySyncRuntime`（いずれも13、上限12）
- instruction-content-hash：`workflow/controller_transition_evidence_boundary_test.go:15`

最後のhash指摘は、固定のMarkdown hashをpinする自己確認なのか、productionが所有するTask contract digestの照合を直接検証するケースなのかを区別してください。後者の意味まで削ってLintを通すことは認めません。既存のcanonical authority生成fixture/ownerを使って同じ正負のbehaviorを検証できるならそこへ収束し、helperへhash処理を隠すだけの回避はしないこと。guard自体のfalse positive修正が必要と判明した場合は、その最小例と根拠をCodexへ返し、本PRで勝手にlintを緩和しないでください。

最終exact SHAで関連test・lint・必要な正規gateと独立Reviewを成立させてから採否を確定します。Task/Planの削除・同期は親Codexが担当します。Web GPT側で先行削除せず、checkpoint先頭の順序も変更しないでください。
````

### 2026-11-03 統合時のadmissionとretained review採否

- 共有admissionとworkflow公開入口は、渡されたconfigとStoreのGit lineageおよびStateBaseを照合する。別repository/別StateBaseへRuntimeやControllerを暗黙生成して整合させず、Controller ownerの単一scope検証で拒否する。controller-only recoveryにもRuntimeの生成・読取りは要求しない。
- retained reviewer stopの省略判定は、current Task種別を含むbindingと実workspace snapshotの一致を確認する。保存済みworker-endとcheckpoint間の一致だけを、実sourceへの帰属の代用にしない。
- commit後のcompletion失敗は既存transition IDと元原因をerror contextに保持し、同じtyped recoveryへ接続する。新しいjournalや旧関数名の互換経路は作らない。

### 2026-11-03 終了Metadataのユーザー指示

原文（順序どおり）:

````text
最後のコミットで
\- [\`IMPLEMENTATION_TASKS/codex-efficiency-post-adoption-checkpoint.md\`]\(IMPLEMENTATION_TASKS/codex-efficiency-post-adoption-checkpoint.md\)\
が先頭に戻るようにしておけよ
````

````text
IMPLEMENTATION_TASKS/codex-efficiency-followup-checkpoint.md
いやこっちだった
````

- 最新の訂正を採用し、終了Metadataで`codex-efficiency-followup-checkpoint.md`を再開してPlanのACTIVE先頭へ戻す。同じ評価責務の`codex-efficiency-post-adoption-checkpoint.md`は未実施の重複として統合する。完了扱いにせず、実評価は次TaskとしてCodexに残す。

## Resolved references

- 根拠: `review-evidence/codex-efficiency-checkpoint-2026-10-29.md`。
- 具体的な要求sync不整合は`task-revision-worker-admission.md`、Publication sourceは`canonical-publication-source-continuity.md`、輸送は`communication-transport-unification.md`が所有する。

## Purpose

遷移の成功・部分失敗・再実行の意味をCLIから分離し、追加のたびに調停処理がappへ集中する構造を解消する。

## External feasibility

status: not-applicable

## Contract

- Controllerからworkflowへ依存するcycleを作らず、既存の遷移ownerとworkflow接続の責務を親判断で確定してから実装する。
- source/task/generation/leaseとruntime bindingの整合、途中errorの実副作用、正規復旧後のhandoffを一つの遷移契約で扱う。
- appに順序policyや重複判定を残さない。必要な異なるtrust boundaryの再検証は保持する。
- owner単体の失敗点検証と、CLIからの少数の正常・中断・復旧接続testに分離し、production手順をfixtureのstate補正で代替しない。

## Must not

- 別journal/state DB/daemon、汎用巨大framework、旧schema互換を追加しない。
- ファイル移動のみ、error握り潰し、bindingだけを手動補正したテストを改善と扱わない。
- 先行Taskで修正済みの具体的不具合を再実装しない。

## Acceptance criteria

- appのtyped dispatchからownerへ一度委譲し、意味的な遷移手順・復旧がowner内で閉じる。
- 同期前・Controller更新後・runtime更新後の中断からidempotentに回復し、handoffの状態と実副作用が一致する。
- 新しい同期条件を追加する変更例でappへのpolicy分岐追加が不要なことを示す。
- 必要な品質検証を保持し、重いfixture反復を減らした根拠を示す。

## Historical invariants

- source-bound evidence、権限分離、fail closed、親の最終採否を維持する。

## Dependencies

none

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/task-revision-worker-admission.md`

- `IMPLEMENTATION_TASKS/canonical-publication-source-continuity.md`
