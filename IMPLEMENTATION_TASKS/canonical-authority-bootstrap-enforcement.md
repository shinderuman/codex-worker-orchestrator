# Task: canonical authority bootstrap enforcement

## Original instruction

````text
43に残っているやつを全部IMPLEMENTATION_PLANに戻せ
順序もちゃんと考慮してIMPLEMENTATION_PLANにしろ
もちろんFindingで見つかったやつも全部IMPLEMENTATION_PLANにする
````

## Amendments

none

## Resolved references

- Dogfood bundle `3de7a4cd-b296-4630-a3e1-7acd342b9c0a` のfresh parent bootstrapで、canonical `glm-worker --authority rules/plan/active` より先にauthority path探索・disk直読が行われた
- session注入済みinstructionはcanonical authority tripleを先に読むよう要求していたが、prose-only順序要求では実際のbootstrapを拘束できなかった
- 本Findingは特定shell commandの禁止ではなく、active repository harnessでcanonical authority projectionをbootstrapのnormal pathとしてmachine-ownedにする責務を扱う
- formal Dogfood `cc4e60d1-8e64-4f86-a66a-8a2acd070163` ではcompactionが複数回発生し、compaction前はouter/inner `21600000` long-blocking waitを使用していたのに、最初のcompaction直後から同一running sessionへparentが明示的に`30000`×10、`60000`×14、後続で`300000`等のshort yieldを生成した
- current managed profile/runtimeには`21600000` long-wait settingが存在し、同Taskの正常区間でも実際に使用されているため、blocking mechanism未実装ではなくcompaction/resume後にcanonical authority/connection contractを再取得せずparent behaviorが退行した再発と判定する
- closed #455どおりCodex/Desktop host schedulerがcellをsuspendし続けるかはrepositoryから強制不能であり、本taskはhost schedulerそのものを実装対象にしない。custom-tool waitのbundle計測は`parent-wait-custom-tool-observability.md`が別ownerである

## Purpose

active repository harnessのfresh parent bootstrapおよびcompaction/session-resume bootstrapで、canonical rules / plan / active authorityを同一snapshotから取得する前に自由なpath探索・disk直読やstale orchestration memoryへ依存する必要をなくし、authority source selectionと再接続時のcontract restorationをmachine-ownedにする。

## External feasibility

status: not-applicable

## Contract

- repository-harnessがactiveなtask/session bootstrapでは、canonical authority projectionが成立する前にparentがauthority file/pathを探索・直読する必要がないnormal pathを提供する
- fresh sessionだけでなくContextCompaction、provider/session interruption、long stop/resume等のRulesがauthority rereadを要求する再開境界でも、次のmodel-running orchestration/action selectionより先にcanonical rules / plan / active projectionを再取得する
- rules / plan / activeの同一snapshot整合性を一つのadmission/projection boundaryで扱う
- canonical bootstrap失敗時は自由なdisk探索やconversation/compaction cacheをfallback successとして扱わず、machine-visible recovery/errorへ落とす
- authorityのsemantic content自体をmachineが解釈・決定しない。machineはcanonical source selection / snapshot binding / admissionだけを所有する
- canonical instruction/profile connection ruleに含まれるlong-blocking parent wait contractは、compaction/resume後のbootstrapで失われたstale memoryに依存せず再び有効なparent orchestration contextへ接続される
- repository側がhost schedulerを制御できるとは主張しない。ただしparentが既存21600000 contractを忘れて明示的short yield overrideを生成する退行はcanonical bootstrap/resume pathで防ぐ
- repository-harness inactiveなforeign repositoryへこのrepository固有authorityを漏らさない
-追加model callを導入しない

## Must not

- shell command blacklistの列挙だけで終わらない
- `rg` / `sed`だけを禁止して別commandで同じbypassを残さない
- authority contentを別duplicated stateへコピーして第二正本を作らない
- generic filesystem access全般を禁止しない
- repository runtimeがCodex/Desktop host scheduler/yield retention自体を強制できると偽らない
- compaction後のshort wait対策として新しいpoll loop、timer、watcherを追加しない

## Acceptance criteria

- active repository harnessのfresh parent bootstrapで、canonical rules / plan / active projection成立前にauthority path探索/直読を必要としない
- ContextCompactionまたはsession resume相当fixture/evalで、再開後の最初のorchestration判断より先にcanonical authority projectionが成立し、stale conversation memoryだけで続行しない
- compaction/resume後のrunning-worker wait pathが、canonical long-wait contractを再取得した状態でexplicit short-yield overrideへ退行しないことをparent behavior regressionで固定する。ただしhostが不可避に返した場合の外部scheduler behaviorはPASS/FAIL条件へ混同しない
- stale/mismatched authority snapshotはfail closedする
- bootstrap failureがarbitrary disk fallbackへ自動昇格しない
- foreign/inactive repository behaviorは変えない
- current snapshotで独立reviewer、Sol semantic review、必要なrepository validationとruntime変更に応じたinstall/smokeを完了する

## Historical invariants

- authorityのsemantic正本はtracked Rules / Plan / Taskであり、machine projectionはsource selectionとsnapshot bindingだけを所有する
- conversation context / compaction summaryはcacheであり、resume authorityではない
- host scheduler external boundaryとparentが生成するexplicit yield contractを区別する

## Dependencies

- `IMPLEMENTATION_TASKS/machine-enforced-control-authority-legitimacy.md`
