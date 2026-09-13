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

ACTIVE task fileの`## External feasibility`宣言について、status/field shape、全dispatch入口のfail-closed admission、PoC/observationのread-only・snapshot同一性、`NEEDS_SOL_DECISION` routingは`control:external-feasibility-admission`が機械強制する。親Codexはそのargv・field・順序を別の手続きとして再実装せず、machine resultに従う。

親Codexは宣言内容の意味を判断する。外部成立性が本当に不要なら`not-applicable`、未検証ならPoC/observation、実producer evidenceと親Go判断が揃った後だけimplementationとして扱う。人工fixture・scripted packet・worker/reviewer/Solの合意を実producer成立性の証拠へ昇格させない。

PoC/observation結果のGo/No-Go・観測継続は親Codexが判断する。Goは実producer evidenceと親Go判断をtask authorityへ記録して同じtaskをimplementationとして再開し、No-Goはmachine handoffが許すcanonical terminal pathを使う。GLMだけでimplementationへ昇格させない。

`not-applicable`の真偽やcritical assumptionの見落としは機械検証できない。これは親Codexの残余責任であり、Sol review・escaped review検知を第二防御とする。
