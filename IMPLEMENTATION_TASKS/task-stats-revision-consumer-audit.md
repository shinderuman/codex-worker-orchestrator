# Task: task stats revision consumer audit

## Original instruction

````text
さっきの「意味のある停止」というのが何なのか知らないが、そういうのを改善したほうがいいと思うなら随時タスクに積んでいくように
今後も作業中に改善要素を見つけたら随時タスクに積むように
022の前での再評価タスクでも改めてタスクに積むべきものがなかったか精査するように
````

## Amendments

none

## Resolved references

- timeline retention fallbackのinstalled smokeで、実在するv3 stats archiveの多くが`schema_revision`を持たず、既存`AllTaskStats`経路では黙って除外されることを確認した
- GLM reviewerは`a431151`由来のrevision 1要求により旧archive 92/103件がskipされ、timeline以外の`AllTaskStats` consumerにも残存すると報告した
- timelineはtask identityに限定したbounded readerでabsent/0/1を受理し、future revisionをrejectする個別修正を行った

## Purpose

`AllTaskStats`の旧v3 revision archive除外が各consumerにとって意図したcurrent-only policyか、telemetry/Eval coverageを欠損させる不具合かをconsumer単位で判断し、誤った互換layer追加を避ける。

## External feasibility

status: not-applicable

## Contract

- `AllTaskStats`と関連stats archive readerの全consumerを機械的に列挙し、各consumerがcurrent revisionだけを必要とするか、retained historical cohortを必要とするかを一次根拠付きで分類する
- 92/103件という観測をcurrent artifactで再確認し、revision/version/status別のcoverageとsilent skipの可観測性をbounded reportにする
- timeline、history cohort query、stats/call-outliers、project lifecycle等の既実装surfaceと責務を重複させない
- 親Codexがconsumer単位でGo/No-Goを判断し、Goの修正責務だけを独立taskとして新たにtracked化する
- 本audit task自体はproduction behaviorを変更しない

## Must not

- old machine dataを一律に読み続ける汎用compatibility layerを追加しない
- file存在やversion 3だけから全consumerで互換性が必要と推定しない
- silent skipを0件・complete coverageとして扱わない
- GLMだけで最終Go/No-Goを確定しない

## Acceptance criteria

- 全consumer、要求するtime horizon、受理revision、欠損時impact、既存代替surfaceを表にして報告する
- current artifactに対するtotal/accepted/skipped/malformedとrevision cohortをmachine-readableに確認する
- Codex/Sol実消費削減とQuality Deltaへの影響をconsumer単位で評価する
- 親Codexが各candidateをGo/No-Goとし、Goがあれば022より前へ独立task化する
- No-Goの場合は理由と再評価条件を明示してproduction変更なしで完了する

## Historical invariants

- machine-only old schemaは必要性を証明せず恒久互換しない
- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta

## Dependencies

none

## Review findings

none

## Current boundary

telemetry history compact summary完了後、105より前にauditする。実装前にconsumer単位のGo/No-Goを確定する。
