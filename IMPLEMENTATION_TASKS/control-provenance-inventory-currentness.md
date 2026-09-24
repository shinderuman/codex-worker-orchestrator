# Task: Current machine-control provenance inventory reconciliation

## Original instruction

```text
今回の仕事は、#1136配下の open `campaign:refactor` Issueをcurrent stateから取得し、1件ずつ直列に実装してterminalにすること。

tracked implementation authorityが存在しない場合は、repositoryの既存workflowに従って必要なTask / Plan authorityをparent側で確立してからproduction実装へ進む。
```

## Amendments

none

## Resolved references

- Current adopted management item is GitHub Issue #1146, `[REFACTOR][1137] Restore current machine-control provenance inventory coverage`, selected from the current open `campaign:refactor` queue at main `904a513583ac0a2a509144e2fd9714f937acfdfd`.
- #1146 current evidence identifies `forward-only-compatibility` as a proven missing machine-control provenance entry and `internal/publicationguard` as an additional same-root missing focused machine owner. Its `parent-plan-continuation` evidence overlaps the separate partial-control representation root owned by #1145 and is not folded into this task.
- Existing execution-unit/milestone ownership remains within the parent-action/milestone control family unless current evidence proves a locator gap that can be repaired without creating another control identity.

## Purpose

Reconcile `codex/control-provenance.json` and its validation with current evidence-backed machine controls that are missing from the provenance index, without changing production behavior or creating a second behavior authority.

## Contract

- Production control code remains authoritative; `codex/control-provenance.json` remains provenance/index only.
- Add current provenance for the proven missing `forward-only-compatibility` machine gate using exact current production owner, focused test, and postcondition/violation locators.
- Add current provenance for the focused publication-guard setup machine control when its current owner/test/postcondition are independently traceable from current Git.
- Add only the minimum fail-closed coupling required so those proven current machine controls cannot silently remain active while their provenance identities disappear.
- Recheck current post-registry ownership transfers relevant to this root and update an existing provenance locator only when current Git proves that its canonical machine boundary moved and the change stays within this task.
- Preserve all existing classification semantics and runtime enforcement strength.

## Must not

- Do not change forward-only compatibility behavior, publication behavior, parent-action semantics, or lifecycle admission.
- Do not solve #1145's partial-control machine-segment representation root in this task.
- Do not create a second provenance registry, mirror database, heuristic control discovery framework, compatibility layer, or generic command/control framework.
- Do not infer machine enforcement from prose, naming, Issue metadata, or package existence alone.
- Do not reclassify existing controls merely to increase coverage.
- Do not weaken semantic-parent-only, partial, or external-unenforceable boundaries.

## Acceptance criteria

- `forward-only-compatibility` has a stable provenance identity backed by exact current owner, test, and postcondition/violation locators.
- The current publication-guard setup machine boundary has a distinct provenance entry if current source/test evidence confirms it is an independent machine control.
- Removing either proven required provenance identity causes repository provenance validation to fail closed without adding a second behavior registry.
- Existing generic locator validation continues to fail closed on owner/test/postcondition rename or deletion.
- Current execution-unit/milestone and other reviewed post-registry ownership transfers are either already represented by an existing current control family or receive the minimum evidence-backed locator correction; no speculative control IDs are added.
- Production behavior is unchanged.
- Focused provenance regressions, repository lint/harnesslint, required Go tests, PR CI, and merged-main required CI pass.

## Historical invariants

- Repository implementation authority is current Git -> `IMPLEMENTATION_RULES.md` -> `IMPLEMENTATION_PLAN.local.md` -> Plan-selected Task.
- Provenance is an index over production behavior, never a replacement behavior authority.
- Semantic judgment is not converted into machine enforcement by heuristic.

## Dependencies

none
