# Task: current-corpus 評価で repository 不在を区別

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1540#discussion_r4525627012

** Minor** · **Functional Correctness** · **Quick win**

**Skip the test when the working directory is not inside a git repository.**

`git rev-parse --show-toplevel` fails when the source tree has no `.git` directory. This can happen with an exported source archive or a vendored build. The test then fails with `t.Fatalf` instead of skipping. The `LookPath` check already skips for a missing `git` binary. Apply the same handling to a missing repository.

<details>
<summary>Proposed fix</summary>
<dl>
<dd>
<br>

```diff
--- "a/glm-worker/internal/workflow/repo_search_context_budget_eval_test.go"
+++ "b/glm-worker/internal/workflow/repo_search_context_budget_eval_test.go"
@@ -13,10 +13,10 @@
 	if _, err := exec.LookPath("git"); err != nil {
 		t.Skipf("git is unavailable: %v", err)
 	}
 	rootOutput, err := exec.Command("git", "rev-parse", "--show-toplevel").Output()
 	if err != nil {
-		t.Fatalf("resolve repository root: %v", err)
+		t.Skipf("not inside a git repository: %v", err)
 	}
 	root := strings.TrimSpace(string(rootOutput))
 	report, err := reposearch.Search(context.Background(), root, "cache", reposearch.Options{
 		DisableCache: true,
```
</dd>
</dl>
</details>
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1540#discussion_r4525627012) — `glm-worker/internal/workflow/repo_search_context_budget_eval_test.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

source archiveでgit rev-parseが失敗すると代表corpus testが無条件FAILする。

## External feasibility

status: not-applicable

## Contract

- repositoryが存在しない場合だけcurrent-corpus評価をskipし、git自体の異常や存在するrepositoryの検索失敗を隠さない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- .gitなしのsource archiveで理由付きskipし、実repositoryではsearch/locator/snippet予算assertを実行する。既存repository内の権限破損・検索失敗を一律skipしない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
