# Task: parent usage interleaved turn ambiguity

## Original instruction

````text
目的:

* Sol High相当の品質を維持しながらCodex tokenを減らす
* correctnessを捕捉する意味判断reviewは削らない
* same-state / same-evidence waste、重複読取、不要なparent再入を見つける
* correctness defectはQuality Delta findingとして扱う

finding確定前に、open / closed Issue、current code、Plan / Taskを確認し、同じroot causeを重複管理しない。

findingは次へ処理する。

* 新規実装finding → 新しい`priority:do` Issueを作らずTask化
````

## Amendments

none

## Resolved references

- Dogfood bundle `2352c5a5-66b2-4263-8f4a-40e0d5d6e5c8` はtask lifecycleを07:01:27Z〜08:32:54Zの `task_execution.status=available` とし、`parent_token_delta` をinput 8,869,251 / cached 8,756,864としている
- 同じ `analysis-index.json` はinitial owning turn後かつlifecycle完了前に始まった7 turnを `unattributed-subsequent-request` として列挙し、そのinput合計は6,240,964 / cached 6,158,848である
- current `parentUsageExecutionInterval` / `analysisExecutionTokenDelta` はtask lifecycle startからexecution endまでの連続token anchor差を `available` として扱い、task ownership外turnが区間へ混在してもavailabilityを落とさない
- current compact parent usage aggregateはこの既存per-task semanticsを再利用するため、上記のようなmixed intervalもavailable cohort totalへ加算し得る
- closed #306はexact `glm-worker --status` + `glm-parent-action resume` evidenceを持つauto-resume continuation turnをtask-ownedへ拡張し、evidenceのないlater turnはunattributedのまま残す責務を持つ。今回のFindingはそのownership境界を維持したまま、unattributed turnがexecution intervalへ混在したusageを比較可能なtask usageとして扱わない責務であり、同rootではない

## Purpose

parent usageのtask execution token / activity aggregateを、machine-verifiableにtask-ownedなevidenceだけで比較可能とし、unattributed user turnが混在するlifecycle windowをCodex Reductionのavailable cohortへ誤って合算しないようにする。

## External feasibility

status: not-applicable

## Contract

- task execution usageのavailabilityはlifecycle時刻だけでなくcurrent `analysisTaskOwnership` / subsequent-request evidenceと整合させる
- execution interval内にtask-ownedと証明できないturnが存在する場合、そのturnのtoken / activityをtask usageへ推定帰属しない
- exact task-owned segmentだけを既存anchor semanticsでlosslessに集計できる場合だけ集計し、そうでなければtask execution usageをboundedなunknown / ambiguous / non-comparable reasonとしてfail closedする
- #306でmachine-verifiableになったauto-resume continuation ownershipは維持し、exact continuation evidenceを持つturnをunattributedへ戻さない
- parent turn内にtask作業と別ユーザー要求が混在して区切れない場合、prose内容や時刻近接から割合配分せずnon-comparableとして扱う
- compact parent usage aggregateはnon-comparable taskをavailable `tasks_summed` / token / activity totalへ加算せず、coverage / exclusion reasonへ反映する
- Direct Codex対Codex + glm-worker Evalは同じownership/comparability semanticsを使用し、raw lifecycle deltaをtask costとして昇格しない
- model call、daemon state、第二task authorityを追加せず、既存rollout turn / task ownership / token anchor evidenceを再利用する

## Must not

- later turnをtimestamp、同一thread、自然言語、commit messageだけでtask-ownedと推測しない
- `unattributed-subsequent-request` のtokenを便宜的にtask totalから算術控除し、残差を正確なtask usageとみなさない
- user correction / unrelated requestを0 tokenまたはtask costとして扱わない
- exact ownership不明をavailableへ縮退しない
- #306のauto-resume ownership contractを弱めない
- task usageを綺麗に見せるためraw evidenceやsubsequent-request reportingを隠さない

## Acceptance criteria

- lifecycle start〜completeの間にunattributed later turnが存在するfixtureで、task execution token / activity usageが比較可能なavailable totalとして返らない
- bundle `2352c5a5-66b2-4263-8f4a-40e0d5d6e5c8` 相当fixtureで、8,869,251 input全量がtask-owned available usageとしてcompact aggregateへ加算されない
- single task-owned turnだけのfixtureは従来どおりavailableである
- #306相当のexact auto-resume continuation fixtureは複数turnでもtask-ownedとして扱われ、unrelated later turnだけが除外またはnon-comparable判定へ寄与する
- task-owned / unattributed workが同一turnに混在して機械分離不能なfixtureは推定配分せずunknown / ambiguousになる
- compact summaryでavailable / excluded coverage countとreasonが整合し、excluded taskのtokens/activityがaggregate totalへ混入しない
- counter reset、missing anchor、rollout chain、missing token fieldの既存failure semanticsを維持する
- Repository Lintと関連Go testがPASSする

## Historical invariants

- Codex Reductionは比較可能なcohortだけで評価し、unknown / ambiguous usageを改善または0消費として扱わない
- task ownershipはmachine-verifiable evidenceを正とし、conversation proseから推測しない
- raw rollout evidenceと`unattributed-subsequent-request`の区別を保持する

## Dependencies

none
