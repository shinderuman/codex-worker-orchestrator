# Task: parent-onlyのauthority同期テストを独立させる

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B15. Separate parent-only sync test authority from GLM-runtime sync tests — #1635
> - **Owner/problem:** `app/controller_task_authority_sync_test.go` has ~715 lines GLM-runtime integration plus ~185 lines parent-only no-model-runtime Amendment and evidence spoofing negatives.
> - **Historical rationale:** parent-only mode was added after ordinary GLM lane tests.
> - **Implement/minimum boundary:** same-package test-only parent-only negative/positive scenarios/helpers to focused owner; reuse current shared fixture, no compatibility alias or workflow changes.
> - **Preserve/risk:** no GLM promotion, exact smoke identity, no side effects on rejected evidence, source/worktree tamper, checkpoint/retry, ordinary lane remains distinct.
> - **Acceptance:** both authority-mode suites and full CI pass, no test omissions; source-read/review locality measured.

## Purpose

parent-onlyのauthority同期テストを独立させる。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

controller_task_authority_sync_test.goのparent-only側の成功・証拠偽装拒否を分ける。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

GLM runtimeを起動しない経路、拒否時無変更、通常laneの独立性を確認。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
