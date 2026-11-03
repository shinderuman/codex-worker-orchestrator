# Task: observation準備処理への取消伝播

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
observationの取消/deadlineをpreflightとmodule copyへ伝播し、取消済み要求で新subprocessを起動しない。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

### 2026-11-03 Codexの先行採否

採否原文: https://github.com/shinderuman/codex-worker-orchestrator/pull/1481#issuecomment-6588887083

````text
Codexの確認結果です。対象はhead `03b2e394c15e28574782445a003b5ff317a50957`、Darwin検証のcode SHAは `10210d9e8da7ad99780253b9c582dd5ecbcb09bf` です。

**単一context/deadlineを準備からtargetへ伝播し、既存のatomic copy・process-group ownerを使う方針は採用します。現差分はCodexへの引継ぎ候補として扱いますが、ここでは完成認定・Merge承認を行いません。** 以下を修正または未達事項として引き継いでください。

### 採用する部分

- 取消済み要求をconfinement admission/preflightの前で止め、preflight・module copy・module-cache lookup・targetへ同じexecution contextを渡す構成。
- DarwinのcontextなしCombinedOutputを、既存process-groupの起動・停止・waitを用いる処理へ置き換えること。
- `execfile.WriteStream` を維持し、copy中の取消確認と公開前後の取消確認を加えること。source I/OをForkLockの内側へ戻したり、非atomicな直接書込みへ置き換えたりしないこと。
- sandboxが利用できない場合の拒否、host cwdへのfallback禁止、target前の失敗と実target結果の区別。旧signatureの互換wrapperや旧取消動作へのfallbackは不要です。

### 1. 取消テストを実際の取消へ結び付ける

`cancelAfterReadsContext.Err()` が「Errの呼出回数が3回になったらCanceledを返す」fixtureは採用しません。コピー開始から取消までを内部のcontext検査回数へ結び付けており、検査箇所の追加・移動だけで意味が変わります。

実際の `context.WithCancel` とcontrolled reader/同期点等を使い、I/Oが開始したことを確認してからcancelする形へ寄せてください。固定sleepでcopy途中と推定したり、fixtureを大きくして競争に勝つことを成功条件にしたりする必要はありません。

取消時には最終destinationがないことに加え、`.module-copy-*` の部分一時fileが残らないことも確認してください。Reader・atomic write・publicationそれぞれの既存ownerを直接検証し、test専用のcopy状態機械を作らないこと。

### 2. 回収待ちの結果を捨てない

新しい `runObservationPreflightProcess` の取消分岐は `_ = boundedGoTestWait(waitDone)` としており、回収待ちの期限超過を含む結果を失います。context取消と「processを回収できたか」は別の事実です。

既存の停止ownerを使ったまま、待機結果と取消原因を保持し、回収を確認できなかった場合に「回収済み」とは報告しないでください。必要ならerrors.Join等で元のcontext原因も保ち、正常に停止したprocessのexpectedな終了状態と、待機の未完了/失敗を区別します。同じTaskで扱うmodule-cache lookupの取消分岐も整合を確認してください。新しいprocess管理基盤は不要です。

子processのテストも、一定時間後にsurvived markerがないことだけでは「process不在・回収済み」の十分な証拠にはなりません。既存fixtureを使い、実PID/process groupの終了と自分が所有するchildのwaitを確認する形へ補強してください。markerは起動や副作用の確認として残せます。

### 3. Darwin CI

supported platformの回帰を実行するDarwin jobの追加方針は採用します。Go versionをquality-tools.ymlから取得し、productionのconfinement拒否条件を緩めない構成を維持してください。

path filterはobservationexec自身だけでなく、直接依存する `internal/execfile`、module/toolchain契約の変更時にも必要な回帰が実行されるよう整理してください。大量の無関係なCIや別のversion正本を追加する必要はありません。

[Darwin run #41524905085](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41524905085)のjob successと、[最終head run #41526069500](https://github.com/shinderuman/codex-worker-orchestrator/actions/runs/41526069500)の基本5系統・集約lint・install-smoke 4系統、計10ジョブsuccessを確認しました。またcode SHAから最終headへの差分はTask/Plan/dependencyの3 Metadata fileだけで、GoコードとDarwin workflowに差がないことも確認済みです。これらは該当sourceの検証証拠として採用します。

ただしpackage/jobのsuccessと個別の実行・skip範囲は区別し、controlledなsandbox-exec/go代替fixtureを実production環境全体の成立性の証明とはしません。今後コードを修正した場合、既存結果を変更後SHAのPASSへ読み替えないでください。

### 引継ぎとMetadata

Web GPTだけで全Acceptanceを完成させる必要はありません。対応できた修正、未対応の上記事項、検証したexact SHAと結果を分けて残してください。Codexが引き取った後に追加修正・検証・最終採否を行います。

PR内のTask削除、Plan参照削除、cleanup TaskのFulfilled移動は、**完了同期の候補差分**として確認します。Web GPTの「完成」記載だけで親の採否やdependency成功を確定した扱いにはしません。Codexが統合時に原要求と実装・証拠を照合してMetadataを同期します。完了記録のための独立コミットやHistory追記は不要です。

**7件の巻き取りは、ユーザーがこのチャットで明示的に開始指示した後、一斉に開始します。それまでは本PRへの確認・返答だけです。** この返答でLocal Squash、追加実装、Merge/Close、本配置を開始しません。checkpoint先頭の順序も維持します。
````

## Resolved references

- `Review.md` の「2026-10 再監査」の A6。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

observationの取消/deadlineをpreflightとmodule copyへ伝播し、取消済み要求で新subprocessを起動しない。

## External feasibility

status: not-applicable

## Contract

- RunIsolatedGoTest入口、confinement preflight、copy loop、target実行を同じcontext/deadlineで制御する。
- subprocessとその子processの停止・回収、copy途中の一時artifact cleanupをboundedにする。file/byte上限をcontextの代替にしない。
- supported platformごとの実装を確認し、DarwinのcontextなしCombinedOutputだけ直して他の準備段階を残さない。

## Must not

- sandboxの緩和、host cwdでの代替実行、取消無視の再試行を追加しない。
- 実際のtarget実行前の失敗をtarget test結果へ昇格しない。旧取消動作との互換モードを作らない。

## Acceptance criteria

- 取消済みcontextではcontrolled preflight helperのmarkerも作られない。
- preflight中、copy中、target実行中の取消/deadlineでboundedに戻り、対象processが残らないことをcontrolled fixtureで検証する。
- 正常実行とconfinement失敗の意味・evidenceを保持し、related testsとquality gateを通す。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
