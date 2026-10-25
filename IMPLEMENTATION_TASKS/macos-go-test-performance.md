# Task: macOS Go Quality Gateのtoolchain整合とテスト高速化

## Original instruction

Goバージョン逸脱の是正要求（原文）:

````text
Bundleが1.27.0を使ってるってことを示してるんだとしたらそれはCodexが勝手にバージョンを変えて作業してるって言うクソ行為だから是正させなきゃいけないんじゃないの
````

Mac側の速度改善要求（原文）:

````text
かつMac側の速度改善はそういうタスクをCodexでやるタスクにするべきだと思うのでこれもまたIssueとPRを作っておいてくれ
````

## Amendments

2026-10-25 ユーザー指示（原文）:

````text
じゃあ速度改善のタスクとバージョンを揃えるタスクは1つのタスクでいいんじゃないか
NEXTの先頭でやるべきだと思うだがこのあとConflictして順序が変わるはずなのでその後改めて順序は支持する
````

### 2026-10-25 External Reviewから同一責務へ統合: quality-gate-timing-write-finalization

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
timing書込失敗をwrapper failureとして記録し元のchild失敗を保存する。
completeQualityGateRunでtiming artifact書込失敗後もrecordを確定し、child成功時はwrapper由来FAIL/nonzero、child失敗時は既存exit code/sourceを維持する。
timings pathは不存在artifactを指さず、runner reconcileのinterrupted/unknownで原因を上書きしない。
````

### 2026-10-25 External Reviewから同一責務へ統合: timing-analysis-validation-run-identity

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
同じvalidation runを自分自身のbaselineへ選ばない。
runtime/repository/predecessor等から集まる同一ValidationRunIDをtiming group生成前に正規identityへ収束させる。
同じIDで内容/条件が矛盾する場合のunknown/errorを定め、単なるfirst winsで証拠矛盾を隠さない。新しい第二indexを追加しない。
nil timingsや条件欠落の扱いと、form/module/toolchain条件の比較境界を維持する。
````

## Resolved references

- `quality-tools.yml` の `go` がrepositoryの既存のtoolchain contract。観測当時は `go: 1.25.4` で、CIの `actions/setup-go` と `goquality` はこの値を参照する。
- 2026-10-25のmacOS Bundle `1a25e51c-8935-4150-8b6f-520d2d0d3848.zip` の3つの `live/validation/quality-gate/*/run.json` は `toolchain.go_version=go1.27.0`、`goos=darwin`、`goarch=arm64`、`test_cache=executed` を報告。これは正規Quality Gateが利用したGoバージョンの証拠であり、親Codexが意図的に変更した証拠とは区別する。
- 現行 `glm-worker/internal/app/quality_gate_execution.go` の `runQualityGateProcess` は `exec.Command("go", ...)`、`glm-worker/internal/app/quality_gate_persistence.go` の `qualityGateEnv` は `GOFLAGS`を消去するが、環境の `GOTOOLCHAIN` / `PATH`を継承する。 `glm-worker/internal/qualitygate/run_record.go` の `CaptureToolchain` は観測を記録するだけで規定値を強制していない。

- 「Mac側」: M1 Mac mini、メモリ16GB、Apple Silicon。2026-10-25のユーザー提示Bundle `1a25e51c-8935-4150-8b6f-520d2d0d3848.zip` は `darwin/arm64`、Go 1.27.0、parallelism 8を記録。
- Bundleの正規通常 `go-test` 3 runは約25分（タイムアウトFAIL）・30分（PASS）・32分（PASS）。成功回の `internal/app` は約30分、`internal/controller` は約15～17分。
- Linux CIの2026-10-25比較実行では、 `internal/app` 81～104秒、`internal/controller` 93～114秒。CIはLinux x86_64かつ独立matrixのため、ソース/バージョン/同時負荷/キャッシュが一致した比較ではない。
- Goバージョン逸脱の是正とMacテスト高速化はこのTaskに統合する。BundleのCPU・メモリ・swapサンプリングは独立Task `IMPLEMENTATION_TASKS/bundle-host-resource-observability.md` に委ねる。

