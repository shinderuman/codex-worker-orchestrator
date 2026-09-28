# Task: 条件付きfailure-path advisoryのproduction adoption

## Original instruction

```text
System-One semantic workload closure評価でGoとなった条件付きfailure-path reviewerのadvisory経路を、独立したbounded decision classとして通常production reviewへ導入する。
```

## Amendments

none

## Purpose

通常reviewerが見逃し得る反復的なfailure-path findingを独立したGLM reviewerが先に抽出し、Sol Highの検証対象を絞る。採否とsource proofの責任はSol Highへ残し、DogfoodでCodex / Sol実消費とQuality Deltaを測る。

## External feasibility

status: not-applicable

## Contract

- 対象decision classはexternal-model invocation、metric / reduction accounting、external-output persistence、review / lifecycle / routingの4 trigger classに限定する。changed pathと内容のdeterministic triggerを既存trialから引き継ぎ、trigger不成立時は追加model callを行わない。
- bounded inputはcurrent Taskの関連diff・trigger根拠・通常reviewer結果に必要な最小情報とする。独立role / sessionで呼び出し、structured outputはfindingごとのclass、issue、exact source locator、判定不能状態を区別する。
- production routing pointは通常reviewer結果の後、Sol-visible review packetを作る前とする。成功したadversarial findingを短いadvisoryとして同packetへ載せ、通常reviewerのstatus、canonical accept / fix、source proof、reviewer independenceを変更しない。advisoryはSolが検証する候補であり、自動採否authorityを持たない。
- provider / schema failure、bounded deadline、partial output、interrupt、uncertain result、registry failure時はadvisoryを出さずcanonical reviewer結果へfail-openする。失敗や欠測をhit・Quality・Reduction成功へ算入しない。
- Sol Highはfinding妥当性、true positive / duplicate / false positiveのlabel、最終採否、曖昧・高risk・高レバレッジなsemantic tailを保持する。deterministicに証明できる条件はmodelへ委ねない。
- 初回rolloutは4 classと既存cohort上限を越えないbounded pilotとし、flagで即時rollback可能にする。zero labeled recordでもadvisory表示自体は有効化し、決定権限の追加昇格は本Taskで行わない。
- task単位の同一cohortでhit率、fail-open率、false negative / escaped finding、Quality Delta、false positive、追加GLM cost、Codex / Sol実token、review / fix / re-entry削減、latency、human interventionを記録・比較する。tokenが直接得られない場合はproxyと区別し、未測定を成功値にしない。
- production導入後の通常TaskをDogfoodとして継続観測できるtelemetryと、scope拡大・縮小の判断境界を残す。GLM追加消費だけを停止条件とせず、Codex / Sol削減が観測できないか品質が悪化する場合はrollbackする。

## Must not

- shadow-only / trial-onlyのままterminalにしない。
- failure-path advisoryによって通常reviewer statusやcanonical accept / routingを自動変更しない。
- advisoryをsource proofやSol判断の代用にしない。provider failure・partial output・判定不能を成功計上しない。
- 他のdecision classを同じTaskへ混ぜない。

## Acceptance criteria

- 対象4 classだけで、実際の通常review executionから独立GLM semantic resultがSol-visible advisoryへ到達する。trigger不成立とfailure時は従来経路へ戻る。
- structured finding、bound、fail-open、Sol tail、source proof / reviewer independence維持、flag rollbackが検証される。
- Quality Delta / false negative / escaped findingとCodex / Sol実消費を区別してDogfood計測でき、追加GLM cost・latency・human interventionも同じcohortへ結び付く。
- 通常の独立review、validation、publicationを経て、このdecision classの変更だけを1 commitでterminalにする。

## Historical invariants

- System-One Goはdecision class / execution path単位で実装し、Sol Highには曖昧・高risk・高レバレッジなtailを残す。
- 最上位EvalはCodex ReductionとQuality Deltaであり、GLM追加消費だけを不採用理由にしない。

## Dependencies

none

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/system-one-semantic-workload-closure-eval.md`
