# Task: 通常経路のsnapshot不整合停止の条件差と必要な正規経路の復旧

## Original instruction

以下はユーザーの追加指示の原文である。

````text
多分この次のタスクはいまの先頭のやつじゃなくて「通常経路で不整合停止が再発するなら直す」タスクを積むべきだと思う
そもそもBundleは通常経路が復旧してる前提で作業しているはずなのにさっきから何度もコケまくってるし

今回の5h limit復帰のタイミングじゃコケなかったな
コケるときとコケないときの違いはなんなんだ
それも含めてBundleが終わったらやるタスクにするべきだろう
````

## Amendments

以下はユーザーの追加指示の原文である。

````text
1コミットにしろ

正規経路がないなら正規経路を作れるようにするタスクを用意しろ
それは次のタスクで同時にやれ

1タスク1コミットってのはPlanの書き換えまでを必ずしも含んではいないぞ
1タスクで
a.go
b.go
c.go
の修正が全部バラバラのコミットになるようなクソみたいなことはするなという話だ

Planの変更まで縛ってしまうと今度はPlan開始してないタイミングでタスクを追加、修正することができなくなってしまうだろ
````

1 Task＝1 commitは、同じTaskの実装修正をファイル別など複数commitへ分割しない要求である。Plan更新やTask終了metadataまで同じcommitに含める必要はない。親が追加した「親metadata・実装・Task終了をすべて同じcommitに収める正規経路」の要求は、ユーザーの意図と異なったため取り消す。通常経路での不整合停止の調査・修正と、実際に不足する正規lifecycle経路の確認・整備を、このTaskで扱う。既存の正規経路が使える場合はそれを使い、不足が確認された場合だけ既存canonical ownerの責務内で整備する。現在のBundle Taskの未公開実装・review・validation・install evidenceを保全する。Bundleを完了済みと扱わず、本Taskの登録だけで実行開始したことにも扱わない。

## Resolved references

- Bundle Taskの通常経路の証拠: glm-worker runtime Task ID `d0d22e0b-6bb6-4efe-97ac-54efd75f0ad4`、canonical Attempt `233b6799-ca36-491a-9df5-a9c1630a0f91`。Taskのtelemetry/events、controllerのmodel-call/lease/mutation/failure証拠、completed Bundleを一次資料とする。
- 保存済みの今回限りの運用State操作receipt: `/private/tmp/glm-bundle-audit-state-switch/`。実結果と一時的なState操作を区別するための入力であり、恒久的な復旧機能を追加する根拠として扱わない。
- 検出surfaceは通常glm-parent-action decision/fix/resumeの実行、およびworker-auto-fix/reviewer入口のparent metadata checkに現れた `execution workspace snapshot does not match live lease` / `parent_metadata_active_unresolvable`。後者の文言だけでmetadata内容が改変・欠損したと断定しない。
- 公開前のreadonly previewは `base_oid=4d8bed4fe4f0a784d0604bbb1ab956ed0b39af5f`、`snapshot_id=c71812a685d0ce8f76efca9b3e258ccccec2f504dc675eed5b1b0383eaefb325`、`tree_oid=5c5704a61526fc431711a4bd82fcadb7764c00b8`。実装25ファイルを含み、準備済みの親metadataを含まなかった。実装を1 commitにまとめ、metadataを別commitにすること自体はユーザーの要求に反しない。
- 正規経路の確認対象: `glm-worker/internal/app/controller_publication.go`、`glm-worker/internal/controller/publication_acceptance.go`、`glm-worker/internal/controller/terminal_planning.go`、`glm-worker/internal/controller/publication_adoption.go`、`glm-worker/internal/controller/publication_mutable_adoption.go`。既存retire処理がTask終了metadataを別commitにすることだけを不具合や経路不足と扱わない。
- 1 commit指定後の再開bootstrapでも `authority bootstrap: resolve controller execution task: execution workspace snapshot does not match live lease` が発生した。直前のacceptは成功していた。原因は未確定で、readonly previewが原因だと推定しない。

## Purpose

通常glm-worker workflowで成功する境界と不整合停止する境界の条件差を一次証拠から特定し、実装の回帰であれば正規ownerの責務内で修正する。必要な正規lifecycle経路が実際に不足している場合は、その不足も同時に解消する。同じTaskの実装修正は1 commitにまとめ、次の作業を一時operatorやState捏造なしで継続できる状態にする。

## External feasibility

status: not-applicable

## Contract

