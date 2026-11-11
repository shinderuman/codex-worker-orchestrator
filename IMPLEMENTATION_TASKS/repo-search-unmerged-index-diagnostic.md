# Task: repo-search の unmerged index 診断を区別

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1490#discussion_r4452808534

** Minor** · **Functional Correctness** · **Quick win**

**Report unmerged index entries separately from unsupported index flags.**

Git emits `M` for unmerged stage 1–3 entries. The current parser reports those entries as `ErrUnsupportedIndexFlags`, which gives the operator incorrect remediation guidance.

<details>
<summary>Suggested fix</summary>
<dl>
<dd>

```diff
--- "a/glm-worker/internal/reposearch/enumerate.go"
+++ "b/glm-worker/internal/reposearch/enumerate.go"
@@ -113,9 +113,13 @@
 	if (mode != trackedModeRegular && mode != trackedModeExecutable) || excludedPath(path, excludeDirs) {
 		return trackedEntry{}, false, nil
 	}
-	if raw[0] != 'H' {
+	switch raw[0] {
+	case 'H':
+	case 'M':
+		return trackedEntry{}, false, fmt.Errorf("repo-searchは未解決のmerge conflict entryを扱えません: %s", path)
+	default:
 		return trackedEntry{}, false, fmt.Errorf("%w: %s (index flag %c)", ErrUnsupportedIndexFlags, path, raw[0])
 	}
 	return trackedEntry{mode: mode, sha: sha, path: path}, true, nil
 }
 
```

</dd>
</dl>
</details>
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1490#discussion_r4452808534) — `glm-worker/internal/reposearch/enumerate.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

Git M stage 1-3をunsupported index flagと誤報告する。

## External feasibility

status: not-applicable

## Contract

- unmerged entryをindex flagsと区別するエラーとして扱い、search/exhaustiveの両ownerで同じ診断へ収束する。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 実conflict indexの各stageでmerge conflictと診断し、skip-worktree/assume-unchangedは既存ErrUnsupportedIndexFlagsのまま拒否する。cache有無でも混同せず、unsupported入力を黙ってskipしない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
