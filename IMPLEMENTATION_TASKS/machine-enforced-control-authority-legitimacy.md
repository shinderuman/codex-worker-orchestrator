# Task: machine-enforced control authority legitimacy

## Original instruction

````text
Findingで見つかったやつも全部IMPLEMENTATION_PLANにする。
````

## Amendments

none

## Resolved references

- DogfoodではSession Rotationがgeneric new-task admissionの前段でmachine-enforcedされる一方、Dogfood外側では1 task = fresh parent sessionを意図的に保証しており、context-reset policyとruntime state machineの二重ownershipが観測された
- 過去の責務監査はcorrectness/safety invariantとworkflow preferenceを分離したが、主にcanonical owner整合性を確認し、「そのauthority自体をgeneric runtimeが所有すべきか」というtop-level legitimacyを十分に判定していなかった
- current `machine-enforced` controlは、canonical ownerの一意性だけでなく、authority existence / layer legitimacyを先に証明する必要がある

## Purpose

machine-enforced controlが「一つのcanonical ownerを持つ」だけで正当とみなされる状態をやめ、そもそもそのinvariantをmachine enforcementすべきか、そのruntime/layerにauthorityがあるかを同一基準で再確認し、過剰authorityだけを安全に縮小する。

## External feasibility

status: not-applicable

## Contract

- machine-enforced controlごとに、canonical owner整合性より先に authority existence / layer legitimacy を証明する
- 少なくともSession Rotationについて、generic task correctness、repository development/Dogfood workflow、external Codex thread/session creation responsibilityを分離する
- workflow preferenceだけを「親がproseを守らない」理由でgeneric hard fail-closed stateへ昇格させない
- exact-once identity、wrong-thread / duplicate start防止、target-task identity、retry整合等の独立correctness invariantは、必要性を証明できる最小layerへ残す
- current `machine-enforced` controlsを `intrinsic correctness / repository policy / external owner / semantic parent / workflow optimization` の同一観点でdispositionする
- authorityが正当なcontrolはKEEP/NO CHANGEとし、理由なく再実装しない
- 過剰authorityは安全に独立mergeできる最小SIMPLIFY/MOVE/REMOVEへ落とし、Quality Deltaを弱めない
- machine-negative resultとresidual parent judgmentの共通authority境界は別task `machine-negative-result-authority.md` が所有する。本taskではclassification/legitimacy観点だけを扱う

## Must not

- Session Rotationを結論先行で全削除しない
- canonical ownerが一つであることだけをauthority正当性の根拠にしない
- control数削減そのものを目的にcorrectnessを弱めない
- generic plugin/policy frameworkを作らない

## Acceptance criteria

- Session Rotationの各invariantについて、どのlayerが所有すべきかcurrent evidence付きのdispositionがある
- external fresh-session boundaryとgeneric runtime responsibilityの二重ownershipが残る場合、その必要性が具体的に証明される
- current machine-enforced controlsについてauthority existence / layer legitimacyを同じ基準で確認し、未disposition controlがない
- actionable overreachは安全な最小production simplificationへ反映される
- current snapshotで独立reviewer、Sol semantic review、必要なrepository validationとruntime変更に応じたinstall/smokeを完了する

## Historical invariants

- 最上位目的はSol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減すること
- machine enforcementは親prose違反への万能な代替ではなく、正当なmachine-owned invariantだけをfail closedにする

## Dependencies

none