- 実装開始前にcurrent Git、導入前後のGit/Issue/PR、既存testsとcontroller/runner/workflowの正規contractを調査する。未確定の原因や比較条件を推測で設計しない。
- 同じBundle Taskの成功・失敗ケースを比較する。quota停止前、停止checkpoint、quota回復直後、worker終了、quality gate/lint/format/full Go終了、auto-fix/review入口の各境界で、HEAD/index/worktree/ref digest、snapshot ID、generation、lease/attempt/model-call identity、mutation owner/recordの対応を復元する。
- 5h limitからの自己resumeが通ったケースと、resumeで止まったケース、resume後に別境界で止まったケースを区別する。成功したquota復帰を後続停止へ付け替えず、実装変更・Git変更・親metadata変更・volatile refs・gateによる変更のどれが原因かは確認した証拠で判断する。
- controller authority、runnerの既存volatile-ref境界、worker変更権限、machine gateの変更責務、親metadata/read-only境界の不一致を調べる。scopeを問わず比較を緩和する修正や任意snapshot採用を先に選ばない。
- データだけの不整合か、通常経路で再発するソースコードの不具合かを判定する。ソース不具合を確認した場合だけ、root causeと修正semantic boundaryをSolへ提示して正規ownerで修正する。既存contractに照らしたfalse positiveの場合は不要な修正をしない。
- 調査・修正・独立review・validationはGLMへ委譲し、親は意味判断とmetadataを所有する。過去の長大調査を無条件に反復せず、未確定点と対照ケースに限定する。
- 不整合停止が妨げている親metadata登録・candidate生成・accept/seal・publish・Task retirement・次Task admissionのうち、該当する既存ownerと履歴・testsを調べる。同じTaskの実装修正を1 commitへまとめられる既存経路は維持し、Plan/Task終了metadataを別commitにすることを理由にpublicationを再設計しない。必要な正規経路の不足が確認された場合は、責務・authority・公開API・永続状態の意味をSolへ提示してから実装する。
- 親によるPlan/Task定義の追加・修正はTask実行とは独立したmetadata操作として扱う。Task開始前やTaskが実行されていない時にも行えることを維持し、1 Task＝1 commitの制約をmetadata操作へ拡張しない。metadata編集を行うためだけにTaskを開始したり、登録したNEXTを実行開始扱いしたりしない。既存canonical authority・親専有境界は維持する。
- commitをまとめるためにreview・validation・install・sealed evidence・remote OID確認・Task終了のpostconditionを省略しない。親metadataを先にTask終了へ変更することで未完了を完了扱いする経路を作らない。GLMによる親metadata編集やGit remote writeを許可しない。
- 現在のBundle Taskはreview/validation/installまでの採用済み結果を保持しているが、publication/retirementは未完了である。既存成果とexact evidenceを引き継ぎ、publicationの不足をBundle未完了と区別して扱う。ソースが変わらない部分の調査・実装を無条件にやり直さず、最終公開対象の変化に必要な検証を行う。

## Must not

- 今回は本Taskの要求定義を準備するだけとし、自動開始しない。開始時はBundleの未完了publication/lifecycleを保全して正規Task handoverを行い、Bundle完了を捏造しない。Bundle exporter本体やCLI admissionの別責務の実装を本Taskへ混ぜない。
- failed-closed状態を通すことだけを目的に、恒久的なbootstrap exception、汎用recovery/fallback/互換command、第二authority/state DBを追加しない。
- 今回限りのState修復、checkpoint捏造、一時binary、check無効化を本Taskの正規成功条件にしない。generation rollback、state削除、head直接上書き、証拠の再帰属やPASS捏造を行わない。
- actual mutation/task executionのlocking、parent metadata不変性、未知のGit/source変更に対するfail-closedを弱めない。
- userが書き換えたGit/Remote履歴を再書換えしない。GLMにcommit/pushやparent metadata編集を許可しない。
- 原因未確認のまま過去のcontroller recovery patchを清書・再install・恒久化しない。

## Acceptance criteria

1. 停止するケースとしないケースの対照、quota復帰の成否と後続停止の区別、各snapshot/lease/mutationの因果をexact証拠で説明できる。
2. データ不整合・ソース不具合・観測/操作の誤りを区別し、ソース修正が必要なら根本原因と採用した正規semantic boundaryを説明できる。
3. 実装回帰を確認した場合、通常worker変更→gate→auto-fix/review、およびstop/quota resumeを正規コマンドで実行し、一時operator/State修復なしで同じTaskのlifecycleを進められる。
4. 必要なregression testsで正規変更の受理と非正規source/Git/metadata変更の安全停止を両方保証する。失敗・成功の実データから得た条件差をtestに含める。
5. repository lint、必要なfull Go tests、vet、build、install-smoke、独立review、通常配置後のproduction smokeを通す。未検証を成功扱いしない。
6. 同じTaskの実装修正をファイル別など複数commitへ分割せず、1 commitにまとめて正規公開できる。Plan/Task終了metadataは別commitでよい。履歴書き換えを必要とせず、レビュー・validation・install・controller evidenceのcanonical bindingとremote OID一致を保持する。既存経路で成立する場合、これだけを目的にソースを変更しない。
7. 正規publication/lifecycle経路を変更する場合は、変更境界に応じた失敗・中断・retryのregression coverageを用意する。実際の公開状態とcontroller/Task状態を区別して保持し、同じ実装修正の重複commit・未公開状態の完了扱い・未許可NEXT開始を防ぐ。
8. Task終了後、通常配置したglm-worker/glm-parent-actionで次Taskの正規開始が可能な状態を確認する。確認だけを理由に無関係なNEXT Taskの実装を開始しない。

## Historical invariants

- single canonical repository controller authorityを維持する。
- Codex ReductionとSol Highの意味判断を維持し、原調査・実装・検証をGLMへ委譲する。
- 一時運用例外と通常経路のソース修正を混同しない。

## Dependencies

none
