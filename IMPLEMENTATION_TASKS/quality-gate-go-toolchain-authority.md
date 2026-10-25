# Task: 正規Quality GateのGo toolchainをrepository contractへbindする

## Original instruction

````text
Bundleが1.27.0を使ってるってことを示してるんだとしたらそれはCodexが勝手にバージョンを変えて作業してるって言うクソ行為だから是正させなきゃいけないんじゃないの
````

## Amendments

none

## Resolved references

- `quality-tools.yml` の `go` がrepositoryの既存のtoolchain contract。観測当時は `go: 1.25.4` で、CIの `actions/setup-go` と `goquality` はこの値を参照する。
- 2026-10-25のmacOS Bundle `1a25e51c-8935-4150-8b6f-520d2d0d3848.zip` の3つの `live/validation/quality-gate/*/run.json` は `toolchain.go_version=go1.27.0`、`goos=darwin`、`goarch=arm64`、`test_cache=executed` を報告。これは正規Quality Gateが利用したGoバージョンの証拠であり、親Codexが意図的に変更した証拠とは区別する。
- 現行 `glm-worker/internal/app/quality_gate_execution.go` の `runQualityGateProcess` は `exec.Command("go", ...)`、`glm-worker/internal/app/quality_gate_persistence.go` の `qualityGateEnv` は `GOFLAGS`を消去するが、環境の `GOTOOLCHAIN` / `PATH`を継承する。 `glm-worker/internal/qualitygate/run_record.go` の `CaptureToolchain` は観測を記録するだけで規定値を強制していない。

## Purpose

親Codex/GLMの手元環境にインストールされたGoが何であっても、正規Quality Gateはrepository-ownedの `quality-tools.yml` が選択するGoを利用する。規定と異なるバージョンでPASS evidenceを作成しない。

## Contract

- 正規 `--quality-gate` / `glm-parent-action finalize-check` から実際の `go test` 起動までの単一のcanonical ownerで、Git snapshotに対応する `quality-tools.yml` の `go` 値を検証し実行環境へ適用する。既存の `goquality`、CIの `setup-go` と同じ正本に従う。
- 開始時に解決・記録した要求バージョンと実際にテストを実行するGoバージョンが一致していることを、プロセスの実環境で保証する。外部の `PATH` や `GOTOOLCHAIN` の設定により黙って別バージョンへ切り替わらない。実行中のsource/version不一致や要求toolchain取得不能はfail closedとし、誤ったPASSを生成しない。
- 既存のtoolchain/report（version, GOOS/GOARCH, GOCACHE, command, cache usage）に実行結果の正しい証拠を保持する。 `go.mod` の `go 1.22` は必要最低バージョン契約であり `quality-tools.yml` の固定実行バージョンとは区別する。
- `go test` / `go test -race` の正規run・restart/attach/resultとタスク・snapshot bindingが崩れないことを確認する。既存のrecording・validation・export経路へ結び付ける。
- MacとCIの両方で、規定どおりに実行されたことが検証可能であること。必要なテストは環境変数で別バージョンを注入しても、失敗または正規バージョンへ明示的に固定されることを示す。

## Must not

- `quality-tools.yml` の値を、手元Macで偶然選ばれた1.27.0に合わせるだけで修正完了にしない。バージョン更新自体が別途必要なら意図的なrepository-wide contract変更として判断する。
- GO toolchain選択をCodexの個別指示や都度の手作業に依存させない。
- 既存のCLI/State/Schemaへの歴史的互換コード、新規wrapper daemon、手動のfallback成功扱いを追加しない。
- 実際のテスト高速化、分割CI、Bundle資源サンプルの追加を本Taskへ混ぜない。

## Acceptance criteria

1. Mac側の規定外1.27.0使用を再現・説明でき、ユーザーから渡されたQuality Gate正規入口の契約が `quality-tools.yml` と一致する。
2. 外部 `PATH` / `GOTOOLCHAIN` が異なる条件でも、実際の `go test` と保存されたevidenceが規定と一致するか、FAILで止まることをテストで確認する。
3. 種類の異なる正規gate、必要なresume/error経路、source binding、失敗時の証拠記録が維持される。
4. 関連の実tests、適切なQuality Gate、独立Review、ソース・Runtime一致検証が完了する。実行環境にないplatformでの確認は未検証として明示する。

## Historical invariants

- `IMPLEMENTATION_RULES.md` のTask authority / machine-only schema / fail-closed とCodex消費削減を維持する。

## Dependencies

none
