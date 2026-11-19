# Task: 保留中のController操作を一箇所からhandoffへ投影する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B02. Canonical Controller pending-transition handoff projection — #1639
> - **Owner/problem:** `app/parent_handoff_controller.go::applyPendingControllerMaintenanceHandoff` independently selects completion/advancement recovery while `controller/publication_operation_projection.go::ProjectParentCompletion` already projects pending journal operations.
> - **Historical rationale:** app handoff fallback bridged the prior publication/Controller cutover and retained separate pending-kind dispatch.
> - **Implement/minimum boundary:** controller read-only operation projection selects recovery/typed command exactly once; app only maps that projection to handoff/evidence fallback. Retain `app → workflow → controller` execution ownership.
> - **Preserve/risk:** pending journal precedence over summary, generation checks, corrupted/missing journal rejection, no mutation on read, lock/typed command/replay safety; repository-specific policy does not move to generic Controller.
> - **Acceptance:** pending completion and advancement, stale/corrupt transition, retry, summary precedence and current handoff shape regressions.

## Purpose

保留中のController操作を一箇所からhandoffへ投影する。

## External feasibility

status: not-applicable

## Contract

appの `applyPendingControllerMaintenanceHandoff` がpending種別を再選択。controllerの `ProjectParentCompletion` に集約。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

journalの優先、generation、不正journal拒否、replayを確認。appは表示を所有。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
