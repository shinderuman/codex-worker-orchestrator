# Task: quality gate記録の補完・資源計測の正確性

## Original instruction

````text
Darwin補完実行の早期失敗時のNativeSupplement終了帰属と、darwinSysctlInt/darwinSysctlUint64の実値拒否を修正する。
````

## Amendments

none

## Resolved references

- CR-1401-1 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1401#discussion_r4381578230 · `glm-worker/internal/app/quality_gate_backend.go:78-82`付近
  原文: "**Finalize `NativeSupplement` before the early return on toolchain mismatch.** Line 79 attaches `n` to `record.Execution.NativeSupplement` with `Status` set to `StatusRunning`. The check at Line 80 can then return an error before the command starts. In that case, nothing updates `n.Status`, `n.ExitCode`, or `n.ExitSource`. The caller already has a non-nil `runErr`. `verifyCompletedQualityExecution` returns early in that case. `completeQualityGateRun` therefore persists a failed run whose supplement still reports `running` with no exit attribution. Consumers of the run record and exported validation evidence can misread this as an unfinished supplement. Set the failure fields before returning."
- CR-1409-2 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1409#discussion_r4397725527 · `glm-worker/internal/qualitygate/resource_samples_darwin.go:309-321`付近
  原文: "**Fix `darwinSysctlInt` and `darwinSysctlUint64`. The length checks reject real sysctl values.** `syscall.Sysctl` removes one trailing NUL byte from the returned string. For `hw.ncpu` (4-byte value, e.g. 8), the string has length 3. For `hw.memsize` (8-byte value, e.g. 16 GiB), the string has length 7. Both functions require the full length and return 0. Both calls at lines 247 and 280 therefore always use the `hostinfo` and `memory_pressure` fallbacks. If those commands are blocked or fail, `LogicalCPUs` and `TotalMemoryBytes` stay 0 with no disclosure. The tests pass only because they build full-length inputs, which the real API does not return. Accept a string that is one byte short and zero-pad it before decoding. Update `TestDarwinSysctlDecoding` to use the stripped form."

## Purpose

gate記録とresource sampleが失敗・実値を正しく表現する。

## External feasibility

status: not-applicable

## Contract

- toolchain不一致return前に`n.Status/ExitCode/ExitSource`をfail確定する。
- `darwinSysctlInt`/`darwinSysctlUint64`はNUL除去後の1 byte短い値をzero-padしてdecodeし、それ以外の長さは拒否する。`TestDarwinSysctlDecoding`を実際の`syscall.Sysctl`が返す形式へ更新する。

## Must not

- fallback経路（`hostinfo`/`memory_pressure`）とその失敗時の0報告規約を削除しない。

## Acceptance criteria

- 早期return時のsupplement fail attribution testと、3/7 byte形式のdecode test。

## Historical invariants

none

## Dependencies

none
