# 実装task lifecycle規則

このfileはtask lifecycleと親Codex/GLMの責務境界を定める。個別Taskの要求、進捗、実行時状態を複製しない。

## source-of-truth順位

Git / working treeの実状態、`IMPLEMENTATION_RULES.md`、`IMPLEMENTATION_PLAN.local.md`、PlanにACTIVEがある場合はそのTask file、Taskが明示参照する`IMPLEMENTATION_HISTORY.md`の例外decisionの順に確認する。conversation、compaction summary、internal TODOはcacheであり要求正本ではない。実装と要求が矛盾しても、原要求を実装に合わせて変更しない。ユーザーによる要求変更は`Amendments`に保存する。

## 要求受領とtracked化

- 意味契約を変更する新規ユーザー要求は、compaction・GLM呼出・長時間調査・実装開始より前に、親CodexがRules / Plan / Taskの適切な正本へ固定する。run-controlだけの指示は`codex/instructions/task-request-boundary.md`に従い、ユーザーがdurable化を明示した場合を除いてTaskへ転記しない。混在するmessageは意味契約の追加分だけをlosslessに分離する。
- 現ACTIVEの同一責務への追加要求は`Amendments`へ原文を時系列追記し、別責務なら独立Taskを作る。新Task作成だけでACTIVEを変更せず、優先実行が必要な場合だけPlanを更新する。
- `Original instruction`はimmutableな原要求、`Amendments`はappend-onlyの追加要求、`Resolved references`は会話依存参照の解決記録とする。要約・置換・旧要求の削除は禁止し、新旧要求が矛盾するときは最新Amendmentの優先をderived `Contract`へ明示する。
- `Purpose` / `Contract` / `Must not` / `Acceptance criteria`は原要求から導いた実装・review用indexであり、原要求の複製や代用にしない。複数Taskへ分割する場合も必要な原要求と禁止事項をlosslessに引き継ぐ。
- RULES変更が既存Taskと矛盾するときは、そのTaskのmetadataも整合させてから確定する。

## Markdown authority

Planはscheduleとpriority、Taskは個別要求、Historyは将来のtracked Taskが明示参照する非diffのcross-task decisionだけを保持する。通常完了・進捗・HEAD・branch・status・diagnostic・runtime evidenceをMarkdownへ複製しない。取得元はGit / CI / bundle / telemetry / runtime stateとする。History recordが参照されなくなったら削除する。READMEや生成Markdownを第二正本にせず、source由来の生成値が必要ならgeneratorと一致検証を唯一の更新経路とする。machine gateで自然言語の意味的一致まで保証したことにしない。

## parent maintenanceと通常task

- production behavior・コード・prompt・testを変更せず、現在のACTIVEの意味契約を変えず、独立review・rollback単位を必要とせず、Rules / Plan / Task metadata / 必要なHistory decisionだけで完結する変更は親Codexがparent maintenanceとして直接扱える。ACTIVEを一時退避・再昇格しない。要求は対象の正本へ保存し、必要な最小検証を行う。
- production code、CLI/API/protocol、state、prompt/dispatch、integration testを実質変更する作業、独立したacceptance・rollbackや長時間調査を要する作業はsemantic filenameの通常Taskへ分離する。Metadataに関係するguard / production wiringの修正も通常Taskとする。
- 1 Taskは原則1つの独立review可能な責務・1コミット候補とする。同じinvariantの実装とintegration testは分離しない。GLMへumbrellaではなく具体Task 1件をdispatchする。

## 親MetadataのGitコミット境界

通常Taskは1 Task 1 Commitとする。実装完了コミットにTask退役・Plan同期等のmetadataをAmendしてからPublicationし、実装と退役を別々の公開コミットにしない。metadataの追記ごとにコミットを増やさない。Controllerのadmission / snapshot / Publication / Retire、最終OIDと証拠の整合を保ち、Guardを迂回・成功偽装・公開済み履歴の改変をしない。

## task file必須構造

Task fileには`Original instruction`、`Amendments`、必要時の`Resolved references`、`Purpose`、`Contract`、`Must not`、`Acceptance criteria`、`Historical invariants`、`Dependencies`を設ける。未解決Findingがあるときだけ`Review findings`、成功済みdependencyの明示保持が必要なときだけ`Fulfilled dependencies`を設ける。Task fileに`Status`・`Current boundary`を置かず、schedule stateはPlanを正とする。

Darwin専用テストが必要なTaskは`## Darwin acceptance`へ `- \`./internal/qualitygate:TestDarwinSysctlDecoding\`` 形式で対象packageとテスト名を宣言する。request-backedの場合は正規request本文に置き、Controllerが正式な`go-test`へ引き継ぐ。

## task filenameとdependency

- 新規Task filenameはsemantic slugとし、priority・順序・statusを表す番号を付けない。既存の番号付きTaskはrenameしない。過剰な命名validatorは追加しない。
- PlanのACTIVE / NEXTはunordered `-` の出現順をpriorityとする。Task fileの`Dependencies`にはcorrectness上の真の先行要件だけをpathで記載し、予定順や番号からhard dependencyを推定しない。
- `Dependencies`はoutstanding、`Fulfilled dependencies`は親Codexが成功確認後に明示移動したものだけとする。Taskが消えたことやGit履歴だけからfulfilledを推定しない。欠損・未知参照、cycle、self dependency、明示状態の矛盾はfail closedする。

