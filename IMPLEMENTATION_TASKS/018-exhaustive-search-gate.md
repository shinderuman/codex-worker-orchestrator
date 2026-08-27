# Task: exhaustive search gate

## Original instruction

````text
## Task 018: exhaustive search gate

「exhaustive確認」が要求されたcaseではBM25 top-Nだけで完了扱いしない。

worker query/resultをreviewerが独立検証する。
````

## Amendments

- 2026-08-22 parent maintenance:

````text
## 8. Exhaustive searchは「独立BM25をもう一度やる」では成立しない

Task 018の重要contractをさらに明確化してください。

`exhaustive`を要求されたcaseでは、

workerとreviewerが独立してBM25 top-Nを実行してもexhaustive proofにはなりません。

BM25はrankingです。

### Contract

exhaustive確認では、

* full corpus enumeration
* deterministic exact/semantic predicate
* 全候補走査
* または網羅性を説明できる別のdeterministic mechanism

を使ってください。

BM25は、

* query seed
* candidate ordering
* initial navigation

には使用して構いませんが、top-N hitだけをexhaustive evidenceにしないでください。

reviewerの独立性も、

> workerとは別のtop-Nを見た

だけではなく、

> exhaustive criterionを満たすfull corpus確認を独立検証した

ことを要求してください。
````

## Purpose

ranking上位だけで網羅性を誤認するfalse-completeを防ぐ。

## Contract

- exhaustive requirementを通常searchから区別
- full corpus enumerationとdeterministic exact/semantic predicateによる全候補走査、または網羅性を説明できる別のdeterministic mechanismを使う
- reviewerはworkerと別のtop-Nを見るだけでなく、exhaustive criterionを満たすfull corpus確認を独立検証する

## Must not

- BM25 top-N取得や独立rankingの反復だけでexhaustive表示しない

## Acceptance criteria

- positive/negative exhaustive scenario、full-corpus criterion、production wiring
- test、独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- BM25 corpus境界とfingerprint統一
- 016 worker repo-search integrationはPR #22のSquash Merge commit `da468541683a832b102acecc60770678452e6fa4`で充足済み。BM25はnavigation/ranking用途でありexhaustive proofにはしない
- F9 reviewer diff-first independent searchはPR #19のSquash Merge commit `948ea31ee4381f4192afeff607c015696349a9a1`で充足済み。reviewer独立searchはnavigationでありexhaustive proofにはしない

## Dependencies

none

## Review findings

none

## Current boundary

COMPLETE / CAMPAIGN_STOP_SENTINEL。F10実装・独立review・恒久Repository Lint `33126476876`・runtime completion `33126575073`は完了済み。ユーザー指定によりPR #20完了後は通常Plan taskを開始しない。current plancheckがACTIVE 1件を必須とするため本taskをsentinelとして一時保持するが、再dispatch・追加実装は禁止。010以降はNEXTのまま維持する。
