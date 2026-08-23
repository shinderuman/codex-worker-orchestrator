# Task: Desktop terminal payload二重描画を既知外部境界として保持する

## Original instruction

````text
もし実害がない場合はこの現象、状況を忘れないようにBLOCKEDとかあるいは新しい境界のタスクとして残せ
````

## Amendments

none

## Resolved references

- 「この現象」はTask 007 task ID `173436e3-633c-493d-a6c7-9816704f0888`の同一terminal machine JSONがCodex Desktop上で2回表示された事象を指す。
- 保存raw stdout、telemetry reviewer result、親Codexへ渡されたtool outputは各1件であり、現時点で確認できる重複はユーザー可視表示だけである。
- completion detectionが同じ表示問題を過去2度「解消済み」と誤判定した重大インシデント本体は`IMPLEMENTATION_TASKS/completion-detection-false-negative-incident.md`で扱う。このfileは、実害なしの場合にも表示現象と境界を忘れないための保持面である。

## Purpose

Desktop上のterminal payload二重描画がCodex実消費・Quality Deltaへ影響しないと判定された場合も、既知現象、確認済み境界、再調査条件をtrackedな正へ残す。

## Contract

- PlanのBLOCKEDに置き、表示美観だけを理由にACTIVE化しない
- 最上位判断基準はDirect Codex対Codex + glm-workerのCodex ReductionとQuality Deltaとする
- producer stdout、telemetry、親Codex tool output、Desktop表示を別の観測層として維持する
- Codex model contextまたは永続conversation contextへの二重流入が確認された場合は、actual token・compaction pressure・parent processingへの影響を測定するtaskとして再開する
- Codex Desktop側の診断情報または修正可能な境界が利用可能になり、上位目的への実害を低コストで判定できる場合は再調査してよい
- 再発だけではACTIVE化せず、既知証拠との差分または新しい実害証拠をactivation条件とする

## Must not

- Desktopに2回見えることだけからCodex tokenが2倍消費されたと推測しない
- 実害なしの表示問題をproduction orchestration変更の理由にしない
- repository側へblind dedupe、capture framework、daemonを追加しない
- 既知現象を「解消済み」と書き換えない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- BLOCKED中も現象、層別evidence、現在の非対応理由、activation条件がこのfileから復元できる
- 新しい証拠が出た場合、model/context二重流入とDesktop表示だけの再発を区別できる
- ACTIVE化する場合はCodex ReductionまたはQuality Deltaへの具体的影響仮説がPlan/taskへ記録される
- 実害なしの間はproduction code・instruction・event schemaを変更しない

## Historical invariants

- Task 007再現時のaccepted resultは保存raw stdout 1件、telemetry result 1件、親Codex tool output 1件、Desktopユーザー可視表示2件だった
- 現時点でCodex model contextへ同一JSONが2回流入した証拠はない
- 二重描画そのものは重大インシデントではなく、過去2度の検知失敗が重大インシデントである

## Dependencies

none

## Review findings

- repository/provider側の二重emitは確認されていない
- Codex actual tokenへの実害は未確認であり、Desktop表示だけなら対応不要

## Current boundary

既知外部境界としてBLOCKED。新しいmodel/context二重流入証拠、測定可能なCodex実消費増、Quality Delta低下、またはCodex Desktop側の調査可能な修正境界が得られるまで実装しない。
