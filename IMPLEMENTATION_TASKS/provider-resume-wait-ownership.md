# Task: Z.ai再開待機をCLIの実行受付から分ける

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B16. Focus Z.ai self-resume wait policy outside app CLI admission source — #1643
> - **Owner/problem:** `app/execution.go` mixes CLI/Controller/repository admission with long-lived provider reset/peak-pause/stop/retry scheduling logic.
> - **Historical rationale:** provider self-resume (#961/#1252) and peak-pause (#1531) grew inside established execution entry file.
> - **Implement/minimum boundary:** same-package provider-specific wait/self-resume source owner with existing call flow; keep CLI start/lock admission in app, model-stage recovery in workflow. No generic scheduler/plugin.
> - **Preserve/risk:** 10-second/reset and bounded retry horizon policy, injected clock, checkpoint/Task identity, schedule/interrupt/stop, non-retry errors, lock lifetime and exact CLI semantics. No repository policy in generic wait mechanics.
> - **Acceptance:** Z.ai self-resume/peak pause, CLI/Controller E2E tests and cross-platform CI pass; only targeted source surface changes.

## Purpose

Z.ai再開待機をCLIの実行受付から分ける。

## External feasibility

status: not-applicable

## Contract

app/execution.goのZ.ai待機・retryとCLI/controller admissionを分離。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

reset・peak pause・stop・Task identity・lock保持・非retry errorを維持。汎用schedulerを増やさない。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
