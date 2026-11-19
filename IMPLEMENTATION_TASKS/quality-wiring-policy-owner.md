# Task: リポジトリ固有の品質規則と照合処理を分ける

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B05. Separate quality-wiring repository policy from generic scanning — #1644
> - **Owner/problem:** `harnesslint/quality_surface.go` interleaves repository-required CI/install/hook/workflow wiring tables with generic Go AST/text/binding matcher implementation.
> - **Historical rationale:** exact source-wiring checks and robust AST matching evolved together in one harnesslint file.
> - **Implement/minimum boundary:** same-package focused repository rule-inventory owner + generic AST/text matcher owner; one narrow dispatch, no public framework or new registry.
> - **Preserve/risk:** exact required/forbidden paths and bindings, AST aliases/order, fail-closed parse/missing-file behavior, lint/Fix output and current CI self-protection. Do not move repository policy to generic runtime.
> - **Acceptance:** existing source-wiring, injected-negative/mutation, AST/binding, CI and lint tests pass; before/after bounded source-read/review proxy recorded.

## Purpose

リポジトリ固有の品質規則と照合処理を分ける。

## External feasibility

status: not-applicable

## Contract

harnesslint/quality_surface.goの固有CI・hook規則とAST照合を同package内で分離。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

alias、binding順序、欠落・parse失敗の負例を維持。変更時に読む範囲を対比。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
