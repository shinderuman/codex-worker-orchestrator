# Task: Repo-search semantic objective / lexical query separation evaluation

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

- 外部探索から成立した第二Findingは、Parallel Search等が検索のsemantic objectiveとcandidate取得用search query/keywordsを分離している点。current repository searchでは検索目的とlexical candidate queryが一つのquery representationへ収束しており、semantic questionから語へ落とす際の情報損失が追加search/readを生む可能性がある。
- 参考: https://parallel.ai/products/search
- 参考: https://exa.ai/blog/dynamic-highlights
- current repositoryでは `glm-worker/internal/reposearch/` とworker/reviewer navigation wiringがlexical query / scopeを扱う。実装開始時にcurrent Gitでquery seed生成、known target、path/symbol scope、reviewer diff-first navigation、parent standalone searchを再確認する。
- このFindingは `IMPLEMENTATION_TASKS/repo-search-result-context-budget.md` と独立する。前者は取得済みresultへの有限context配分、こちらはretrieval intent表現と追加search/readの削減可能性を扱う。

## Purpose

Semantic task/questionをlexical search queryへ一度に潰すことで、関連候補が取れても判断目的に十分なlocatorが得られず、追加search/readやquery再構築が発生していないかを評価する。Quality Deltaを維持したままCodex / Solのsearch/reconstruction turnを削減できる明確な境界が成立する場合だけ最小実装する。

## External feasibility

status: applicable

Parallel等のagent-oriented searchはobjectiveとsearch queryを別入力として扱う。外部serviceのsemantic retrieval実装をそのままrepositoryへ導入するのではなく、objectiveを保持することで既存deterministic navigationを改善できるかだけを検討する。

## Contract

- representative task / reviewer / parent searchで、semantic questionからlexical queryへの変換後に追加query、broad search、source rereadが生じる例をbounded evidenceで確認する。
- current query seed、path/symbol scope、diff-first navigation、known target skip、exhaustive proofとの役割差を明確にし、既存ownerで十分ならno-changeとする。
- objectiveを別representationとして保持する場合、それ自体をsource correctness authorityやsemantic classifierにしない。
- zero-model-callの既存情報だけでobjectiveとlexical queryを分離できる境界を優先する。追加LLM call、embedding、外部retrieval serviceを通常pathへ追加しない。
- objectiveを保持してもranking/selectionに安全に利用できない場合、model-facing locator説明やsearch refinementのbounded inputとしての価値を測定し、単なるschema追加にしない。
- 改善効果はquery数だけでなく、追加parent/model turn、model-visible bytes、search/read回数、Quality Deltaを区別して評価する。
- semantic task authorityはACTIVE task / current review question等の既存canonical ownerに残し、repo-search側へ第二task authorityを作らない。

## Must not

- semantic objectiveをregex / keyword heuristicで正解判定するmachine authorityへ昇格しない。
- embedding model、追加model call、外部search API、vector DB、generic semantic-search frameworkを導入しない。
- lexical BM25の弱点という一般論だけで実装を採用しない。
- objective fieldを追加するだけでCodex Reductionとみなさない。
- `repo-search-result-context-budget.md` のresult-set budget問題と同一実装へ混ぜない。
- quality-required exhaustive proofやexact source inspectionを省略しない。

## Acceptance criteria

- semantic objectiveがlexical queryへ失われることで追加search/readまたはparent/model re-entryが生じるcurrent evidenceを確認するか、成立しないことを明示してno-changeとする。
- 採用する場合、objectiveとlexical candidate queryの責務が明確で、canonical task/review authorityを複製しない。
- normal pathで追加model call / external serviceを増やさない。
- representative before/afterで追加query/read/model-visible outputまたはparent re-entryが減り、Quality Deltaが悪化していないことを確認する。
- reviewer independence、diff-first navigation、known target behavior、path/symbol scope、quality-required exhaustive proofを維持する。
- repository-owned validationとfocused regressionsが通る。

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。
- semantic judgmentをCodex / Solからarbitrary machine heuristicへ移さない。
- current schema / current ownerだけを正とし、旧shape維持のcompatibility layerを追加しない。

## Dependencies

none
