# Task: Repo-search result-set context budget / dedup evaluation

## Original instruction

````text
https://note.com/npaka/n/n1d86b2196515

ここもしくはこの関連サービスからCodex Reductionに貢献できるFindingがあったら教えてほしい
````

````text
じゃあこれをIMPLEMENTATION_PLANに追加してくれ
Issueではない
System oneのタスクを除いたタスクで優先度をお前が決めて差し入れてくれ
````

## Amendments

none

## Resolved references

- 外部探索から成立した主Findingは、Exa Dynamic Highlights等に見られる「documentごとの固定excerptではなく、retrieval結果集合全体へ有限のmodel-visible token/context budgetを配分し、重複・低価値resultには少量または0を割り当てる」方式をrepository searchへ適用できる可能性。
- 参考: https://exa.ai/blog/dynamic-highlights
- 参考: https://parallel.ai/products/search
- current repositoryでは `glm-worker/internal/reposearch/` がranked resultとsnippetを生成し、automatic worker/reviewer navigationとstandalone parent repo-searchが利用する。実装開始時にcurrent Gitでowner / caller / output contractを再確認する。
- historical #742 はautomatic BM25 navigation経路そのもののKEEP / REMOVE / SIMPLIFY価値測定を所有したが、result-set全体のmodel-visible budget allocationは対象外。#313はstructured machine evidenceのbounded projectionであり、raw source/navigation snippet allocationとは別境界。

## Purpose

Repo-searchが返すmodel-visible source surfaceを、result件数ごとの固定的なsnippet配分ではなく、検索全体の有限budgetへ局所化することでCodex / Sol context流入を削減できるかをQuality Deltaと同時に評価し、成立する場合だけ最小実装する。

## External feasibility

status: applicable

Exa / Parallel等の外部search serviceは、agent向けretrievalでdocument単位ではなくretrieval全体の情報密度・budgetを最適化する設計を提供している。ただし外部サービスの削減率をこのrepositoryへ外挿しない。

## Contract

- 実装開始時にcurrent repo-searchのworker / reviewer / parent consumer、ranking、snippet/output budget、refinement、telemetry/eval ownerを再確認する。
- まずrepresentativeなcurrent repo-search出力について、result間の重複、低価値snippet、model-visible bytes、後続の追加search/read、Quality Deltaに関係するmissをbounded evidenceで測定する。
- 改善を採用する場合、1回のretrievalに対する明示的なmodel-visible budgetをcanonical ownerで持ち、ranked result間へ配分する。高価値resultへより多く、重複・低価値resultへ少量または0を割り当てられること。
- budget配分はsource correctnessのauthorityにしない。repo-search結果は引き続きnavigation locatorであり、semantic conclusionに必要なexact source inspectionやquality-required exhaustive proofを置換しない。
- duplicate suppression / allocationはzero-model-callのdeterministic処理を優先する。semantic relevanceを新しいLLM callや恣意的classifierへ移さない。
- current standalone parent repo-searchのscope / lease / duplicate-delivery / refinement contractと、worker/reviewer navigationのindependenceを維持する。
- before/afterは単純なsnippet文字数だけでなく、representative taskでのmodel-visible search bytes、追加search/read回数、Codex/Sol usageまたは利用可能なproxy、Quality Deltaを区別して評価する。
- evidenceが不足する場合は未測定の削減率を主張せず、MEASURE-FIRST / no-change dispositionを許容する。

## Must not

- #742のautomatic repo-navigation KEEP / REMOVE / SIMPLIFY判断を測定なしに再開しない。
- #313のstructured evidence projectionを第二実装として複製しない。
- Top-N件を機械的に短くするだけでCodex Reduction成立とみなさない。
- relevant sourceを失わせるsilent truncation、false proof、exhaustive-search弱体化を行わない。
- retrieval budgetのために追加model call、embedding service、外部search依存、generic retrieval frameworkを導入しない。
- external serviceの公開ベンチマーク削減率をrepository固有の削減率として扱わない。
- raw LOC / snippet lengthだけをQuality Deltaの代替指標にしない。

## Acceptance criteria

- current repo-searchのmodel-visible outputについてresult-set単位のbudget不足が実データで成立するかを測定し、重複・低価値surfaceと追加search/readへの影響を記録する。
- 採用する場合、1 retrieval全体のbounded budgetがあり、resultごとの固定配分よりdecision-relevant sourceへbudgetを集中できる。
- same/near-duplicate resultがmodel-visible budgetを不必要に重複消費しない。
- 必要なsource locator、exact follow-up read、reviewer independence、quality-required exhaustive proof、parent-evidence read-scope/lease/refinement safetyを維持する。
- zero extra model callsで成立する。
- representative before/afterでmodel-visible search bytesまたは同等proxyが減り、Quality Delta悪化や追加search/readの増加で相殺されていないことを確認する。成立しない場合はno-changeで終了できる。
- repository-owned validationとfocused regressionsが通る。

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。
- repo-search resultはnavigation locatorでありsource-code proofではない。
- current schema / current ownerだけを正とし、旧shape維持のcompatibility layerを追加しない。

## Dependencies

none
