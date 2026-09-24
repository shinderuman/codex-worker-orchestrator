# Task: Partial-control machine-segment provenance

## Original instruction

```text
今回の仕事は、#1136配下の open `campaign:refactor` Issueをcurrent stateから取得し、1件ずつ直列に実装してterminalにすること。

tracked implementation authorityが存在しない場合は、repositoryの既存workflowに従って必要なTask / Plan authorityをparent側で確立してからproduction実装へ進む。
```

## Amendments

none

## Resolved references

- Current adopted management item is GitHub Issue #1145, `[REFACTOR][1137] Preserve machine-segment provenance for partial controls`, selected after #1146 became terminal on main `f9cda373dcbeca3e3e6b37d4a1dd6e016af08942`.
- Current `partial` controls are `codex-auto-resume-automation-transaction`, `execution-permission-convergence`, `failure-artifact-confidentiality`, `parent-plan-continuation`, `runtime-install-completion`, `session-rotation-fail-proof`, `sol-review-evidence-before-accept`, and `user-global-instruction-config-ownership`.
- Existing `machine_owners`, `tests`, `postconditions`, and `projection_guards` schema fields are preferred for the machine-owned segment if they can represent the current boundary without changing classification semantics; do not add a second registry or nested framework merely for naming symmetry.

## Purpose

Make every current `partial` provenance entry traceable to its actual repository-owned machine segment while preserving the residual semantic/external boundary and existing negative-result policy.

## External feasibility

status: not-applicable

## Contract

- Preserve `partial` as a mixed-boundary classification; machine-segment provenance must not imply full machine enforcement.
- Revalidate every current partial entry against current production behavior.
- For each partial control with a concrete machine segment, record exact production owner, focused test, and postcondition/admission locators in the canonical provenance registry.
- If a current partial entry lacks an actual machine-owned segment, use current evidence to reclassify it rather than inventing locators.
- Extend provenance validation only as needed so partial machine-segment locator drift fails closed while fully machine-enforced validation retains its existing meaning.
- Keep semantic-parent-only and external-unenforceable controls free of fictitious machine provenance.
- Preserve current runtime behavior, gate strength, external write authority, semantic judgment, and negative-result promotion behavior.

## Must not

- Do not promote a partial control to fully machine-enforced merely because it has machine-owned sub-behavior.
- Do not add behavioral guards, alter runtime-install or session-rotation semantics, or change external automation/user authority.
- Do not create a second provenance registry, generic policy framework, compatibility layer, or arbitrary nested-control abstraction.
- Do not make partial controls eligible for compact fully-machine `control:<id>` projection unless separately proven by another authority.

## Acceptance criteria

- Every current partial entry has an evidence-backed disposition: exact machine-segment provenance plus residual boundary, or evidence-backed reclassification.
- `runtime-install-completion` has current production owner, focused tests, and postcondition/evidence locators.
- Renaming/removing any recorded partial machine owner/test/postcondition causes provenance validation to fail closed.
- Residual semantic/external authority remains explicit and `NegativeResultPolicyFor(partial)` remains parent-residual.
- Existing fully machine-enforced locator/projection validation continues to work unchanged in meaning.
- Semantic-parent-only and external-unenforceable entries do not gain machine locators.
- Repository lint, focused provenance tests, full Go tests, PR CI, and merged-main required CI pass.

## Historical invariants

- `codex/control-provenance.json` remains the single provenance/index surface; production behavior remains authoritative.
- Ordinary task completion deletes this Task and restores the preempted Plan state; completion evidence remains in Git/CI rather than History.

## Dependencies

none
