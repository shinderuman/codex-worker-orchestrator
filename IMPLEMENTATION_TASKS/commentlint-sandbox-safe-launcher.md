# Task: commentlintをCodex sandbox内で再現可能に実行する

## Original instruction

````text
`commentlint` launcherのsandbox compatibilityを通常のCodex + GLM implementation taskとして解決する。

現状、repository rootの`harnesslint` launcherは`quality-tools.yml`からGo versionを読み、`GOTOOLCHAIN`と`${TMPDIR:-/tmp}`配下の`GOCACHE`を固定している。一方`commentlint` launcherはplain `go run`で、実運用ではCodex sandbox内から`~/Library/Caches/go-build`へアクセスして`operation not permitted`となり、親CodexがGuardian escalation付きで再実行した。

commentlint自体に追加capabilityが必要なわけではない。launcher境界を修正し、通常validationでsandbox内からそのまま成功するようにする。
````

## Amendments

none

## Resolved references

- 現行`harnesslint` launcherのGo toolchain / cache setupを既存のrepository-owned実装例として扱う。

## Purpose

commentlintの実行環境差による予測可能なsandbox failureと、その後の親Codex再判断・Guardian escalationを除去する。

## External feasibility

status: not-applicable

## Contract

- `commentlint`をCodex Desktopの通常sandbox内で実行可能にする
- Go versionは既存のrepository authorityである`quality-tools.yml`から取得し、別のversion sourceを作らない
- user homeのGo build cacheへ依存せず、既存のtemp cache conventionと整合させる
- `harnesslint`との共通化が有利でも、2つの小さなlauncherのためだけにgeneric shell frameworkを作らない
- commentlintの検査内容・failure semanticsを弱めない

## Must not

- commentlintを通すためにcheckをskip・緩和しない
- home Go cacheへのアクセス目的でGuardian escalationを恒常化しない
- Go version literalを複数箇所へ複製しない
- full-suite `go test` capability gateや他のquality gate責務まで無関係に変更しない

## Acceptance criteria

- clean/current repositoryで`./commentlint`がCodex Desktop sandbox内から`~/Library/Caches/go-build`へアクセスせず成功する
- `commentlint`と`harnesslint`が意図したGo toolchain sourceを共有する
- ordinary validation pathでcommentlintのGo cacheだけを理由としたGuardian escalationが不要になる
- 既存commentlint / harnesslint behaviorと関連testが維持される
- repositoryが要求するlint/test/validationを完了し、独立reviewと必要なSol品質gateを通す

## Historical invariants

- 親Codexのmodel/Guardian turnはCodex Reduction上のcostであり、予測可能なsandbox failureを一度踏んでから同じcommandを昇格再実行する運用を正常系にしない。
- sandbox内で成立するquality gateはsandbox内に留め、追加capability根拠のあるfull suite等とは境界を分ける。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。Task 021 parent decision gateより先に、このtaskだけを通常Codex + GLM workflowで完了する。
