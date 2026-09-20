# Task: remove stale Z.ai auto-resume-plan guidance

## Original instruction

````text
IMPLEMENT_DO finding: Z.ai provider self-resume migration removed `glm-worker --auto-resume-plan`, but `glm-worker/internal/state/parent_action.go` still tells an early manual `--resume` caller to "reserve the wake with glm-worker --auto-resume-plan". Remove that stale guidance and keep the canonical machine-owned 5h recovery contract explicit.
````

## Amendments

none

## Resolved references

- implementation PR #961 removed the old GLM provider scheduler/fallback command family and made 5h recovery machine-owned
- `glm-worker/internal/app/command_test.go` already negative-tests that `--auto-resume-plan` is absent from the command registry
- `glm-worker/internal/state/parent_action.go` still emits the removed command name when explicit resume is attempted before the reset boundary
- explicit resume after the reset boundary remains a supported recovery path when the waiting process is no longer alive

## Purpose

Remove an escaped stale recovery instruction so parent/user-facing admission errors describe only the canonical machine-owned 5h recovery and valid explicit resume behavior.

## External feasibility

status: not-applicable

## Contract

- keep early explicit resume fail-closed before the recorded Z.ai 5h reset boundary
- do not mention or restore `glm-worker --auto-resume-plan` or any retired scheduler/fallback mode
- explain that automatic 5h recovery is machine-owned and that explicit `--resume` becomes admissible after the reset boundary
- preserve durable rate-limited state and existing resume admission semantics
- add regression coverage that the early-resume error cannot reintroduce the retired command name

## Must not

- reintroduce a scheduler choice or parent wake orchestration for GLM provider 5h recovery
- weaken reset-boundary validation
- alter Codex wake scheduling behavior
- broaden this escaped-defect fix into unrelated provider recovery changes

## Acceptance criteria

- an explicit resume attempt before reset is rejected with canonical guidance and no `--auto-resume-plan` reference
- the rate-limited checkpoint/state remains intact after rejection
- command registry negative regression for retired GLM provider modes remains PASS
- relevant Go tests and full Repository Lint PASS

## Historical invariants

- 5h provider recovery is machine-owned after #961
- explicit user stop/resume remains compatible with durable state
- retired GLM provider scheduler/fallback commands are not canonical recovery surfaces

## Dependencies

none
