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

## Purpose

active repository harnessのfresh parent bootstrapで、canonical rules / plan / active authorityを同一snapshotから取得する前に自由なpath探索・disk直読へ依存する必要をなくし、authority source selectionをmachine-ownedにする。

## External feasibility

status: not-applicable

## Contract

- repository-harnessがactiveなtask/session bootstrapでは、canonical authority projectionが成立する前にparentがauthority file/pathを探索・直読する必要がないnormal pathを提供する
- rules / plan / activeの同一snapshot整合性を一つのadmission/projection boundaryで扱う
- canonical bootstrap失敗時は自由なdisk探索をfallback successとして扱わず、machine-visible recovery/errorへ落とす
- authorityのsemantic content自体をmachineが解釈・決定しない。machineはcanonical source selection / snapshot binding / admissionだけを所有する
- repository-harness inactiveなforeign repositoryへこのrepository固有authorityを漏らさない
-追加model callを導入しない

## Must not

- shell command blacklistの列挙だけで終わらない
- `rg` / `sed`だけを禁止して別commandで同じbypassを残さない
- authority contentを別duplicated stateへコピーして第二正本を作らない
- generic filesystem access全般を禁止しない

## Acceptance criteria

- active repository harnessのfresh parent bootstrapで、canonical rules / plan / active projection成立前にauthority path探索/直読を必要としない
- stale/mismatched authority snapshotはfail closedする
- bootstrap failureがarbitrary disk fallbackへ自動昇格しない
- foreign/inactive repository behaviorは変えない
- current snapshotで独立reviewer、Sol semantic review、必要なrepository validationとruntime変更に応じたinstall/smokeを完了する

## Historical invariants

- authorityのsemantic正本はtracked Rules / Plan / Taskであり、machine projectionはsource selectionとsnapshot bindingだけを所有する

## Dependencies

- `IMPLEMENTATION_TASKS/machine-enforced-control-authority-legitimacy.md`
