# Task: 未対応 OS での diff capture build を閉じる

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1489#discussion_r4452300572

** Minor** · **Stability & Availability** · **Quick win**

**Add a `diffWorktreeObject` fallback for operating systems other than Linux, macOS and Windows.**

`diffWorktreeObject` is defined in only two places:
- This file, which has the constraint `//go:build linux || darwin`. Go does not treat the `_unix.go` filename suffix as a constraint, so the build tag alone decides where this file builds.
- `diff_fs_windows.go`, which builds only on Windows because of its `_windows` filename suffix.

On FreeBSD, OpenBSD, NetBSD, illumos and other operating systems, no file defines `diffWorktreeObject`. `collectDiffFile` in `projector.go` (Line 591) calls it, so the whole `parentevidence` package fails to compile there. Anything that imports the package also fails to build. Before this change, the package compiled on those platforms.

Add a stub that fails closed, like the Windows file does.

Based on learnings: "platform-specific implementations should use file name suffixes ... paired with a corresponding stub file ... providing no-op or fallback implementations."

<details>
<summary>🛠️ Proposed fallback file (new file `glm-worker/internal/parentevidence/diff_fs_other.go`)</summary>
<dl>
<dd>

```go
//go:build !linux && !darwin && !windows

package parentevidence

import "fmt"

func diffWorktreeObject(repoRoot, path string) (string, string, []byte, bool, error) {
	return "", "", nil, false, fmt.Errorf("secure diff evidence capture is unsupported on this platform for %q", path)
}
```
</dd>
</dl>
</details>

_Source: Learnings_
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1489#discussion_r4452300572) — `glm-worker/internal/parentevidence/diff_fs_unix.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

Linux/Darwin/Windows以外ではdiffWorktreeObjectが未定義になる。

## External feasibility

status: not-applicable

## Contract

- その他OS向けstubでsecure capture unsupportedを明示的に返し、既存Windows fail-closed方式と同じ責務にする。新たなsecure filesystem実装やOS support保証は追加しない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- FreeBSD等のその他OSで当該packageに定義欠落がなく、stubは成功を返さない。Linux/Darwin/Windowsの既存file選択を重複・欠落させない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