- PR #1381 / comment 4362202703: timings書込失敗はrecord確定前にreturnし、後続reconcileでinterrupted/unknownになる。非終端ではなく誤分類として修復し、既存nonzero child exitを保存する。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/app/quality_gate_execution.go:completeQualityGateRun`

- PR #1381 / comment 4362202715: collector群のappendとtiming groupにRunID dedupがなく、latestのcopyをbaselineにできる。重複を除いて真の旧baselineを維持する。矛盾する同ID内容を黙って選ばない。

- `glm-worker/internal/controller/evidence_export_validation.go:collectGateRun`
- `glm-worker/internal/controller/evidence_export_predecessor.go:collectPredecessorValidationRuns`
- `glm-worker/internal/controller/evidence_export_analysis.go:collectTestTimingGroups`
- `glm-worker/internal/controller/evidence_export_analysis.go:nearestComparableTimingRun`

## Purpose

正規Go Quality Gateがリポジトリの `quality-tools.yml` が選択したバージョンを確実に使う状態を成立させた上で、Macの通常Goテストが20～30分以上かかる原因を定量的に特定し、coverageと失敗検出力を維持しつつ短縮する。バージョン整合性と性能改善は単一Taskの連続する責務として扱う。

## External feasibility

status: not-applicable

## Contract

### 正規Go toolchainの整合

- 正規 `--quality-gate` / `glm-parent-action finalize-check` から実際の `go test` 起動までの単一のcanonical ownerで、Git snapshotに対応する `quality-tools.yml` の `go` 値を検証し実行環境へ適用する。既存の `goquality`、CIの `setup-go` と同じ正本に従う。
- 開始時に解決・記録した要求バージョンと実際にテストを実行するGoバージョンが一致していることを、プロセスの実環境で保証する。外部の `PATH` や `GOTOOLCHAIN` の設定により黙って別バージョンへ切り替わらない。実行中のsource/version不一致や要求toolchain取得不能はfail closedとし、誤ったPASSを生成しない。
- 既存のtoolchain/report（version, GOOS/GOARCH, GOCACHE, command, cache usage）に実行結果の正しい証拠を保持する。 `go.mod` の `go 1.22` は必要最低バージョン契約であり `quality-tools.yml` の固定実行バージョンとは区別する。
- `go test` / `go test -race` の正規run・restart/attach/resultとタスク・snapshot bindingが崩れないことを確認する。既存のrecording・validation・export経路へ結び付ける。
- MacとCIの両方で、規定どおりに実行されたことが検証可能であること。必要なテストは環境変数で別バージョンを注入しても、失敗または正規バージョンへ明示的に固定されることを示す。

### 比較可能な条件を確立した上でのMac高速化

- このTask内で上記のtoolchain整合性を成立・検証した後、同じsourceと固定Go version（`quality-tools.yml`）の比較条件を確立する。`GOVERSION`、arch/OS、GOFLAGS、GOMAXPROCS、cache、go test args、source snapshotを照合し、環境差を明示する。
- 現行Quality Gateの `test-timings.json`、Go JSONログ、CIジョブログと既存bundle/telemetryを優先し、パッケージ別・テスト別にクリティカルパスを特定する。 `app`、`controller` を特に重点確認する。
- Mac単独パッケージ実行とfull suite中のパッケージ時間の差、プロセス/テスト並列化、Gitやsubprocess起動、disk I/O、メモリ圧迫とswap、ビルドcache状態、macOS固有fixtureの影響を根拠に基づいて切り分ける。新Resource観測Taskが未完了なら測定不能部分を明示し、存在しない数値を推定しない。
- 測定のための高コストなfull suiteを無制限に繰り返さない。比較条件を固定した必要な実行を計画し、同一条件の既存PASS証拠を再利用する。低コストのisolated package/target testやinstrumentationを使える場合は優先する。
- 根本原因に基づく実装上の改善（不要な待機、同期I/Oや子プロセス実行、fixtureの反復、資源競合、適切な検証分割）を評価し、実際に速度に寄与するものだけ採用する。CI側の追加分割やGo version変更は本Taskの目的そのものにはしない。
- 改善前後のwall-clock、主要package/test時間、CPU/I/O等可能な資源証拠を同じ条件で比較し、局所最適で全体が悪化していないか確認する。速くならない場合も原因・確認限界を正確に記録し、無理に変更を作らない。

