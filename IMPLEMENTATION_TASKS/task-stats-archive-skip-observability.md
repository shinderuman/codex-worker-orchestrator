# Task: task stats archive skip observability

## Original instruction

````text
さっきの「意味のある停止」というのが何なのか知らないが、そういうのを改善したほうがいいと思うなら随時タスクに積んでいくように
今後も作業中に改善要素を見つけたら随時タスクに積むように
022の前での再評価タスクでも改めてタスクに積むべきものがなかったか精査するように
````

## Amendments

none

## Resolved references

- `task-stats-revision-consumer-audit`で、`AllTaskStats`がunsupported revision archiveを無通知で除外し、通常statsとcompact statsのaggregate surfaceがfiles-considered / skipped countを持たないことを確認した
- 親Codexはcandidate BだけをGoとし、旧archive decode・reader acceptance・汎用compatibility layerはNo-Goとした
- audit時点の観測は、current repository live stateが5/5 accepted、quarantineが133件中41 accepted / 92 skipped、他repository live stateが8件中1 accepted / 7 skippedだった

## Purpose

task stats archiveのunsupported skipをaggregate利用者へboundedに可観測化し、欠損したcohortをcomplete coverageと誤認しないようにする。

## External feasibility

status: not-applicable

## Contract

- `AllTaskStats`のarchive走査結果に、少なくともfiles consideredとunsupported schema/revisionによるskip件数をboundedなmachine-readable値として追加する
- 通常`--stats`と`--stats --compact`のaggregate outputへ同じ意味のcoverage情報を露出する
- telemetry historyのcohort reportingと意味・命名・集計境界を比較し、同一概念を別定義で重複させない
- accepted archiveから計算する既存aggregate値は変更しない

## Must not

- schema_revision欠落archiveをdecodeまたは受理しない
- timelineを含む個別readerのacceptanceを変更しない
- old machine data向けcompatibility layer、migration、fallbackを追加しない
- file名一覧や個別archive内容を無制限にoutputへ展開しない

## Acceptance criteria

- mixed-revision archive fixtureでfiles-considered / accepted / skipped-unsupportedの関係をstate package testに固定する
- 通常statsとcompact statsのoutput testでcoverage値と既存aggregate不変を確認する
- unsupported archiveが0件の場合と複数件の場合を検証する
- current schemaの受理条件とunsupported archiveの拒否条件が変更されていないことを確認する

## Historical invariants

- machine-only old schemaは必要性を証明せず恒久互換しない
- silent skipを0件またはcomplete coverageとして扱わない

## Dependencies

none
