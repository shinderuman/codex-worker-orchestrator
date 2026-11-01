# Task: 隔離入力copyのForkLock保有範囲

## Original instruction

````text
moduleTreeCopier.copyFileがio.Copyとsize境界read全体でForkLock.RLockを保有する問題を、exec保護を保ちつつsource I/Oへlockを掛けない形へ修正する。
````

## Amendments

none

## Resolved references

- CR-1431-1 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1431#pullrequestreview-5695099183（additional unverified: `glm-worker/internal/observationexec/gotest.go:269`）
  原文: "** Major · Stability & Availability · Heavy lift** ⚠️ **Unverified finding** **Keep blocking source reads outside `ForkLock`.** If the source filesystem stalls during `io.Copy` or the size-boundary read, `copyFile` holds `ForkLock.RLock` throughout the stall. Concurrent subprocess starts can then wait on this module copy. Preserve the executable-write protection without holding the global fork lock across source I/O. Go documents this lock as unsuitable for operations that can block indefinitely."

## Purpose

source停滞時に全subprocess起動がblockされることを防ぐ。

## External feasibility

status: not-applicable

## Contract

- `io.Copy`・境界readをForkLockの外へ出す。既存ETXTBSY回避の根拠（descriptorのclose-on-execとfork競合、実行可能fileの公開・close順）を確認して保護を保持する。O_EXCLがcreate後の全競合まで防ぐとは仮定しない。必要なlockはfile descriptor作成・設定の短い区間へ限定し、blocking source I/Oを含めない。

## Must not

- 一時file+rename等の別機構を、実行保護が同等に実証されないまま追加しない。

## Acceptance criteria

- copy中の並行subprocess起動がblockされないtest、およびcreate〜write境界のraceに関する既存testの維持。

## Historical invariants

none

## Dependencies

none
