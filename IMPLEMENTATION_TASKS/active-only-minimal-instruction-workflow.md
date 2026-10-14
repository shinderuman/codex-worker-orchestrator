# Task: 短いTASK指示のみでACTIVE完了・NEXT未開始を成立させる

## Original instruction

ユーザー原文:

````text
TASK

* 現在のACTIVE taskだけを完了させる。
* ACTIVE task完了後はNEXTを開始せず停止する。
````

その指示だけで作業が進められるか整備する、というタスクを作ってバグ修正系の後ぐらいに入れろ

さっきのテストの見直しよりは前でいいと思う

ついでにいまのタスクリストの順序も見直せ

見直しした結果はPRにはすぐ反映せず俺に教えろ
````

## Amendments

ユーザーの追加説明（原文、時系列順）:

````text
違うこの場で検討しろなんて言ってない
````

````text
検討するタスクをPRで作れと言ってる
````

````text
いま現在その指示だけで作業が行えないことなんてわかってる
````

````text
だからその指示だけで作業進められるか整備するタスクを作れと最初から言っている
````

## Resolved references

- 「その指示」: 本TaskのOriginal instruction冒頭の3行のTASKブロック。別セッションでこの3行以外の実装手順、Issue番号、Task名、bootstrapコマンド、Resume手順をユーザーが補わずに実行できることが目標。
- 「現在のACTIVE」: 発話時の特定Taskではなく、その指示を受け取った時点でcanonical Planが指すACTIVE Taskを意味する。
- 「バグ修正系の後」「テストの見直しより前」: 登録時のPlanではWorktree修正・CLI task-start admissionの後、自己確認型テスト再設計3件より前の位置。後続作業の依存関係とpriorityは実行時current Gitを正とする。
- 「タスクリストの順序も見直せ」: 現行PlanのACTIVE/NEXT/BLOCKEDと各TaskのDependencies・目的・障害修正/評価の優先関係を調査し、具体的な優先順位変更案と根拠をユーザーへ報告する要求。**検討結果を承認前にPlan/PRへ反映しない。**
- 現行Rulesの通常完了ではTask metadata同期・NEXTのACTIVE昇格を行う場合があるが、「NEXTを開始せず」は次Taskのbootstrap/activation/dispatch/実装を明示的に禁止する。Planの正常な完了同期との区別を実装時に検証する。

## Purpose

新しいCodexセッションで、ユーザーが上記の短いTASK指示だけを送れば、repositoryのcanonical authorityを自律的に取得し、ACTIVE Taskの必要な調査・実装・GLM独立Review・Validation・install・Publication・Retireと正規終了まで進め、次Taskには着手せず停止できる運用/機械的workflowを成立させる。セッションごとの長文bootstrap指示・ユーザーからの都度手順補完を不要にする。

併せて、現行Planの作業順序について妥当性を評価し、変更案だけをユーザーへ報告する。

## External feasibility

status: not-applicable

## Contract

