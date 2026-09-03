# Task: external review PR 344 intake

## Original instruction

````text
EXTERNAL_REVIEW_INTAKE

range: dd71330d452f6c384f19bc6740ac26e7ea126491..a70d35c1c5c4ac6aa0844c8d96f14b5af8975d82\
pr: https://github.com/shinderuman/codex-worker-orchestrator/pull/344\
branch: gpt-review/dd71330-a70d35c\
proposal_head: c7fa9591414c432a25fbbdb289956bbbd48b156c

review authority:

- END_SHAのIMPLEMENTATION_RULES.mdを基準にreview済み
- relevant task requirementはcurrent treeまたはGit履歴から確認済み

Codex:

- current repository authorityを再確認する
- review branchを明示的にfetchする
- proposal_headのlocal参照可能性を確認する
- 全findingとproposal diffをlosslessにGLMへ渡す
- 同じsubstantive reviewをGLM前に再実行しない

GLM:

- current HEAD / current Rules / relevant task contractに対してfinding成立性を検証する
- GPT proposalを検証し、必要ならcurrent HEADへ適応する
- 成立する問題を修正し必要なtestを実行する

fetchまたはproposal_head確認に失敗した場合はGLMへ進まずtransport failureとして停止する。

GPT branchをblind mergeしない。\
GLM処理後は既存のCodex最終review / acceptance flowへ戻る。
````

## Amendments

### 2026-09-03

````text
PRにCodeRabbitとGreptileのレビューコメントが付いてるから参照しておいて
````

## Resolved references

- PR: `https://github.com/shinderuman/codex-worker-orchestrator/pull/344`
- reviewed range: `dd71330d452f6c384f19bc6740ac26e7ea126491..a70d35c1c5c4ac6aa0844c8d96f14b5af8975d82`
- fetched remote ref: `refs/remotes/origin/gpt-review/dd71330-a70d35c`
- proposal head: `c7fa9591414c432a25fbbdb289956bbbd48b156c`
- 2026-09-03 intake時点でfetched remote refとproposal headは同一objectとしてlocal参照可能であることを確認済み
- CodeRabbit issue comment: `5523903881`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/344#issuecomment-5523903881`
- CodeRabbit completion comment: `5526129543`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/344#issuecomment-5526129543`
- Greptile summary comment: `5526189805`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/344#issuecomment-5526189805`
- Greptile inline review comment: `3924800004`、review `5102221692`、`glm-worker/internal/app/bundle_parent_usage.go:258`、`https://github.com/shinderuman/codex-worker-orchestrator/pull/344#discussion_r3924800004`

## Purpose

PR 344の外部review findingをcurrent repository authorityに対して検証し、成立する問題だけをcurrent HEADへ適応して修正する。

## External feasibility

status: verified

- explicit fetch成功
- proposal headのlocal参照成功
- reviewed range両端のlocal参照成功

## Contract

- GLMは下記Review findingsとProposal diffを欠落なく読み、current HEAD、current Rules、関連task contractに対してF1の成立性を検証する
- proposalを正解と仮定せず、current HEAD上の実装・既存test・関連surfaceとの整合性を確認する
- F1が成立する場合はcurrent HEADへ必要な修正とregression testを実装する
- CodeRabbitとGreptileの下記review内容を参照し、Greptileのpartial-anchor findingがproposal自身に対して成立するかを検証する
- external review本文がresidual riskとして挙げたshared bundle-analysis token-delta pathも確認し、同じcounter-reset contractが適用されるsurfaceかを根拠付きで判定する
- GLM処理後は独立reviewerとCodex最終review / acceptance flowへ戻す

## Must not

- GPT branchまたはproposal commitをblind merge、blind cherry-pickしない
- GLM検証前にCodexが同じsubstantive reviewを再実行しない
- counter resetを跨いだtoken deltaを推測加算しない
- finding不成立の場合にproposalを形式的に取り込まない
- parent-managed implementation metadataをGLMが編集しない

## Acceptance criteria

