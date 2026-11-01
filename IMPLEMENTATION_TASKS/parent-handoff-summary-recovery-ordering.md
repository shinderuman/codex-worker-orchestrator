# Task: summary recovery告知前のcanonical controller handoff適用

## Original instruction

````text
buildParentHandoffWithConfigのsummary recovery早期returnがapplyCanonicalControllerHandoffを飛ばす問題を修正する。
````

## Amendments

none

## Resolved references

- CR-1440-1 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1440#discussion_r4434924603 · `glm-worker/internal/app/parent_handoff_automation.go:30-31`付近
  原文: "** Major · Functional Correctness · Heavy lift** **Apply canonical controller handoff before advertising summary recovery.** `SummaryRecoveryAvailable()` does not establish that the canonical controller is idle or that no transition is pending. Available summary recovery can coexist with a pending controller transition. The early return can advertise `resubmit-report` without the controller recovery action or status. Let `applyCanonicalControllerHandoff` run before returning, while preserving the availability check."

## Purpose

`resubmit-report`告知がcanonical controller回復action/statusと矛盾して広告されないようにする。

## External feasibility

status: not-applicable

## Contract

- `SummaryRecoveryAvailable()`がnilの早期returnでも`applyCanonicalControllerHandoff`を先に適用する。availability判定自体は維持する。

## Must not

- `task-revision-worker-admission.md`（改訂拒否→sync→handoff）の境界と重複実装しない。

## Acceptance criteria

- summary recovery available＋pending controller transition状態で、出力がcanonical handoff適用後のaction/statusになる回帰test。

## Historical invariants

none

## Dependencies

none
