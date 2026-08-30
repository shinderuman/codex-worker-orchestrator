# Task: reviewer repo-searchのimpact termsから親管理metadataを除外する

## Original instruction

````text
Codexの節約、Codexの判断、GLMの判断、無駄な行動等多角的な問題をひたすら洗え
````

## Amendments

````text
実装を開始する前に、起票したIssueにCodexにやらせられるタスクがあったらCodexにまわしてくれ
````

## Resolved references

- reviewer diff-first searchは`reviewerImpactPaths()`でPlan、History、task file、parent rules等のparent-managed pathをreview impact対象から除外する既存設計を持つ。
- 一方`collectReviewerDiffImpactTerms()`はbaselineからのfull diff本文をpath filterなしでtoken化する。
- completed commentlint dogfoodではtask途中の親管理Plan/task metadata変更がbaseline diffへ入り、その語彙がbounded independent-search query termsへ逆流した。
- これはreviewer independenceを強める情報ではなく、既存のparent-managed path除外がterms側だけ片落ちしているproduction mismatchである。

## External feasibility

status: not-applicable

## Purpose

reviewer independent repo-searchのimpact queryを実装変更に集中させ、parent-managed metadataの語彙がterm budgetとsearch結果を汚染するのを防ぐ。

## Contract

- existing `isParentManagedReviewPath`等の単一predicateを再利用し、parent-managed pathの別listを作らない。
- baseline diffからimpact termを抽出する前にfile boundaryを認識し、parent-managed pathのhunkをterm sourceから除外する。
- changed-path evidence自体はtruthfulに維持し、parent/user変更が存在した事実を消さない。
- reviewer diff-first、independent search、result cap、telemetry、exhaustive proofのauthorityを変更しない。
- ordinary implementation docs/testsをparent-managed扱いへ拡大しない。
- model callを追加しない。

## Must not

- query結果を良く見せるためparent-managed pathをcanonical task diffから削除しない。
- reviewerへworker search結果のauthorityを与えない。
- natural-language keyword blacklistでPlan語彙を落とさない。
- changed path全体を隠してreview scopeを狭めない。

## Acceptance criteria

- mixed baseline diffにPlan/History/task metadataとimplementation codeを含めるregressionで、reviewer impact termsはreview-relevant implementation path由来だけになる。
- parent-managed語彙がbounded term limitを消費しない。
- `reviewerImpactPaths()`とterm extractionのparent-managed受理集合がdriftしないtestを持つ。
- repo-search telemetry/outcomeとindependent reviewer semanticsを維持する。
- full validationと独立reviewを通す。

## Historical invariants

- parent-managed Plan/History/taskはworker/reviewerのimplementation authorityではない。
- reviewer independenceとdiff-first searchは維持する。
- repo-searchはnavigation-onlyである。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。commentlint dogfoodで実再現したbounded search-query汚染だけを修正する。