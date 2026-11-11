# Task: same-HEAD source の review equivalence を事前拒否

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1552#discussion_r4532767315

** Major** · **Stability & Availability** · **Quick win**

**Reject a same-HEAD source before reentry.**

A parent-source reentry can occur without a HEAD advance. If the index and worktree also match the accepted HIGH review, equivalence preparation succeeds, but provenance validation rejects the equal HEADs. Materialization then fails instead of starting a fresh review.

Return an error for this case so the controller uses ordinary reentry.

<details>
<summary>🐛 Proposed fix</summary>
<dl>
<dd>
<br>

```diff
--- "a/glm-worker/internal/workflow/review_source_equivalence.go"
+++ "b/glm-worker/internal/workflow/review_source_equivalence.go"
@@ -30,12 +30,9 @@
 		current.Head == "" || current.WorktreeDigest != original.WorktreeDigest || len(targets) == 0 {
 		return state.SnapshotDigest{}, fmt.Errorf("review source cannot prove unchanged working-tree and review identity")
 	}
 	if original.Head == current.Head {
-		if original.IndexDigest != current.IndexDigest {
-			return state.SnapshotDigest{}, fmt.Errorf("review source index changed without a committed advancement")
-		}
-		return current, packet.ValidateReviewTargetProof(repo, targets)
+		return state.SnapshotDigest{}, fmt.Errorf("review source has no committed advancement to prove equivalent")
 	}
 	if err := validateEquivalentReviewAdvancement(repo, original, current, targets); err != nil {
 		return state.SnapshotDigest{}, err
 	}
```
</dd>
</dl>
</details>
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1552#discussion_r4532767315) — `glm-worker/internal/workflow/review_source_equivalence.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

同一HEADのequivalence準備は成功するがprovenance検証が後で拒否する。

## External feasibility

status: not-applicable

## Contract

- committed advancementのないsourceはequivalence候補にせず通常reentry/reviewへ戻す。後段provenance検証を緩めない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- HEAD/index/worktreeすべて同一のreentryがmaterialization errorで停止せずfresh reviewへ進む。許容済みmetadata-only fast-forwardは維持し、source/target改変と不正provenanceを拒否する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
