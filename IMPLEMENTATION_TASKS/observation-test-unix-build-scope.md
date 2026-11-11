# Task: Unix 専用 observation fixture の build scope

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1480#discussion_r4448267508

** Minor** · **Functional Correctness** · **Quick win**

**Restrict this test file to Unix platforms.**

Windows supports the `observationexec` package through `internal/execfile`, but this test uses Unix-only behavior. Windows does not interpret the `#!/bin/sh` shebang when `exec.Command(destination)` starts the copied file. `os.Symlink` can also require the symlink privilege and fail in Windows environments. Current CI uses Ubuntu only, but Windows developers can still run this package's tests.

<details>
<summary>Suggested fix</summary>
<dl>
<dd>

```diff
--- "a/glm-worker/internal/observationexec/gotest_copy_publish_test.go"
+++ "b/glm-worker/internal/observationexec/gotest_copy_publish_test.go"
@@ -1,4 +1,6 @@
+//go:build darwin || linux
+
 package observationexec
 
 import (
 	"errors"
```

</dd>
</dl>
</details>
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1480#discussion_r4448267508) — `glm-worker/internal/observationexec/gotest_copy_publish_test.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

gotest_copy_publish_testはshebang/symlinkを使うがWindowsでも選択される。

## External feasibility

status: not-applicable

## Contract

- 当該test fileを既存対応Unixのbuild constraintへ限定する。Windowsでのexecfile production behaviorは変更しない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- Darwin/Linuxで既存copy/cancel/nonreplacement testが選択され、WindowsではUnix fixtureが選択されない。build constraint位置・対応packageの既存platform分岐を保つ。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none

## Darwin acceptance

- `./internal/observationexec:TestModuleCopyPreservesExistingDestinationAndCleansTemporaryFile`
