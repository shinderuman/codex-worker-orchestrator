# Task: 現行commandの到達性で廃止済み経路の復活を検出する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B04. Replace historical clean-cutover AST inference with live owner proof — #1638
> - **Owner/problem:** `harnesslint/clean_cutover.go` reconstructs a *retired* rejection predicate and synthetic legacy helper graph even though current `parentactioncmd/command_metadata.go` has a different live registry/dispatch shape.
> - **Historical rationale:** #1311/#1299 required rejection-only legacy-root removal and machine enforcement; the old AST recognizer remained after cutover.
> - **Implement/minimum boundary:** mechanically prove current canonical registry has no registered rejection-only tombstone and each live command is executable; then remove no-longer-valid historical AST traversal/safe-Fix logic only **after** equivalent enforcement is demonstrated; update provenance locators/tests.
> - **Preserve/risk:** no backward-compatible tombstones, strict unknown-command rejection, all current action grammar and safety, current machine-control provenance and deadcode checks; no extra generic inference framework.
> - **Acceptance:** negative test with a newly inserted rejected-only registered root fails, live command registry works, lint/Fix/provenance/Go checks remain effective.

## Purpose

現行commandの到達性で廃止済み経路の復活を検出する。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

harnesslintのclean-cutover AST判定を現行parentaction registryの検証へ移す。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

rejection-only登録を注入した負例で検出してから旧検査を退役。旧command名をaliasとして残さない。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
