# Task: macOSのGoテスト所要時間を原因別に分解し短縮する

## Original instruction

````text
かつMac側の速度改善はそういうタスクをCodexでやるタスクにするべきだと思うのでこれもまたIssueとPRを作っておいてくれ
````

## Amendments

none

## Resolved references

- 「Mac側」: M1 Mac mini、メモリ16GB、Apple Silicon。2026-10-25のユーザー提示Bundle `1a25e51c-8935-4150-8b6f-520d2d0d3848.zip` は `darwin/arm64`、Go 1.27.0、parallelism 8を記録。
- Bundleの正規通常 `go-test` 3 runは約25分（タイムアウトFAIL）・30分（PASS）・32分（PASS）。成功回の `internal/app` は約30分、`internal/controller` は約15～17分。
- Linux CIの2026-10-25比較実行では、 `internal/app` 81～104秒、`internal/controller` 93～114秒。CIはLinux x86_64かつ独立matrixのため、ソース/バージョン/同時負荷/キャッシュが一致した比較ではない。
- Go version不一致の是正は別Task `IMPLEMENTATION_TASKS/quality-gate-go-toolchain-authority.md`。BundleのCPU・メモリ・swapサンプリングは別PR #1387 が追加予定の `IMPLEMENTATION_TASKS/bundle-host-resource-observability.md` に委ねる。

## Purpose

正規Go検証のcoverageと失敗検出力を維持したまま、Macの通常Go suiteが20～30分以上かかる原因を定量的に把握し、費用対効果のある高速化を実装・検証する。

## Contract

- `quality-gate-go-toolchain-authority.md` の完了後、同じsourceと固定Go version（`quality-tools.yml`）の比較条件を確立する。`GOVERSION`、arch/OS、GOFLAGS、GOMAXPROCS、cache、go test args、source snapshotを照合し、環境差を明示する。
- 現行Quality Gateの `test-timings.json`、Go JSONログ、CIジョブログと既存bundle/telemetryを優先し、パッケージ別・テスト別にクリティカルパスを特定する。 `app`、`controller` を特に重点確認する。
- Mac単独パッケージ実行とfull suite中のパッケージ時間の差、プロセス/テスト並列化、Gitやsubprocess起動、disk I/O、メモリ圧迫とswap、ビルドcache状態、macOS固有fixtureの影響を根拠に基づいて切り分ける。新Resource観測Taskが未完了なら測定不能部分を明示し、存在しない数値を推定しない。
- 測定のための高コストなfull suiteを無制限に繰り返さない。比較条件を固定した必要な実行を計画し、同一条件の既存PASS証拠を再利用する。低コストのisolated package/target testやinstrumentationを使える場合は優先する。
- 根本原因に基づく実装上の改善（不要な待機、同期I/Oや子プロセス実行、fixtureの反復、資源競合、適切な検証分割）を評価し、実際に速度に寄与するものだけ採用する。CI側の追加分割やGo version変更は本Taskの目的そのものにはしない。
- 改善前後のwall-clock、主要package/test時間、CPU/I/O等可能な資源証拠を同じ条件で比較し、局所最適で全体が悪化していないか確認する。速くならない場合も原因・確認限界を正確に記録し、無理に変更を作らない。

## Must not

- GO toolchain不一致を残したまま異なるversionの実測を比較し、速度改善を証明したことにしない。
- テスト削除・skip・timeout延長・品質gate緩和・失敗の成功扱いで高速化を達成したことにしない。
- 重複テストの見直しは、独立したsemantic coverageの証明なしに削除しない。
- 正規Quality Gateを都度の手製ラッパや直接`go test`実行へ置き換えない。必要なdiagnostic実験とcanonical validationの違いは証拠上区別する。
- 無関係なTask実装、CIの設定変更、未完了Bundle資源収録を勝手に取り込まない。

## Acceptance criteria

1. Macの20～30分の主な実行時間要因を、package/testごとの実測と固定ツールチェーン条件で説明できる。OS固有要因・資源競合が未確定なら明示する。
2. 改善を採用する場合は、Macの正規Quality Gateにおける前後のwall-clock/クリティカルパスとCIの回帰を記録し、再現性があり品質の劣化がないと説明できる。
3. 対応が未解決なら実測・仮説・追加観測条件を区別して未完了とする。必要な追加TaskがあればPlanへ分離する。
4. 関連テスト、既存の必要なValidation、独立Review、正規Lifecycleを維持する。

## Historical invariants

- Quality Gateのsource binding、実行条件、PASS evidenceは変えず、`IMPLEMENTATION_RULES.md` に従う。

## Dependencies

- `IMPLEMENTATION_TASKS/quality-gate-go-toolchain-authority.md`