- F1の成立性と根拠がcurrent HEAD / current Rules / relevant task contractに対して報告される
- F1成立時はinterval途中でreset後に旧baselineを超えて回復するfixtureが`counter-reset`、zero deltas、baseline/end locator保持を検証する
- 累積fieldを欠くpartial intermediate anchorを挟んだreset/recovery caseについて、reset見逃しの有無を検証し、finding成立時はregression testと修正を行う
- shared bundle-analysis pathへの適用要否が検証され、必要な場合だけ整合する修正とtestを行う
- relevant Go tests、repository-wide quality gate、独立reviewer、current snapshot validationを完了する
- proposal由来の変更はcurrent HEADへ適応したdiffとしてCodexが最終採否する

## Historical invariants

- parent Codex usageはこのprojectの主要optimization metricであり、counter resetを跨ぐ推測deltaをavailableとして報告しない
- external findingの対策はescaped cause layerを確認し、指摘箇所だけでなくproduction causalityへ対応する

## Dependencies

none

## Review findings

PR bodyのsubstantive review内容を以下にlosslessで保持する。その後2026-09-03にCodeRabbitとGreptileのreviewが追加された。最新取得時点のbot review inventoryはCodeRabbit issue comment 2件、Greptile issue summary 1件、Greptile inline review comment 1件、Greptile review object 1件（bodyなし）。

````markdown
## Findings

### F1 — parent token reset is missed when the counter later exceeds the old baseline

`--parent-usage` selected only the baseline and final token anchors, then called the endpoint-only `analysisAnchorsCounterReset`. If a Codex cumulative counter drops inside the execution interval and subsequently grows past the pre-reset baseline before the final anchor, every endpoint value can be greater than the baseline. The report then returns `status=available` and a fabricated `end-baseline` delta instead of `counter-reset`.

This violates `parent-codex-token-attribution.md`'s Contract/Must-not requirement not to guess-sum across a counter reset. The impact is direct undercounting of parent Codex usage, which is the primary optimization metric for this project.

## Implemented correction

`bundle_parent_usage.go` now checks every token anchor between the selected baseline and end anchors and reports `counter-reset` as soon as an adjacent cumulative counter decreases. The existing endpoint case remains covered because the final anchor is included in the scan.

## Finding ↔ proposal mapping

- F1 → `glm-worker/internal/app/bundle_parent_usage.go`
- F1 regression → `glm-worker/internal/app/bundle_parent_usage_reset_test.go`

## Tests added/changed

Added `TestParentUsageCounterResetThenRecoveryStillReportsReset`. The fixture resets every cumulative token field inside task execution, then lets the final anchor exceed the original baseline. It asserts `counter-reset`, zero emitted deltas, and preserved baseline/end source locators.

## Tests executed

None from this review environment. No repository checkout with executable dependencies is available through the connected GitHub surface, so no test is recorded as PASS.

## Unexecuted validation / residual risk

- `go test ./glm-worker/internal/app/...` not executed here.
- repository-wide Go tests / vet / harnesslint not executed here.
- Existing bundle-analysis token-delta logic also uses endpoint-only reset detection; it predates the new `--parent-usage` surface. The proposal is intentionally scoped to the finding introduced by parent attribution, and Codex/GLM intake should verify whether the shared analysis path must be adapted at current HEAD to preserve cross-surface consistency.

## Parent-managed metadata findings left unmodified

None established. The proposal does not modify `IMPLEMENTATION_RULES.md`, `IMPLEMENTATION_PLAN.local.md`, `IMPLEMENTATION_TASKS/*.md`, or `IMPLEMENTATION_HISTORY.md`.

## Fixed-END warning

This proposal is authored against fixed END `a70d35c1c5c4ac6aa0844c8d96f14b5af8975d82`. Do not blind-merge it into a later `main`; revalidate the finding and adapt the patch against current repository authority first.
````

### CodeRabbit review

Comment `5523903881`はcommit range `a70d35c1c5c4ac6aa0844c8d96f14b5af8975d82..c7fa9591414c432a25fbbdb289956bbbd48b156c`を対象に、actionable commentなしと判定した。findingに関係する判定とpre-merge warningを以下に原文保持する。comment `5526129543`は`Review finished.`という完了通知で、追加findingを含まない。

