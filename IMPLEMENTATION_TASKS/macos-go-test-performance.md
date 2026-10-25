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

2026-10-26 追加要求（原文）:

````text
バージョン不整合の問題はさっきのPRでのタスクでの修正範囲に入っているのか
````

````text
Goのバージョンの作業内容を修正するPRを出せ
````

## Resolved references

- `quality-tools.yml` の `go` がrepositoryの既存のtoolchain contract。観測当時は `go: 1.25.4` で、CIの `actions/setup-go` と `goquality` はこの値を参照する。
- 2026-10-25のmacOS Bundle `1a25e51c-8935-4150-8b6f-520d2d0d3848.zip` の3つの `live/validation/quality-gate/*/run.json` は `toolchain.go_version=go1.27.0`、`goos=darwin`、`goarch=arm64`、`test_cache=executed` を報告。これは正規Quality Gateが利用したGoバージョンの証拠であり、親Codexが意図的に変更した証拠とは区別する。
- 現行 `glm-worker/internal/app/quality_gate_execution.go` の `runQualityGateProcess` は `exec.Command("go", ...)`、`glm-worker/internal/app/quality_gate_persistence.go` の `qualityGateEnv` は `GOFLAGS`を消去するが、環境の `GOTOOLCHAIN` / `PATH`を継承する。 `glm-worker/internal/qualitygate/run_record.go` の `CaptureToolchain` は観測を記録するだけで規定値を強制していない。

- 「Mac側」: M1 Mac mini、メモリ16GB、Apple Silicon。2026-10-25のユーザー提示Bundle `1a25e51c-8935-4150-8b6f-520d2d0d3848.zip` は `darwin/arm64`、Go 1.27.0、parallelism 8を記録。
- Bundleの正規通常 `go-test` 3 runは約25分（タイムアウトFAIL）・30分（PASS）・32分（PASS）。成功回の `internal/app` は約30分、`internal/controller` は約15～17分。
- Linux CIの2026-10-25比較実行では、 `internal/app` 81～104秒、`internal/controller` 93～114秒。CIはLinux x86_64かつ独立matrixのため、ソース/バージョン/同時負荷/キャッシュが一致した比較ではない。
- Goバージョン逸脱の是正とMacテスト高速化はこのTaskに統合する。BundleのCPU・メモリ・swapサンプリングは独立Task `IMPLEMENTATION_TASKS/bundle-host-resource-observability.md` に委ねる。
- 「バージョン不整合」の追加対象: 正規Quality Gateが契約外のGoを使った問題に加え、Codex・GLMが診断・局所確認のためにQuality Gate外で直接起動する `go test`（focused package/test、必要なraceやintegrationテスト等）も、同じrepositoryのGo version契約に従わせる。診断用テストのPASSと正規Quality GateのPASSは別物として扱う。

## Purpose

正規Go Quality Gateがリポジトリの `quality-tools.yml` が選択したバージョンを確実に使う状態を成立させた上で、Macの通常Goテストが20～30分以上かかる原因を定量的に特定し、coverageと失敗検出力を維持しつつ短縮する。バージョン整合性と性能改善は単一Taskの連続する責務として扱う。

## Contract

### 正規Go toolchainの整合

- 正規 `--quality-gate` / `glm-parent-action finalize-check` から実際の `go test` 起動までの単一のcanonical ownerで、Git snapshotに対応する `quality-tools.yml` の `go` 値を検証し実行環境へ適用する。既存の `goquality`、CIの `setup-go` と同じ正本に従う。
- 開始時に解決・記録した要求バージョンと実際にテストを実行するGoバージョンが一致していることを、プロセスの実環境で保証する。外部の `PATH` や `GOTOOLCHAIN` の設定により黙って別バージョンへ切り替わらない。実行中のsource/version不一致や要求toolchain取得不能はfail closedとし、誤ったPASSを生成しない。
- 既存のtoolchain/report（version, GOOS/GOARCH, GOCACHE, command, cache usage）に実行結果の正しい証拠を保持する。 `go.mod` の `go 1.22` は必要最低バージョン契約であり `quality-tools.yml` の固定実行バージョンとは区別する。
- `go test` / `go test -race` の正規run・restart/attach/resultとタスク・snapshot bindingが崩れないことを確認する。既存のrecording・validation・export経路へ結び付ける。
- MacとCIの両方で、規定どおりに実行されたことが検証可能であること。必要なテストは環境変数で別バージョンを注入しても、失敗または正規バージョンへ明示的に固定されることを示す。

### 正規Quality Gate外のGo診断実行にも同じtoolchain契約を適用する

- 親CodexとGLM Worker/Reviewerが実装中の局所検証・調査で起動する `go test`（`-run`、単一package、integration、`-race` 等）も、既存の `quality-tools.yml` の `go` 値を唯一の実行バージョン正本とする。正規Quality Gateの外にあることを理由に、`PATH` 上の任意のGoや自動選択された別バージョンで検証しない。
- 現行の親/Worker/Reviewerのコマンド実行境界とrepository-owned toolchain選択の仕組みを確認し、既存の機械制御面を優先して一貫したバージョンを適用・検証する。Codex/GLMの自発的な注意や毎回の手製ラッパにだけ依存させない。規定バージョンを利用できないときは、診断結果を正しい条件でのPASSと扱わず、実行を停止・失敗として報告する。
- 診断用Goテストの証拠には、実使用Goバージョン、対象source、コマンド・実行範囲と結果を追跡可能にし、正規Quality Gateの結果とは区別する。Goバージョンが異なる過去の診断結果は、規定バージョンでの検証として流用しない。
- 本Taskが必要とする診断経路の範囲を越えた汎用コマンド制御の再設計はしない。既存の検証・実行インターフェースで不足があれば、canonical ownerに必要な機能を加えるか、検証不能として明示する。

