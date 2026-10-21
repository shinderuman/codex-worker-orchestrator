# Workflow費用とapp責務境界の評価

基準source: `f21fcd49172ae6d613cf0f0f5290c2ec14656af2`。開始時点のCI変更（internal/app除外等）を維持した。既存archive評価Taskの全文はACTIVE Taskへ統合済みであり、その削除を完了証拠にはしていない。

## 実装と採否

- 外部code/CIのclean fast-forwardは、完了済みquiescent controllerから明示的adopt-sourceで同期する。read-only authorityの読取と同期mutationを分離した。dirty/divergence/remote不一致/未完了Runtime/live lease/pending/candidate等は拒否する。既存journalとseal graphを共用し、品質PASSを新sourceへ転用しない。
- metadata-onlyの新ACTIVE切替も正規handoffへ接続した。metadata/source共通でapply時に旧Runtimeのquiescenceを再検査する。prepare後のsource移動やRuntime未完了化は拒否し、歴史journal検証へ現在remote/Runtime条件を遡及適用しない。
- candidate/terminal/remote observation等で同一の型付きrootsとobject formatなら既存canonical archiveを参照する。rootsだけの照合のための全履歴列挙、同一envelopeの二重decodeを除去した。canonical refの破損はfresh captureへ握り潰さず拒否する。
- pack検証をunpack-objectsからindex-packへ変更し、全履歴のloose object展開を廃止した。content digest、roots/type、fsck connectivity、missing object拒否、offline復元の契約は維持する。永続cache/index/第二store/新schemaを追加していない。
- 正規finalize-checkはPrepareFinalValidationWorkspaceでc.CommitOIDへcheckoutする。completionも同じc.CommitOIDをRunCommitで検査するため、正規候補のfinalize-check lintを削除しcompletionを単一ownerにした。completion ownerのないcaller_cwd経路のlintはfull suite前に残す。GLMの「finalize-checkにlintなし」「入力が異なるから一律KEEP」という評価は現物確認により訂正した。
- bundle/parentusage/review-gap分析と単体test/fixtureをinternal/bundleanalysisへ移した。production entryはBuildParentUsageSummaryとPrintReviewGapで、appから機能owner、さらにstate/codexrollout/reportへの依存とする。reportのapp-local CompactSummaryFunc callbackを削除した。appにはCLI dispatch/出力と接続testを残す。telemetry summary全体を無理に移さず、まとまった分析責務を完結させた。
- CLI完了fixtureの全repository複製を15個の品質契約参照file、最小module、代替quality processへ置換した。参照用workflow sourceはbuild対象から分離し、代替processはfixture限定の既存環境境界を使う。新たなproduction injection API/Stateはない。実quality tools/commit checkoutの単体・統合検証と最終source gateは別に維持する。

## 検証owner・失効条件

| 処理 | ownerと実効入力 | 回数・失効と採否 |
|---|---|---|
| worker mutable lint/fix | workflow quality gate、変更中worktree/tool契約 | worker round毎。修正後sourceに失効するため保持 |
| full suite | qualitygate、正規候補checkoutのcommit/index/clean worktree/form/module | 通常最終1回、最終FAIL→Fixでは2回。source変更・新validationで旧runは転用不可 |
| finalize-check lint | parentactioncmd | 正規候補ではcompletionと同一commitの重複を削除。単独callerだけ早期実行 |
| exact commit lint | controller completionMetadataLint、CandidateOID/tool契約 | 完了operationに1回。失敗でPublication/Retireを拒否。readiness保存後のrecoverは既存証拠を照合 |
| CI lint/suite | 既存Ubuntu CIとpush対象 | 異環境検証として保持。既存のapp除外を性能改善に数えない |
| source/latest validation | controller publication/final validation/readiness | source/tree/index、最新run、remote、lease、generationを各mutation境界で再確認 |
| candidate/review/install採否 | controller candidate evidence | current candidateのAttempt/Snapshot/Base/Tree/要求formに照合。古いPASSやinstallは転用しない |
| archive/seal | content-addressed evidence store | digest/roots/type/pack closure。既存Store内memoは保持、新invocationのdiskを未検証でPASS扱いしない |
| 歴史閉包保持 | canonical seal/ledger/cleanup graph | 単一ownerのarchive参照を共有。異rootsは独立capture。現在候補の採否と歴史の到達性保持を区別 |

通常成功の同一最終commitへのrepository lintは2回（finalize-check+completion）から1回へ減る。最終suite FAIL→Fix→再Review→再公開でも、変更後candidateの新しいsuite/独立Review/完了lintは必要であり旧snapshot PASSを使わない。基準sourceではsuite失敗時はfinalize lintへ到達しないため、この経路の削減も最後の成功候補への重複1回分である。

安価な失敗の前置きはcaller経路で採用した。正規候補では新しい証拠管理を作ってlintを前倒しする案より、重複lint自体の削除を採用した。workerの早期fix/review前gateとPublication前の必須exact-commit lintは残る。

