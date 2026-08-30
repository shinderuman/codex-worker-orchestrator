# Task: quality-surface変更を親承認前に早期停止し保存済みresultから継続する

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

- completed commentlint dogfood taskでは、最初の`worker-new`がprotected quality surfaceであるroot `commentlint`を変更した。
- 現行workflowは`runWorkerModelWithRuleActivation()`内でdeterministic rule activationとparent validation convergenceを終えた後、`handleWorkerResult()`で`verifyQualitySurfaceBaseline()`を実行する。
- そのため親承認が必須と機械的に確定するsnapshotへ、停止前に`worker-auto-fix-1`と`worker-auto-fix-1-rule-activation-1`を追加dispatchした。
- bounded `current-diff`親承認経路の統合後も、同じtaskをreviewへ進めるため`worker-explicit-fix` / maxを再dispatchした。このcallではsemantic source/test変更は発生せず、既存diffの再validationが中心だった。
- 既存guard recoveryには、recoverable guard修復後に保存済みcompleted worker resultを再利用し、worker modelを再dispatchせず同一taskを継続できるproduction semanticsがある。

## External feasibility

status: not-applicable

## Purpose

unapproved quality-surface変更を最初に検出可能な安全境界で停止し、明示的な親承認後は既存worker resultとexact snapshot evidenceから同一taskを継続して、不要なworker再dispatchを削減する。

## Contract

- quality-surface defaultはfail closedを維持し、GLM自身による承認を許可しない。
- worker resultがunapproved quality-surface mutationを初めて作った時点で、不要なrule-activation correction、parent validation、reviewer等へ進む前に親承認待ちへ停止する。
- 停止時にcompleted worker resultと継続に必要なexact snapshot stateを保持する。
- 親semantic authorityは既存`current-diff` accepted-scope境界を再利用し、新しい永続repo-wide bypassを追加しない。
- 親が現diffを明示承認しただけでsemantic fixを要求していない場合、保存済みworker resultから継続し、承認を通すためだけの`worker-explicit-fix` model callを発生させない。
- 承認後に必要なdeterministic rule activation、parent validation、independent reviewは省略しない。
- 承認scope外または承認後の追加quality-surface mutationは再度fail closedに停止する。
- quality-surfaceを変更しない通常taskのmodel call数を増やさない。

## Must not

- broad `parent approved = quality guard bypass` flagを追加しない。
- quality surfaceの対象pathを減らして解決しない。
- parent承認をworker proseから推測しない。
- rule activation、parent validation、independent reviewを承認済みという理由だけで飛ばさない。
- task resetや既存worker変更の破棄を通常回復経路にしない。

## Acceptance criteria

- regression taskで初回worker resultが`commentlint`を変更した場合、親承認前にauto-fix / rule-activation correction / reviewer model callが発生せず停止する。
- 停止時のworker resultは同一taskで再利用可能な状態として保存される。
- 親がexact current diffを承認した場合、semantic fixが無ければworker modelを再dispatchせず、必要なpost-approval convergenceから継続する。
- 承認後にscope外quality-surface変更を追加すると再度停止する。
- existing instruction-surface guard recovery、snapshot authority、accepted-scope risk semantics、parent authorityと矛盾しない。
- full validationと独立reviewを通す。

## Historical invariants

- quality policyをworker自身が弱体化できない。
- current-diff accepted scopeは親Codexが明示的に選ぶbounded authorityである。
- saved-result guard recoveryは新しいtask state machineを作らず既存lifecycleへ再接続する。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。次の新規Codex長期sessionで最初にdogfood実行する候補。