### 比較可能な条件を確立した上でのMac高速化

- このTask内で上記のtoolchain整合性を成立・検証した後、同じsourceと固定Go version（`quality-tools.yml`）の比較条件を確立する。`GOVERSION`、arch/OS、GOFLAGS、GOMAXPROCS、cache、go test args、source snapshotを照合し、環境差を明示する。
- 現行Quality Gateの `test-timings.json`、Go JSONログ、CIジョブログと既存bundle/telemetryを優先し、パッケージ別・テスト別にクリティカルパスを特定する。 `app`、`controller` を特に重点確認する。
- Mac単独パッケージ実行とfull suite中のパッケージ時間の差、プロセス/テスト並列化、Gitやsubprocess起動、disk I/O、メモリ圧迫とswap、ビルドcache状態、macOS固有fixtureの影響を根拠に基づいて切り分ける。新Resource観測Taskが未完了なら測定不能部分を明示し、存在しない数値を推定しない。
- 測定のための高コストなfull suiteを無制限に繰り返さない。比較条件を固定した必要な実行を計画し、同一条件の既存PASS証拠を再利用する。低コストのisolated package/target testやinstrumentationを使える場合は優先する。
- 根本原因に基づく実装上の改善（不要な待機、同期I/Oや子プロセス実行、fixtureの反復、資源競合、適切な検証分割）を評価し、実際に速度に寄与するものだけ採用する。CI側の追加分割やGo version変更は本Taskの目的そのものにはしない。
- 改善前後のwall-clock、主要package/test時間、CPU/I/O等可能な資源証拠を同じ条件で比較し、局所最適で全体が悪化していないか確認する。速くならない場合も原因・確認限界を正確に記録し、無理に変更を作らない。

## Must not

- `quality-tools.yml` の値を、手元Macで偶然選ばれた1.27.0に合わせるだけで修正完了にしない。バージョン更新自体が別途必要なら意図的なrepository-wide contract変更として判断する。
- GO toolchain選択をCodexの個別指示や都度の手作業に依存させない。
- 正規Quality Gateだけを固定して、Codex/GLMの個別・診断用Goテストには規定外バージョンを許容する状態を完了扱いにしない。診断結果を正規Quality Gateの成功証拠に昇格させない。
- 既存のCLI/State/Schemaへの歴史的互換コード、新規wrapper daemon、手動のfallback成功扱いを追加しない。
- CIの分割変更やBundle資源サンプルの追加をこのTaskへ混ぜない。

- GO toolchain不一致を残したまま異なるversionの実測を比較し、速度改善を証明したことにしない。
- テスト削除・skip・timeout延長・品質gate緩和・失敗の成功扱いで高速化を達成したことにしない。
- 重複テストの見直しは、独立したsemantic coverageの証明なしに削除しない。
- 正規Quality Gateを都度の手製ラッパや直接`go test`実行へ置き換えない。必要なdiagnostic実験とcanonical validationの違いは証拠上区別する。
- 無関係なTask実装、CIの設定変更、未完了Bundle資源収録を勝手に取り込まない。

## Acceptance criteria

1. macOSの正規Quality Gateにおける規定外Go 1.27.0使用の発生経路を特定し、`quality-tools.yml` と実際の検証toolchainが一致することを機械的に保証する。親Codexへの注意喚起やGoの値の変更だけで是正完了としない。
2. 外部 `PATH` / `GOTOOLCHAIN` が異なる条件でも、実際の `go test` / `go test -race` と保存されたevidenceが規定に一致するか、明示的に失敗することを検証する。source binding、resume/error経路、検証証拠を保持する。
3. Codex/GLMの正規Quality Gate外の個別 `go test` / focused / race / integration診断でも、規定のGoバージョンが実際に選択されるか、規定外の実行が明示的に拒否されることを、異なる `PATH` / `GOTOOLCHAIN` 条件と実行証拠で検証する。診断結果が正規GateのPASSと混同されないことも確認する。
4. 整合性を成立させた後、Macの20～30分の主な所要時間をpackage/test実測で説明し、単独実行・full suite、Linux CIとの差、並列実行・プロセス・Git・I/O・メモリの影響を切り分ける。証拠がない事項は未確定とする。
5. 改善を採用する場合は同条件の正規Quality Gateで変更前後のwall-clock、主要package/test時間、CIの回帰を比較し、検証のcoverageや品質を落とさず短縮できたことを確認する。改善不能なら確認できた原因と残る制約を明示し、未達を完了扱いしない。
6. 関連テスト、正規validation、独立Review、Runtime/source整合確認を実施する。利用できないOS・環境の確認結果は成功済みとみなさない。

## Historical invariants

- `IMPLEMENTATION_RULES.md` に従いTask authority、fail-closed、machine-only schema、1 Task 1 Commit、Codex消費削減を維持する。
- 正規Quality Gateのsource bindingとPASS evidenceの意味を保ち、品質・速度の両方で不正な成功扱いを防ぐ。

## Dependencies

none
