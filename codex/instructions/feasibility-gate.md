# 外部成立性のfeasibility gate

外部service・取得方式・実行環境等の外部成立性が本番設計の前提になる変更をGLMへ委譲する場合と、その完了報告を受け取る場合に適用する。親Codexのorchestration contractであり、worker/reviewerへの個別checklist追加で代替しない。

## 適用条件

- 未検証のcritical assumption(外部serviceの継続提供、取得方式の継続成立、実行環境からの到達・認証成立、外部producerが必要なfieldを必要な時点で公開する可用性とそのevent timing等)がproduction code・IaC・運用展開の設計前提になり、誤った前提のまま進んだ後続コストが大きい場合だけgateを適用する。
- 通常の局所変更、確立済み前提の範囲内変更、短時間の意味的検証でcritical assumptionを解消できる対象へ、形式的なPoCや固定の観測期間を要求しない。

## gateで固定する内容

production実装へ進む前に、次を対象の不確実性・変動性・継続成立性の重要度に応じて明示する。全対象へ同じ深度を機械的に要求しない。

- 未検証のcritical assumptionの列挙
- assumptionごとの最小PoCと代表case
- 意味的成功条件: 必要データの意味的検証と代表caseのterminal outcomeまでを含める。HTTP 200・process exit 0・単発取得等のtransport成功だけを成立性の証明にしない
- 必要な試行回数・観測期間: 対象固有の不確実性と変動性から決める。Amazon取得PoCの48〜72時間はその対象固有の観測条件であり一般contractへ固定しない。外部API schema確認・実行環境からの到達確認・認証方式の成立確認など短時間の意味的検証で足りる対象へ長時間試験を要求しない
- Go/No-Go基準と撤退条件

## orchestration

- 成立性検証のPoC・観測taskとproduction実装taskを分離する。未検証の外部成立性を前提にしたproduction code・IaC・運用展開の実装をGLMへ委譲しない。
- 外部producerが必要なfieldを必要な時点で公開すること自体が効果成立の前提なら、実producerでの最小PoCをproduction実装より先に要求する。人工fixture・scripted packet・worker/reviewer/Solの合意は、producerのfield・schema・timing成立の証拠として受理しない。
- transport成功だけの完了報告を成立性の証明として受領しない。意味的成功条件・代表caseのterminal outcome・観測結果が揃わない完了報告は差し戻す。
- Go/No-Goと撤退判断はSol High・ユーザーへ戻し、GLMだけで確定させない。
- 観測中に前提が崩れた場合は、workaroundの追加実装をさせず観測事実をSol/ユーザー判断へ戻す。
- 単発の具体的成功を継続運用可能性へ一般化しない。同時に個別PoCの長時間観測条件を全feasibility gateへ一般化しない。

## dispatch gate宣言(task file機械検証)

外部成立性をproduction correctnessの前提に含むtaskを`glm-worker`へ委譲するときは、ACTIVE task file本文へ`## External feasibility`節として宣言する。glm-workerは新規・decision・fix・reviewer・auto-fix・resumeの全dispatch入口で同一parserにより宣言を検証し、受理できないtaskはworker/reviewer model呼出0回(`external_feasibility_missing`・`external_feasibility_malformed`・`external_feasibility_unverified`)でfail closedする。検証は追加AI call・追加classifierなしの機械処理で、宣言はtask file本体の固定context増分(not-applicable宣言で約50 bytes、implementation宣言でも約300 bytes)のみを消費する。

- `status: not-applicable`: 外部成立性が前提にならない通常task。status行だけを置き、他fieldを書かない。
- `status: poc` / `status: observation`: 未検証段階。`assumption`(検証対象の外部前提)を必須とし、evidence系fieldは書かない。glm-workerはworkerをread-only capabilityで実行し、開始前repo snapshotとの同一性を強制する。workerがdiffを出した時点でfail closedする。結果はreviewerを経ず親CodexのGo/No-Go(`NEEDS_SOL_DECISION`)へ返り、GLMだけでimplementationへ昇格させる経路は存在しない。
- `status: implementation`: 実装許可。`assumption`・`evidence-source: producer`・`evidence`(実producer観測の事実)・`go`(親CodexのGo判断と日付)の4fieldが全て必須。`evidence-source`は実producer観測のみを意味し、人工fixture・scripted packet・worker/reviewerのPASS自己申告はevidenceとして受理しない。

親Codexの責務:

- 既存taskをACTIVATEする前に宣言を確認し、欠けていれば該当taskへ宣言を追加してから委譲する。宣言のないtask fileはdispatch時にfail closedする。
- 新規task生成ではこの節を最初から含める。
- PoC/observation結果のGo/No-Goは親Codexが行う。Goならtask fileの宣言を`status: implementation`へ書き換えて実producer evidenceと親Go判断を記録してから通常のdecision経路で同じtaskを再開する。No-Goなら`glm-worker --handoff`の`allowed_actions`に`no-go`があることを確認し、`glm-parent-action no-go`でmodel call 0のterminal撤退にする。同じNo-Goをdecisionとしてworkerへ再送しない。観測継続なら通常のdecision経路でread-only observationを継続する。
- `not-applicable`宣言が誤っている場合(実際には未検証外部前提を含むtask)を機械検証で絶対防止はできない。この残余riskは宣言した親Codexの判断責任であり、Sol review・escaped review検知が第二防御である。
