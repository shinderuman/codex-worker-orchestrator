# Task: execution milestone reconsideration after single

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- closed #211はone semantic ACTIVE taskを2〜8個のdurable GLM execution milestoneへ分けるmechanismを実装済み
- closed #866はoperationally large taskでmilestone mechanismがsilent non-activationする再発を受け、parent action pathで`single|milestones`を明示選択させるactivation reliabilityをcompleted扱いした
- formal Dogfoodでは最初の`NEEDS_SOL_DECISION`でparentがmachine templateを受け取り`EXECUTION_UNIT: single`を明示選択したが、その後taskは約36.7h、54 GLM calls、17 fix、7 rate-limit、4,000行超のimplementationへ拡大した
- waiting-decision / waiting-sol-review / rate-limit等の自然なparent boundaryが多数あったにもかかわらず、`start-milestones=0` / `revise-milestones=0`のまま終了した
- したがってcurrent gapは初回選択の明示化ではなく、early `single` dispositionがmaterial expansion後のbounded reconsiderationを実運用上永久抑止すること

## Purpose

初回に合法な`single`を選択したACTIVE taskでも、後続の自然なparent boundaryでmaterially largeになった具体的semantic evidenceが得られた場合に、既存#211 milestone lifecycleを再検討できるbounded orchestration boundaryを作る。

## External feasibility

status: not-applicable

## Contract

- existing #211 milestone state、`start-milestones` / `revise-milestones`、task-wide Contract/Must-not/Acceptanceを再利用し、第二のexecution-unit authorityを作らない
- 初回`single`選択はその時点の合法dispositionとして保持するが、将来のnatural parent boundaryでmilestone reconsiderationを禁止するpermanent decisionとして扱わない
- waiting-decision、waiting-sol-review、provider/rate stop、recoverable guard、明示stop等の自然なboundaryで、current material evidenceからparentがsemanticに「single継続」または「milestonesへ移行」をboundedに選択できる
- reconsiderationの契機は既にparentへ返ったconcrete breadth evidenceを使い、token/file/timeのraw thresholdだけでauto splitしない
- parent Codexがcoherent milestone scope/acceptanceを決め、GLMへdecomposition decisionを委譲しない
- healthy in-flight callをrepartitionのためだけに停止しない
- milestoneへ移行しても完了済みworkを再実装せず、current Git + durable task/milestone evidenceから継続する
- task-wide independent review/Sol final authorityを維持し、milestoneごとのfull review ceremonyをroutine追加しない

## Must not

- raw token数、file数、wall-clockだけをsole classifierにしない
- milestone判断専用model callを追加しない
- GLM自身に「自分を分割すべきか」を決めさせない
- semantic Plan taskを機械的に複数Taskへ割ることで代替しない
- early `single`選択を失敗扱いして作業をrestartしない
- milestoneごとにfull independent/Sol reviewを強制してCodex costを増やさない

## Acceptance criteria

- 初回`single`で開始した代表fixtureが、後続natural boundaryでmaterial breadth evidenceを得た後、既存milestone lifecycleへ移行できる
- prior completed workを再実装せず、2つ以上のbounded milestoneをcurrent Gitから継続できる
- explicit `single`継続を選ぶ小規模/十分bounded caseには追加model callや大きなceremonyがない
- early `single`が後続reconsiderationをmachine/action surface上で黙って抑止しない
- task-wide final review/acceptance、semantic task identity、existing milestone state validationを維持する
- formal Dogfood型の長大task fixture/evalで、自然boundaryがあるのにmilestone選択surfaceが二度と提示されないregressionを固定する
- Repository Lint、関連Go test/full suiteがPASSする

## Historical invariants

- semantic Plan taskとexecution milestoneは別概念である
- milestone scope/acceptanceの意味判断はparent Codexが所有する
- natural boundaryでのみin-flight structureを見直し、healthy callを時間だけで止めない

## Dependencies

none
