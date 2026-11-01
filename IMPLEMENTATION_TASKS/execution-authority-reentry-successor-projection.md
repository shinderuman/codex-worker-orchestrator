# Task: execution authority再入院・successor投影のfail-closed境界

## Original instruction

````text
parent-only predecessorのRuntime再入院拒否と、retired-authority error時のsuccessor mode検証を現行実装へ追加する。
````

## Amendments

none

## Resolved references

- CR-1440-2 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1440#discussion_r4434924607 · `glm-worker/internal/controller/execution_lane.go:88-91`付近
  原文: "**Reject Runtime reentry for a predecessor attempt that was parent-only.** The guard runs in one direction only. A predecessor must be parent-only when the committed mode is `parent-only`. In the reverse case there is no check. The function returns early when `committedTaskExecutionMode` reports `glm` or an empty mode at `HEAD`. Trigger: the parent removes or changes the `## Execution authority` section after a parent-only candidate reenters. `MaterializeExecution` then plans an ordinary Runtime attempt for a suspension whose predecessor never had a Runtime. `executeControllerMaterialize` then calls `reenterMaterializedRuntimeCompletion` with a non-empty `PredecessorAttemptID`. That call depends on Runtime state that does not exist. The parent-only validation inheritance in `validateParentOnlyInheritedValidations` is also skipped. Load the predecessor before the early return. Fail closed when its authority differs from the committed mode."
- CR-1440-3 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1440#discussion_r4434924613 · `glm-worker/internal/controller/publication_operation_projection.go:314-320`付近
  原文: "**Check the successor mode before propagating retired-authority errors.** `parentOnlySuccessorAuthority` validates the retired authority before it resolves the current execution mode. For a non-pristine retired head, that error returns from quiescent projection before `projectCompletionSuccessor` can start an ordinary successor. The external-advancement path does not cover retired heads without `PublicationPolicy`. Decline parent-only materialization when the current authority is not `parent-only`, but preserve fail-closed behavior when the current authority cannot be resolved."

## Purpose

execution authorityの双方向不一致をfail-closedで拒否し、retired headでも正規successor投影が始まるようにする。

## External feasibility

status: not-applicable

## Contract

- `bindMaterializedExecutionAuthority`はcommitted modeが非parent-onlyでもsuspension predecessorを読み、 predecessor authorityとcommitted modeの不一致をfail-closed拒否する。load errorは伝播する。
- quiescent投影の`parentOnlySuccessorAuthority` error分岐で、非pristine headは`ResolveCommittedTaskAuthority`で現在authorityを解決し、非parent-onlyならparent-only materializationを辞退（errorなし）して通常successorへ進む。現在authority解決失敗はfail-closedでerrorを返す。

## Must not

- 既存のparent-only→parent-only再入院規則・pristine head挙動を変更しない。authority解決失敗を黙殺しない。

## Acceptance criteria

- (i) parent-only predecessor＋committed mode除去後にRuntime materializeが拒否される負case、(ii) parent-only predecessor＋parent-only committedの再入院が現行どおり成立する正case、(iii) retired head＋現在authority非parent-onlyで通常successorが開始する正case、(iv) 現在authority解決失敗でfail-closedとなる負case、の4test。

## Historical invariants

none

## Dependencies

none