## 限定計測

2026-10-21、darwin/arm64、Go 1.27.0、Apple Git 2.54.0。production quality toolsはquality-tools.ymlのGo 1.25.4等を使用する。以下は実装差の限定観測であり、別環境CIやmodel待機を含むTask全体の短縮率ではない。

| 3段階のarchive capture+verify | 改善前のcapture呼出 | 参照共有後 | 時間ms 前/後 | unique envelope bytes 前/後 |
|---|---:|---:|---:|---:|
| 小履歴 | 3 | 1 | 305 / 235 | 711 / 711 |
| 256KiB blobをcommit後に削除 | 3 | 1 | 319 / 247 | 350299 / 350299 |
| 異rootsを含む複数段階 | 3 | 2 | 477 / 491 | 700657 / 700657 |

同一pack bytesには既存content dedupが効くため、今回のfixtureで保存bytes削減は観測していない。異rootsは再利用せず、複数roots例の時間も改善していない。同一入力のpack再生成を減らす構造効果と、disk容量/CPU/wall-clockの実測を区別する。既存storeでは同一commitの異packが複数保存されていたが、全Taskに容量削減率を外挿しない。旧archive自体の削除は行っていない。

CLI lifecycleは旧GLM観測247.7秒、最小corpusの成功run51.7秒。Go/cache/検証条件の差が残るため削減率は未確定。追加lint拒否接続testは15.5秒、最大resident set約350MB（go test配下の限定観測、比較baselineなし）。同時実行するpackage数やtest timeoutは増やしていない。bundleanalysis単体testはappのCLI/Git fixtureから独立して実行できる。parallelism/cache効果、Task全体のpeak memory/I/Oは未測定。

候補01c8707628bb89bdc4342833c7144d3fb59e2060の最初の実full suiteは972.716秒でFAIL。appは960.485秒、controllerは811.733秒、workflowは369.657秒だった。最小fixtureにGit Guardのpre-push/reference-transactionが欠落していたため、実Guardの接続testで拒否された。必要な2fileをfixtureへ復帰し、testを維持した。このrunはFAIL証拠として保管し、修正後sourceへ転用しない。

候補82e90981abd0ac8780bb399dddc977e714a58886の2回目の実full suiteは980.731秒でFAIL。app（962.296秒）とcontroller（792.646秒）はPASSし、前回のGuard欠落は解消した。observationexecのhost HOME分離testで、go env GOMODCACHEが開始するtelemetry writerとTempDir削除が競合した。GOTELEMETRY環境変数による抑止案は、実Goが参照しないことを独立Reviewで指摘され撤回した。Productionのmodule cache解決は変更せず、既存HOME分離testだけでfake HOME配下のUserConfigDirへtelemetry mode=offを用意する。実go env GOTELEMETRYがoffであることと従来のHOME分離を同時に検証する。実Goを使う当該testは30回PASS、package全体もPASSした。旧runはFAILとして保管し、修正後sourceへ転用しない。

GLM停止時点の既存telemetryはworker16呼出/reviewer2呼出、Sol packet8986 bytes、rate limit4件。parent_usageは取得不可で、Codex token削減率は測定できていない。不要なlint/pack/fixtureとcallbackの削除は今後の機械処理・再検査負担を減らすが、Codex使用量の数値効果とはみなさない。

## 保証・検証帰属

- metadata prepared→Runtime未完了化の新regressionは修正前FAIL（15.32秒）、共通apply境界修正後PASS（15.75秒）。独立Codex Reviewで指摘・修正確認した。
- exact commit lint失敗がremoteとterminal lineageを変えないこと、archive media/roots/破損/閉包欠落/offline保持の対象testがPASS。
- 通常成功と最終FAIL→Fix→再Review→再公開はTestCanonicalValidationCadenceWithoutRuntimeRepairで確認。追加TestCanonicalCadenceCompletedRuntimeAdmitsExternalPullSyncAndSuccessorは実ModeNewTask開始・worker/reviewer各1呼出・新task identityまで60.27秒でPASS。正規CLI入口とstaged操作を使い、fixture途中のState補正を行っていない。
- cadenceは代替model/Go processによる接続検証であり、実suite・実運用のPASS証拠ではない。初回復旧の直接dispatchと、本Taskでユーザーが明示許可したGLM Limit後の引継ぎ例外も通常経路の成功証拠に使わない。
- 最終候補の実full suite、本配置、Publication、Retire、Cleanupはcanonical ownerのvalidation/operation recordを正とする。本書を未実行のPASS記録にしない。Task完了後はNEXTを開始しない。

archive計測はcurrent controller ownerを呼ぶ一時Go overlayで実行し、通常suiteへ計測testを常設していない。測定source/logは本Task artifactに保存する。追加full suiteを調査目的で反復していない。