- 実行時のcurrent Git、root `AGENTS.md` → `codex/AGENTS.md`、Rules、Plan、ACTIVE Task、Controller/Runtimeのcanonical bootstrap、handoff、task admission、必要なmachine projection、Codex親指示を正として入口から完了・停止まで追跡する。過去sessionの記憶やユーザーによるTask名/Issue番号指定をauthorityへ昇格させない。
- 入力は原文の3行のTASKブロックだけとし、親がACTIVE Taskを正規に一意解決する。Plan/metadata/Runtimeが不整合なら証拠を確認し正規ownerの復旧経路を選ぶ。未確定やpermission不足は必要最小限の具体的なSTOP理由を報告し、勝手な正規成功やGuard迂回をしない。
- ACTIVE TaskのOriginal instruction・Amendments・Resolved references・Contract・Must not・Acceptanceを独立reviewに十分な形で受領し、必要なGLM委譲/Review/Sol判断/品質ゲート/installation/source一致/remote Publication/Retire/metadata同期を各既存ownerの正規経路で完了できるようにする。
- 一つのTaskを完了させる継続操作と、別Taskの新規開始を区別する。現在のACTIVEの完了処理に必要なPlan同期は正規規則を保ちながら、**新たにNEXTだったTaskのbootstrap・activation・GLM dispatch・調査・実装は開始しない**。自動継続、resume、automation発火、次Task選択時にもこの停止境界を尊重する。
- 現行の対応表面に不足があれば、必要な最小のinstruction/protocol/production ownerを修正し、短い指示を読ませるための第二の指示書・新しいstate正本・別scheduler・専用daemonを増やさない。既存のrepository-scoped bootstrapとController machine actionを再利用する。
- STOPまでを含む再現可能なEnd-to-End（必要に応じてscripted/mock providerおよび実際の既存command/CI）を検証する。途中失敗からの正規resume、ACTIVE参照ずれ、未完了/blocked、完了後の再入によるNEXT誤起動、不要なユーザーへのコマンド再説明を明示的に扱う。
- NEXT優先順も現物に即して見直す。全Taskについて目的、依存関係、先行すると復旧/品質/コストに影響する前提、bug-fixと改善/evaluationの区別を確認し、現状→推奨順、移動理由、変更不要箇所、blockerを**ユーザーに先に提示**する。順序の是非を検討しただけでPlanを書き換えない。採用は別途ユーザー判断後とし、優先度とhard dependencyを混同しない。

## Must not

- 短いTASK指示で実行できるかの現状調査・結果報告だけで完了扱いにしない。目的はその指示だけで実際に完遂できる状態へ整備すること。
- 現ACTIVEとは別のTaskを先に実装しない。ACTIVE完了後も次Taskの実装・調査開始・GLM呼び出し・正規activationをしない。
- Plan/Task fileを推測生成しない。既存Guard/permission/safety/one-shotやfail-closedを弱めず、失敗を成功と偽装しない。
- 原文TASKにない長文テンプレート、Task名、Issue番号、CLI手順のユーザー追加入力を運用上必須にしない。
- NEXTの見直し提案をユーザー承認前にPlanやPRへ反映しない。レビュー時点の実装とPlan authorityを取り違えない。
- 旧schema互換、hidden state、第二Controller、modelベースの冗長な単純手順判定、新しい非必要機構を追加しない。

## Acceptance criteria

1. 新規セッションでOriginal instructionのTASKブロックだけを入力し、repoの正規Rules/Plan/ACTIVEを親が解決して正規実行を開始できる。ユーザーにbootstrap/Task名/復旧コマンドの追加説明を要求しない。
2. scripted/live安全範囲のE2Eで実際のACTIVEのReview、必要Validation、install、Publication、Retire、Plan同期まで成功し、証拠・remote HEAD一致を検証できる。単発のunit testや「手順書が存在する」だけで完了としない。
3. ACTIVE完了後は新NEXT taskに対するmodel呼出し・activation・mutationが0件のままSTOPする。正規完了のmetadata操作とNEXT実行を混同しない。
4. 途中resume、stale Controller lease、Planズレ、品質失敗/要判断の各ケースで、fail-closedとSTOP境界を保った動作・報告が確認できる。
5. 必要なコード/指示修正があれば通常Review・tests・Lint・installを通し、追加の独自復旧/進行機構を増やさない。
6. 現行NEXTをrepository-wideに見直した順序案と理由をユーザーへ報告し、承認されていない並べ替えをPlan/PRへ加えない。

## Historical invariants

- 正本: current Git、Rules、Plan、ACTIVE Task、既存Controller/Runtimeのcanonical authority。会話とModel内部状態は正本ではない。
- Task完了と次Task開始は別の権限境界。後続を自動開始しないSTOPは最優先で守る。
- 1 Task 1 Commit、独立Review、必要なinstall、同一OID/証拠によるPublication/Retire、Codex ReductionとQuality Deltaを維持する。

## Dependencies

none