## parent orchestrationの改善

親Codexは反復する手動操作・高コストなmodel / test / review / retry等を観測したら、品質と保守費用を比較し、機械化に価値があるものだけを独立Task化してPlanで優先順位を決める。worker/reviewerは対象Taskを勝手に拡張せず観測を報告する。安全停止・必要なSol gate・単発の外部limitだけを根拠に改善Taskを増やさない。待機中に無関係な並列作業や反復pollで消費を増やさない。

## Goal起点のproject orchestration

Goal modeの手順は`codex/instructions/goal-development.md`を正とする。PlanにGOALがない場合は通常Task lifecycleを維持する。Goalの分割・replanning・採否は親Codexのsemantic authorityであり、`glm-worker --project-state`はread-onlyのmechanical projectionとする。Goal進行中は一意のACTIVEを要し、未完了Goalをtasklessへ縮退しない。最終Taskのmechanical readiness、未解決依存・Findingなし、必要なvalidation・install、親Codexのsemantic acceptanceを満たしたときだけcompleted GOALと空scheduleを確定する。専用daemon・state DB・第二正本は追加しない。

## 再読contract

新session、compaction、停止・再開、追加ユーザー要求、レビュー差戻し、最終受入確認では、コードへ触る前にRules / Plan全文、ACTIVEがあればそのTask全文と明示参照したHistory見出しを読む。NEXT Taskは開始時まで全文を読む必要はない。実際のauthority bootstrap経路は`codex/AGENTS.md`を正とする。

## worker / reviewer contract

workerと独立reviewerは同じACTIVE Taskをそれぞれ原文から読み、reviewerは実装とderived Contractの一致だけでなくContractとOriginal instruction / Amendmentsの欠落も評価する。production prompt/dispatchとの因果をtestで検証する。Review結果は欠陥・影響・原因層・再発防止を混同しない。

## blocked taskのactivation

BLOCKED / USER_PERMISSION_WAITは許可だけではACTIVE化しない。許可原文をAmendmentsへ追記し、前提証拠を確認し、具体的なContract / Must not / Acceptance criteriaを確定してから候補とする。

## 親Codex専有metadata

`IMPLEMENTATION_RULES.md`、`IMPLEMENTATION_PLAN.local.md`、`IMPLEMENTATION_TASKS/*.md`、`IMPLEMENTATION_HISTORY.md`は親Codexだけが編集する。GLM worker/reviewerは変更せず提案だけを返す。model-call中の整合性は`control:parent-metadata-integrity`を正とし、Guardを迂回しない。

## task完了

- 成功確認後、残存Taskのhard prerequisiteは`Dependencies`から`Fulfilled dependencies`へ同じmetadata同期で移す。No-Go・cancel・withdrawalによる削除ではfulfilledにしない。
- 完了Task fileを削除しPlanを同期する。非GoalでNEXTがなければACTIVEも空にし、BLOCKEDを保持する。存在しない後続Taskを作らず、実行開始時は一意のACTIVEを要求する。Goal modeの空scheduleはGoalのcompletion条件に従う。
- 最終HEADとremote同期は`control:remote-completion-sync`を正とする。通常の完了証跡はHistoryへ追記しない。コミットされた要求版はGit履歴から回収し、未コミットのTask登録・Amendments版が削除と同じcommitに含まれる場合はControllerのparent-source revision / ProjectSnapshot archiveを正とする。diffはGit、validation・runtime evidenceはCI / bundle / telemetryを正とする。

## install / validation

GLMにcommit/pushさせない。runtime変更はtest/review後、後続の実運用へ進む前の適切な区切りで`install.sh`による本配置、installed/source一致と必要なproduction smokeを確認する。source-only metadata変更はinstall不要。

## repository automation authority

ユーザーは本repositoryの許可済み作業について、親Codexによるautomationの作成・更新・削除・停止後再予約を恒久的に許可している（2026-08-31）。同じ範囲・再開予約ごとに許可を取り直さない。ただし他repository・credential・購入・無関係な操作へ拡張せず、task固有の停止境界・feasibility / quality gate・外部の安全審査を解除しない。再開時はtaskとcheckpoint・repositoryを再検証し、不要な重複予約を残さない。

原指示（権限の範囲を示すlossless source）:

```text
いや今後永続的に許可するよ
と言うかこの契約を無条件に作成させることできないの？
このリポジトリの目的のひとつに24時間連続して開発するツールであるということがあるんだがこのままだとその意味が消滅する
```

## machine-only data原則

glm-worker/Codex/GLM専用のmachine dataに後方互換を恒久追加しない。旧schemaは用途に応じreject/skip/reset/rebuild/delete/resume不能とし、current schemaを厳格に検証する。active task保護と恒久互換を混同しない。

## repository structure invariants

Go commandの`cmd/<name>/main.go`は薄いentrypointに限定し、実装ownerは`internal/`とする（`harnesslint`のentrypoint-thin rule）。`glm-worker`の成功stdoutは単一JSON object、失敗時stdoutは空・stderrにstructured error JSONとし、既存のsingle-object validatorを正とする。

## 禁止

Task専用state DB / watcher / daemon、Task fileへの進捗日記、PlanとTaskの詳細仕様二重化、無許可の実Sol High A/Bや追加benchmark AI call、GLM worker/reviewerのGit remote writeを禁止する。
