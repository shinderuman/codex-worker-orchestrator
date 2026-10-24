# Task: 短いTASK指示から型付き正常完了操作への接続

## Original instruction

2026-10-24 ユーザー原文（連絡元チャットから転送された本Task該当要求）。

````text
新Taskの要求・採否観点:
- 短いTASKブロックだけで、新規Codexセッションが通常Lifecycleを完遂できること。親acceptから型付きcompletion操作へ接続し、Controllerが決められる固定値・evidence/source bindingを機械が供給し、親にはsemantic判断だけを残す。
- 汎用JSON slotへ終了手順全体の暗黙知を押し込めたり、実装コードを毎回探索してoperation JSONを手組みすること、追加の復旧指示・一時operator・State surgeryを通常運用前提にしない。
- 調査/監査のno-code Taskとruntime変更Taskの双方について、正常終了と失敗/再入、最終Validation・install/smoke・Bundle観測固定・Publication・Retireの順序と責務を確認する。
- canonical handoffとprepareの出力だけを消費するproduction境界のE2Eを追加する。テスト側でCandidateAcceptanceInput等を手組みして「受理/promote済みfixture」から始めるテストを全Workflowの証拠へ代用しない。fresh-process bootstrapだけをfresh Codexセッションの完遂検証へ代用しない。
- 実際の新規セッションに短いTASKブロックだけを入力する受入検証を行い、追加操作が必要なら未達とする。NEXTはmetadata昇格だけで実行を開始しない。
- Guard/独立Review/1 Task 1 Commit/immutable evidenceを維持し、既存Controller・Runtime・journal/Publication ownerを接続する。第二state、Task/Runtime ID専用処理、任意command runnerを追加しない。
- この親チャットでの最終監査では、新規セッションの原入力、実command列、source binding、実差分、独立Review、最終Validation・install・Publication/Retireを照合する。公開成功だけを目的達成としない。
````

## Amendments

### 2026-10-24 原要求の会話原文を補完


## Resolved references

- 「次セッションの作業」は `dogfood-bundle-three-task-audit.md` の三Bundle監査。「このバグ」は親accept後、canonical handoffが汎用controller actionだけを提示し、prepareが `controller_command_json` 空slotだけを返すため、短いTASK指示だけでは型付き正常完了操作へ進めない接続不備を指す。
- 前Taskの実commit公開・Retire成功と、短いTASK指示だけによる通常Workflow完遂という目的達成を区別する。後者を達成したという連絡元親Codexの評価は撤回された。
- `glm-worker/internal/app/controller_publication.go:11` の command型・dispatch、`glm-worker/internal/controller/publication_types.go` のcandidate型、`glm-worker/internal/controller/publication_acceptance.go` のacceptance Ownerを既存契約とする。
- `glm-worker/internal/parentactiongrammar/grammar.go:33` はcontroller actionを一律generic stagedへ投影し、`glm-worker/internal/parentaction/prepared_projection.go:27` は空slotのみを提示する。
- `glm-worker/internal/app/retired_workflow_test.go:196` のfresh process bootstrap、同file:214の受理/promoteを直接行うfixture、`glm-worker/internal/app/runtime_disposition_test.go:78` のPublication直接呼出は個別機能の検証であり、親handoffからの全Workflow完遂の証拠ではない。
- 現監査は同一Runtime Task `f3d222de-9ce7-4a59-a826-bf429b9e3d47`・Attempt `430b46e0-9db3-442a-9a52-a236945b1c55` の採用済み成果を維持して閉じる。追加運用指示により既存production型を参照してslotを設定する今回の完了を、本バグ解消の証明に数えない。
- 最終Validation・install/smokeの義務は候補宣言に従い、必要なsmokeはBundle観測固定のpromote前に実施する。候補checkoutは既存 `controller.PrepareFinalValidationWorkspace` Ownerを使用する。
- 修正は監査完了後の別セッションで行い、その後に連絡元親Codexが監査する。本登録は実行開始ではない。
- 現在の既存Ownerには、未sealの親accept後に `reopen-unsealed` で元completionをimmutableに保全し、同一Task/Attemptの正規fix/reviewで実ReviewerのNEEDS_SOL_REVIEWを取得した後、`repair-reentry-review` の `ImportParentSource` で親Metadataを取り込む経路がある。この追加運用指示を要する経路を、短いTASKだけの正常完遂と同一視しない。
- 監査終了の `controller-publication` / `preview` は候補受理前に、旧Project snapshotのNEXT先頭 `communication-transport-unification.md` のExternal feasibility節欠落を理由としてterminal Plan preflightで拒否された。親working treeの新Task/NEXT追加は既存の `normalizeCandidateTree` によりProject metadata baseへ戻されるため、このpreviewへまだ入らない。
- `controller/publication_acceptance.go` の `finalizeCandidateTrees` が後継TaskのPlan preflightを行う。`controller/task_authority_sync.go:329` はACTIVE task以外のMetadata同期を拒否する。`controller/publication_parent_source.go:37` の親source rebindは既に受理済みのsealed candidateを要求し、`app/controller_reentry_review_repair.go:21` のlive importはRuntimeのretained reentry reviewを要求する。候補受理前の通常root完了へ親Metadata登録を接続する境界も調査対象に含める。

## Purpose

親の意味判断を維持しつつ、短いTASK指示から正常完了までの機械が決められる操作・型・証拠bindingを既存Ownerが供給する。

## External feasibility

status: not-applicable

## Contract

- 親acceptからcandidate acceptance、必要な最終Validation・install/smoke、Bundle観測固定、Publication、Retire、終了metadata同期へ型付きで接続する。no-code Taskとruntime変更Taskの正常終了・失敗・再入を扱う。
- canonical handoff/prepareから固定値・必須入力・evidence/source binding・合法次操作を取得でき、親にはsemantic採否だけを残す。
- productionの親操作境界を通るE2Eと、実際の新規Codexセッションへの短いTASK入力による受入検証を行う。
- 連絡元親Codexの最終監査へ、原入力、実command列、source binding、実差分、独立Review、最終Validation・install・Publication/Retireの一次証拠を提供する。

## Must not

- operation JSONの手組み、毎回の実装探索、追加復旧指示、一時operator、State surgeryを通常運用の前提にしない。
- 受理/promote済みfixtureやfresh-process bootstrapだけで全Workflow・fresh Codexセッションの完遂と判定しない。
- Guard、独立Review、1 Task 1 Commit、immutable evidenceを弱めない。第二state、Task/Runtime ID専用処理、任意command runnerを追加しない。
- 公開成功だけで目的達成としない。NEXTのmetadata昇格を実行開始にしない。

## Acceptance criteria

1. 新規セッションが短いTASKブロックだけで、追加の運用指示なしに現在Taskの正常Lifecycleを完遂し、NEXTを開始せず停止する。追加操作が必要なら未達とする。
2. no-code/runtime変更の双方で、handoffとprepare出力だけを消費するproduction境界E2Eが正常終了・失敗/再入と最終source bindingを検証する。
3. 必要な最終Validation・install/smokeがBundle観測固定前に完了し、Publication・Retire・終了metadataが同一最終候補へ整合する。
4. 独立Reviewと連絡元親Codexの一次証拠監査により、原入力から正常終端までを照合できる。

## Historical invariants

- Controller、Runtime、journal、Publicationを既存Ownerへ収束し、独立Review・immutable evidence・1 Task 1 Commitを維持する。
- 親Codexのsemantic採否とGLMの調査・実装責務を維持する。

## Dependencies

none