````markdown
No actionable comments were generated in the recent review. 🎉

**Merge Risk:** _⚪ Minimal_ · up to `c7fa9`

Parent usage reporting now detects counter resets that occur between the baseline and end anchors even when counters later recover. The covered behavior preserves reset reporting and source locations, with no current merge-blocking risk identified.

### ❌ Failed checks (1 warning)

|     Check name     | Status     | Explanation                                                                                                                                                                               | Resolution                                                                         |
| :----------------: | :--------- | :---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | :--------------------------------------------------------------------------------- |
| Docstring Coverage | ⚠️ Warning | Docstring coverage is 0.00% which is insufficient. The required threshold is 80.00%. Docstring coverage is scoped to functions touched by this diff. Analyzed 4 functions across 2 files. | Write docstrings for the functions missing them to satisfy the coverage threshold. |
````

### Greptile review summary

Issue comment `5526189805`の本文を以下にlosslessで保持する。

````html
<h3>Greptile Summary</h3>

The PR expands parent-token reset detection from endpoint comparison to an adjacent-anchor scan and adds a regression test for counters that reset and later recover.
- Scans token anchors between the selected baseline and end.
- Returns `counter-reset` when an adjacent cumulative counter decreases.
- Tests recovery above the original baseline after a reset.

<h3>Confidence Score: 4/5</h3>

The PR is not yet safe to merge because partial intermediate token anchors can still conceal a counter reset and produce incorrect parent-usage attribution.

The scan replaces its comparison anchor even when cumulative fields are omitted, while reset detection skips missing field pairs; a later lower value can therefore bypass reset classification and be emitted as available usage.

**Files Needing Attention:** glm-worker/internal/app/bundle_parent_usage.go

<details><summary><h3>Important Files Changed</h3></summary>




| Filename | Overview |
|----------|----------|
| glm-worker/internal/app/bundle_parent_usage.go | Adds intermediate-anchor reset detection, but the previously reported partial-anchor gap remains outstanding. |
| glm-worker/internal/app/bundle_parent_usage_reset_test.go | Covers a full-field reset followed by recovery above the baseline. |

</details>


<!-- greptile_other_comments_section -->

