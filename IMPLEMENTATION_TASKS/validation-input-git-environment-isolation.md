# Task: validation 入力検査の Git location 環境隔離

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607625

** Minor** · **Stability & Availability** · **Quick win**

**Strip inherited Git-location variables from the Git calls.**

When `repository` is outside the caller's worktree, the direct Git commands can use inherited `GIT_DIR`, `GIT_WORK_TREE`, or `GIT_INDEX_FILE` values. Route all three calls through `gitexec.Command`. Apply the same filtering to the test helper.

<details>
<summary>Suggested fix</summary>
<dl>
<dd>

```diff
 import (
 	...
+	"github.com/shinderuman/codex-worker-orchestrator/glm-worker/internal/gitexec"
 	"github.com/shinderuman/codex-worker-orchestrator/glm-worker-orchestrator/glm-worker/internal/state"
 )

-	data, err := exec.Command("git", "-C", repository, "ls-files", "--others", "--ignored", "--exclude-standard", "-z").Output()
+	data, err := gitexec.Command("git", repository, "ls-files", "--others", "--ignored", "--exclude-standard", "-z").Output()

-	listing, err := exec.Command("git", "-C", repository, "ls-files", "--others", "--exclude-standard", "-z").Output()
+	listing, err := gitexec.Command("git", repository, "ls-files", "--others", "--exclude-standard", "-z").Output()

-	command := exec.Command("git", "-C", repository, "check-ignore", "-z", "--stdin")
+	command := gitexec.Command("git", repository, "check-ignore", "-z", "--stdin")
```

In `run_record_toolchain_test.go`, set the helper command's environment with `gitexec.WithoutLocalEnvironment(os.Environ())` before running it.

</dd>
</dl>
</details>

_Source: Learnings_
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607625) — `glm-worker/internal/qualitygate/run_record.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

NoIgnoredInputsの3 Git呼出がambient GIT_DIR/WORK_TREE/INDEX_FILEを継承する。

## External feasibility

status: not-applicable

## Contract

- 既存gitexec.Command/WithoutLocalEnvironmentをNoIgnoredInputsとnested検査・関連fixtureへ適用し、指定repositoryの入力だけを見る。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 別repositoryのGit location envを汚染しても対象のignored/nested入力を正しく判定する。caller env/別repositoryを変更せず、失敗をno-ignored成功へ読み替えない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
