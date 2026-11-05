# Task: Darwin固有AcceptanceのPublication前検証

## Original instruction

Dogfood Audit #1503で、Darwin専用の変更と必須テストが正規Go品質検証の対象外になり、Task完了・Publication後に親Codexがユーザー許可を得て追加実行した問題を解消する。

## Amendments

none

## Resolved references

- [Audit #1503](https://github.com/shinderuman/codex-worker-orchestrator/issues/1503)、Task `quality-gate-supplement-and-darwin-sysctl-accuracy.md`、published source `8bcda792b4999084640ed4eeb62f7ee06c7a3c0e`。
- `glm-worker/internal/qualitygate/resource_samples_darwin_test.go` は `//go:build darwin`、Acceptanceは `TestDarwinSysctlDecoding` の3/7-byte正負検証を要求。
- 現在の正規 `go-test` はLinux ARM64 Dockerの `./...` とDarwin補完の `./internal/observationexec` であり、上記Darwin専用テストを実行しない。親はPublication後にだけ手動でDarwin testを実行しPASSを取得した。
- 既存の `canonical-goquality-duration-regression-remediation.md` は長時間化改善、`post-publication-terminal-handoff-scope-failure.md` は終端復旧を所有する。いずれもこのAcceptance漏れのownerではない。

## Purpose

Taskが要求するDarwin固有の検証を、正規Publication前に同一sourceの証拠として完了させる。後追いの手動検証を正常完了の代わりとしない。

## External feasibility

status: not-applicable

## Contract

- TaskがDarwin専用source/testの検証を要求する際、既存の正規品質検証ownerから、必要なDarwin testの実行とGo toolchain・source同一性を確認できるようにする。未検証・失敗・不一致ならPublication前にfail-closedにする。
- 通常のLinux ARM64 Docker検証と既存Darwin補完のContractを保持する。Darwin専用検証をLinuxでPASS扱いせず、後から得た成功を事前証拠としない。
- 専用検証の追加負荷を必要なTaskに限定し、既存インターフェース・記録を再利用する。汎用の第二品質Gate、daemon、手動例外前提を導入しない。
- 検証は実装変更の正負ケースを含め、既存の通常Task・parent-only Taskの実行経路を破壊しない。

## Must not

- 正規Go検証や既存テストを削除・skipして所要時間だけを改善する。
- Darwin専用のコード・テストをLinux検証の成功で代替する。
- ユーザーの事後許可を恒久的な実行権限として扱う。

## Acceptance criteria

- Darwin専用の必要テストを持つTaskで、対象テストの成功とsource/OID・toolchainの整合がPublicationより前に記録される。
- 同じ条件でDarwin専用テストのコンパイルエラー・失敗・証拠欠落・source不一致を注入すると、Publication前に拒否され、remoteとTaskの完了状態は不変である。
- Darwin専用Acceptanceを持たないTaskでは不要な追加検証を実行しない。通常・raceの既存実行要件とテスト範囲を維持する。
- 正規の経路で正負の実行証拠を取得し、最終差分を確認する。

## Historical invariants

- 実行済みTaskの本来のAccepted outcomeを事後に改ざんしない。Audit #1503の歴史的な後追い検証は既存証拠として残す。

## Dependencies

none