<sub>Reviews (2): Last reviewed commit: ["test: cover recovered parent token count..."](https://github.com/shinderuman/codex-worker-orchestrator/commit/c7fa9591414c432a25fbbdb289956bbbd48b156c) | [Re-trigger Greptile](https://app.greptile.com/api/retrigger?id=59950197)</sub>
````

### Greptile inline finding

Review comment `3924800004`、proposal commit `c7fa9591414c432a25fbbdb289956bbbd48b156c`、`glm-worker/internal/app/bundle_parent_usage.go:258`の本文を以下にlosslessで保持する。

````html
<a href="#"><img alt="P1" src="https://greptile-static-assets.s3.amazonaws.com/badges/p1.svg?v=9" align="top"></a> **Partial anchors hide resets**

When a cumulative token field is omitted from an intermediate anchor and next appears below its last known value, assigning that partial anchor to `previous` severs the comparison across the omission. A later recovery above the original baseline is then reported as an available delta instead of `counter-reset`, producing incorrect parent-usage attribution.
````

## Proposal diff

Base `a70d35c1c5c4ac6aa0844c8d96f14b5af8975d82`、head `c7fa9591414c432a25fbbdb289956bbbd48b156c`のproposal diff全文を以下にlosslessで保持する。

````diff
diff --git a/glm-worker/internal/app/bundle_parent_usage.go b/glm-worker/internal/app/bundle_parent_usage.go
index ff53a0e..0e62e7d 100644
--- a/glm-worker/internal/app/bundle_parent_usage.go
+++ b/glm-worker/internal/app/bundle_parent_usage.go
@@ -220,7 +220,7 @@ func parentUsageAnchoredTokens(scan bundleRolloutScan, baselineBound, endBound t
 		return parentUsageTokens{Status: analysisStatusMissing, Reason: parentUsageReasonEndAnchor, BaselineAt: baseline.RawAt, BaselineSource: parentUsageSourceLocator(source, baseline.Line)}
 	case end.Offset <= baseline.Offset:
 		return parentUsageTokens{Status: analysisStatusNoObservation, BaselineAt: baseline.RawAt, BaselineSource: parentUsageSourceLocator(source, baseline.Line)}
-	case analysisAnchorsCounterReset(baseline, end):
+	case parentUsageCountersResetBetween(scan, baseline, end):
 		return parentUsageTokens{
 			Status:         analysisStatusCounterReset,
 			BaselineAt:     baseline.RawAt,
@@ -244,6 +244,23 @@ func parentUsageAnchoredTokens(scan bundleRolloutScan, baselineBound, endBound t
 	return tokens
 }
 
+func parentUsageCountersResetBetween(scan bundleRolloutScan, baseline, end analysisRolloutTokenAnchor) bool {
+	previous := baseline
+	for _, anchor := range scan.tokens {
+		if anchor.Offset <= baseline.Offset {
+			continue
+		}
+		if anchor.Offset > end.Offset {
+			break
+		}
+		if analysisAnchorsCounterReset(previous, anchor) {
+			return true
+		}
+		previous = anchor
+	}
+	return false
+}
+
 func parentUsageCounterField(field string, baseline, end *int64, source string, baselineAnchor, endAnchor analysisRolloutTokenAnchor, unknowns []parentUsageUnknownField) (int64, []parentUsageUnknownField) {
 	delta := analysisCounterDeltaState(baseline, end)
 	if delta.Known {
diff --git a/glm-worker/internal/app/bundle_parent_usage_reset_test.go b/glm-worker/internal/app/bundle_parent_usage_reset_test.go
new file mode 100644
index 0000000..fbd85aa
--- /dev/null
+++ b/glm-worker/internal/app/bundle_parent_usage_reset_test.go
@@ -0,0 +1,33 @@
+package app
+
+import (
+	"testing"
+	"time"
+)
+
+func TestParentUsageCounterResetThenRecoveryStillReportsReset(t *testing.T) {
+	task := newAnalysisTerminalTask(t)
+	lines := []string{
+		parentUsageTokenCountLine(t, task.start.Add(-time.Minute), 1000, 500, 240, 160, 1500),
+		analysisTurnLine(t, task.start.Add(-30*time.Second), codexRolloutTaskStartedType, analysisOwningTurnID),
+		parentUsageTokenCountLine(t, task.start.Add(10*time.Minute), 100, 50, 20, 10, 180),
+		parentUsageTokenCountLine(t, task.completeAt.Add(-time.Second), 2000, 1000, 480, 320, 3000),
+		analysisTurnLine(t, task.completeAt.Add(2*time.Minute), codexRolloutTaskCompleteType, analysisOwningTurnID),
+	}
+	writeAnalysisRollout(t, task.codexHome, analysisRolloutRel(), codexTestParentThreadID,
+		task.start.Add(-3*time.Hour), lines)
+
+	report := runParentUsageReport(t, task.cfg)
+	tokens := report.Intervals.TaskExecution.Tokens
+	if tokens.Status != analysisStatusCounterReset {
+		t.Fatalf("execution tokens = %#v", tokens)
+	}
+	if tokens.InputTokens != 0 || tokens.CachedInputTokens != 0 || tokens.OutputTokens != 0 ||
+		tokens.ReasoningTokens != 0 || tokens.TotalTokens != 0 {
+		t.Fatalf("counter reset carries token deltas: %#v", tokens)
+	}
+	if tokens.BaselineSource != parentUsageSourceLocator(analysisRolloutRel(), 2) ||
+		tokens.EndSource != parentUsageSourceLocator(analysisRolloutRel(), 5) {
+		t.Fatalf("counter reset locators = %#v", tokens)
+	}
+}
````

## Current boundary

`telemetry-timeline-retention-fallback.md`完了後に実行する。現在のin-flight処理は再起動・置換・混在させない。
