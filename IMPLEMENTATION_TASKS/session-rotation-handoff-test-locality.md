# Task: rotationのhandoff統合テストをまとめる

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B14. Focus app session-rotation handoff integration tests — #1634
> - **Owner/problem:** `app/parent_handoff_test.go` mixes ordinary handoff/recovery/quality assertions with ~250 lines of rotation state tests, despite focused rotation mismatch tests.
> - **Historical rationale:** rotation-handoff integration originally lived in broad app handoff tests; later focused tests were added.
> - **Implement/minimum boundary:** move `TestSessionRotation*` + coherent fixture to same-package rotation handoff tests; retain normal handoff tests and relocate quality routing assertion only to existing quality test owner where cohesive.
> - **Preserve/risk:** accepted marker/bind/retry/thread/task/retire and failure assertions, exact output, no duplicate helpers or changed test parallelism. No code behavior change.
> - **Acceptance:** same integration cases pass; bounded rotation vs handoff model-visible test reads are local.

## Purpose

rotationのhandoff統合テストをまとめる。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

app/parent_handoff_test.goのrotation群を同packageのrotationテストへまとめる。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。 B01でrotation挙動を変更する場合は、先にその挙動を確定する。

## Acceptance criteria

Pending退役・不正marker・retry等を同じintegration入口から検証。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
