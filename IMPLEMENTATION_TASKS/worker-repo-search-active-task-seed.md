# Task: worker repo-searchをACTIVE task authorityからseedする

## Original instruction

````text
Codexの節約、Codexの判断、GLMの判断、無駄な行動等多角的な問題をひたすら洗え
````

## Amendments

````text
実装を開始する前に、起票したIssueにCodexにやらせられるタスクがあったらCodexにまわしてくれ
次のDogfooding用のタスクが残り1個しかない
なので今後のタスクも必要だからだ
````

## Resolved references

- Plan管理taskの新規開始は`glm-parent-action start`を使い、USER_REQUESTは意図的に固定文`現在のACTIVE taskを実行してください。`へ縮めている。
- worker promptには別途ACTIVE task path/contextが渡され、task fileが要求authorityである。
- current `newWorkerTaskPrompt()`はBM25 worker repo-searchだけをraw USER_REQUESTでrouteするため、Plan管理taskでは固定transport文が検索queryになる。
- completed commentlint dogfoodではその検索が8件を返したが、主な候補はlive Sol A/B、final verification、compaction、session rotation、test impact、model routing等で、launcher実装のnavigationとしてほぼ役立たず、workerは別途Read/Grepで対象を探索した。
- parent transportを再びtask本文で膨らませることはCodex Reductionに反する。

## External feasibility

status: not-applicable

## Purpose

Plan管理taskのworker navigationを固定parent transport文ではなく、すでに解決済みのACTIVE task authorityへ機械的に接続し、default-on BM25の無関係候補と後続探索を減らす。

## Contract

- `glm-parent-action start`のcompact fixed USER_REQUESTは維持し、検索のためだけにtask本文をparent transportへ複製しない。
- workflowが既に持つACTIVE task path/contextを検索seed選択へ再利用する。
- Plan管理taskでは固定文`現在のACTIVE taskを実行してください。`をeffective BM25 queryにしない。
- task本文全体を無条件にqueryへ投入せず、task title/Purpose/Resolved references/明示path等から既存parserで機械的に得られる小さいsurface、またはknown-target skipを優先する。
- semantic classifierや追加model callで検索queryを生成しない。
- Plan非管理のexplicit USER_REQUESTと既存known-target skipの成立性を維持する。
- navigation-only authority、result cap、cache、telemetry、exhaustive proofを変更しない。

## Must not

- full task本文をparent model promptへ再注入しない。
- BM25結果をimplementation authorityへ昇格しない。
- task proseを意味分類する新LLM call/parser frameworkを作らない。
- workerが通常Read/Grepを使えないようにして検索品質を強制しない。
- 測定根拠なくrepo-search全体を無効化しない。

## Acceptance criteria

- Plan管理startのregressionでeffective worker repo-search queryが固定transport文にならない。
- launcher型task fixtureではimplementation-relevant candidateを返すか、ACTIVE authorityからtargetが十分明確ならdeterministically known-target skipになる。
- explicit USER_REQUESTを使う通常routeの既存behaviorを維持する。
- model callを追加せず、parent USER_REQUEST/prompt sizeを増やさない。
- repo-search telemetryは既存category/outcome/result count/duration semanticsを維持する。
- full validationと独立reviewを通す。

## Historical invariants

- ACTIVE task fileがPlan管理taskの要求authorityである。
- repo-searchはnavigation-onlyで、worker/reviewerの意味判断を置換しない。
- parent transport縮小を検索都合で巻き戻さない。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。commentlint dogfoodでPlan-managed fixed requestとworker search queryのintegration mismatchを実再現した。