External Reviewで確認した同一責務の具体条件（quality-gate-timing-write-finalization）:

- completeQualityGateRunでtiming artifact書込失敗後もrecordを確定し、child成功時はwrapper由来FAIL/nonzero、child失敗時は既存exit code/sourceを維持する。
- timings pathは不存在artifactを指さず、runner reconcileのinterrupted/unknownで原因を上書きしない。

External Reviewで確認した同一責務の具体条件（timing-analysis-validation-run-identity）:

- runtime/repository/predecessor等から集まる同一ValidationRunIDをtiming group生成前に正規identityへ収束させる。
- 同じIDで内容/条件が矛盾する場合のunknown/errorを定め、単なるfirst winsで証拠矛盾を隠さない。新しい第二indexを追加しない。
- nil timingsや条件欠落の扱いと、form/module/toolchain条件の比較境界を維持する。

## Must not

- `quality-tools.yml` の値を、手元Macで偶然選ばれた1.27.0に合わせるだけで修正完了にしない。バージョン更新自体が別途必要なら意図的なrepository-wide contract変更として判断する。
- GO toolchain選択をCodexの個別指示や都度の手作業に依存させない。
- 既存のCLI/State/Schemaへの歴史的互換コード、新規wrapper daemon、手動のfallback成功扱いを追加しない。
- CIの分割変更やBundle資源サンプルの追加をこのTaskへ混ぜない。

- GO toolchain不一致を残したまま異なるversionの実測を比較し、速度改善を証明したことにしない。
- テスト削除・skip・timeout延長・品質gate緩和・失敗の成功扱いで高速化を達成したことにしない。
- 重複テストの見直しは、独立したsemantic coverageの証明なしに削除しない。
- 正規Quality Gateを都度の手製ラッパや直接`go test`実行へ置き換えない。必要なdiagnostic実験とcanonical validationの違いは証拠上区別する。
- 無関係なTask実装、CIの設定変更、未完了Bundle資源収録を勝手に取り込まない。

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

1. macOSの正規Quality Gateにおける規定外Go 1.27.0使用の発生経路を特定し、`quality-tools.yml` と実際の検証toolchainが一致することを機械的に保証する。親Codexへの注意喚起やGoの値の変更だけで是正完了としない。
2. 外部 `PATH` / `GOTOOLCHAIN` が異なる条件でも、実際の `go test` / `go test -race` と保存されたevidenceが規定に一致するか、明示的に失敗することを検証する。source binding、resume/error経路、検証証拠を保持する。
3. 整合性を成立させた後、Macの20～30分の主な所要時間をpackage/test実測で説明し、単独実行・full suite、Linux CIとの差、並列実行・プロセス・Git・I/O・メモリの影響を切り分ける。証拠がない事項は未確定とする。
4. 改善を採用する場合は同条件の正規Quality Gateで変更前後のwall-clock、主要package/test時間、CIの回帰を比較し、検証のcoverageや品質を落とさず短縮できたことを確認する。改善不能なら確認できた原因と残る制約を明示し、未達を完了扱いしない。
5. 関連テスト、正規validation、独立Review、Runtime/source整合確認を実施する。利用できないOS・環境の確認結果は成功済みとみなさない。

- child成功/失敗それぞれにtimings書込失敗を注入し、terminal statusとexit provenanceを検証する。
- record/必要logを残し、通常timings成功経路を維持する。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

- 最新runの複数copyが存在してもRunsを水増しせず、真の旧baselineを選ぶ。
- 重複しかない場合は自己比較せず、異内容同IDは誤った改善/劣化へ数えない。

## Historical invariants

- `IMPLEMENTATION_RULES.md` に従いTask authority、fail-closed、machine-only schema、1 Task 1 Commit、Codex消費削減を維持する。
- 正規Quality Gateのsource bindingとPASS evidenceの意味を保ち、品質・速度の両方で不正な成功扱いを防ぐ。

## Dependencies

none
