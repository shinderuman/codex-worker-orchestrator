# Task: finalize-checkでlifecycleを先に判定しcurrent validationを再利用する

## Original instruction

````text
Codexの節約、Codexの判断、GLMの判断、無駄な行動等多角的な問題をひたすら洗え
````

## Amendments

````text
実装を開始する前に、起票したIssueにCodexにやらせられるタスクがあったらCodexにまわしてくれ
````

## Resolved references

- current `finalize-check`はblocking quality validationを最初に開始し、その後canonical handoff/lifecycle consistencyとcurrent snapshot照合を行う。
- commentlint dogfoodの最初の`finalize-check`はtaskが`waiting-sol-review`の時点で呼ばれ、final completionへ進めないlifecycleだった。
- cwd bugが修正されると、この順序では最終化不能stateでもfull suiteを先に実行し得る。
- canonical handoffはexisting validationsとsnapshot identityを保持するが、current `finalize-check`は既存の同form/current-snapshot passing validationを先に再利用せず、新validationを作ってからhand-offに含まれるかを照合する。

## External feasibility

status: not-applicable

## Purpose

cheapなlifecycle/snapshot preconditionをexpensive validationより先に評価し、既存authority上current snapshotへ有効なpassing validationが既にある場合は再利用して、parent finalizationのdeterministic gate workを削減する。

## Contract

- canonical handoffとexisting validation-run identity/snapshot semanticsを単一authorityとして再利用する。
- finalizationが現在admissibleでないlifecycleはfull quality gate開始前にbounded blocked resultを返す。
- requested formのpassing validationがcurrent exact snapshotへ既に有効なら、そのevidenceを再利用しduplicate gateを開始しない。
- validationがmissing/stale/wrong-formの場合だけ、lifecycleが許す時点でexisting blocking quality gateを実行する。
- snapshot mutationはexisting semanticsどおりvalidation reuseを無効化する。
- parent semantic accept/fix/task completionとunexpected state judgmentを自動化しない。
- model callを追加しない。

## Must not

- handoffを第二のvalidation databaseとして複製しない。
- stale validationをtimestamp近似で再利用しない。
- lifecycle inconsistencyを自動repairしない。
- every finalizationで新validationを必須にする現状を別の無条件validationへ置換しない。

## Acceptance criteria

- `waiting-sol-review`等finalization非admissible stateではquality gate subprocessを開始せずblocked resultを返す。
- current snapshotへrequested formのpassing validationが存在する場合、duplicate validation runを作らずready evidenceへ利用できる。
- missing/stale/wrong-form validationではexisting quality gateを実行する。
- validation後snapshotが変わると再利用せずfail closed/再validationする。
- existing `finalize-check` machine outputのsemantic authorityを維持する。
- full validationと独立reviewを通す。

## Historical invariants

- validationはexact snapshotにbindする。
- semantic acceptanceとGit publicationは親Codex authorityに残る。
- unexpected lifecycle/Git stateは親判断へ戻す。

## Dependencies

- `IMPLEMENTATION_TASKS/finalize-check-module-cwd.md`

## Review findings

none

## Current boundary

未着手。module cwd correctness修正後に実装・dogfoodする。