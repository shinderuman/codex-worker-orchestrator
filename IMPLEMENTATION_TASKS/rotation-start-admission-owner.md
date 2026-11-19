# Task: 通常開始とrotation開始の判定元を統一する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B01. One canonical session-rotation task-start admission — #1631
> - **Owner/problem:** `state/session_rotation.go`, `state/session_rotation_boundary.go`, `new_task_transition.go` and app/workflow callers expose conflicting `Pending` ordinary-start contracts (old `ValidateNewTaskRotation` rejects vs production boundary admits and atomically retires) and overlapping claimed-start predicates.
> - **Historical rationale:** later pending-recommendation retirement and commit-time staging were added after strict old marker rejection; old test-only/public predicates survived.
> - **Implement/minimum boundary:** make production `AdmitNewTaskRotationBoundary` semantics canonical, share the *predicate* across read-only preflight and fresh mutation-time recheck, retire obsolete entry/test expectations only if no current stable consumer; migrate tests, no old alias.
> - **Preserve/risk:** `Pending` ordinary start + atomic staged retirement/rollback, `Claimed/Bound/Issued` identity, correct thread/target, retry checkpoint, crash recovery, revalidation and no preflight mutation. Generic state machine; no repository Plan policy.
> - **Acceptance:** tests prove pending ordinary start vs claimed rejection, concurrent marker transition failure, issued idempotent retry and unchanged task/journal safety.

## Purpose

通常開始とrotation開始の判定元を統一する。

## External feasibility

status: not-applicable

## Contract

stateの旧 `ValidateNewTaskRotation` と新 `AdmitNewTaskRotationBoundary`。通常startのPending退役は後者へ統一。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

Pending/Claimed/Issued、別thread、競合、rollbackを確認。preflightは読取専用。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
