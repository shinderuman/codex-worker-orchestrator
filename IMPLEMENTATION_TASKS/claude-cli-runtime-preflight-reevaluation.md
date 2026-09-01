# Task: Claude CLI runtime preflightの再評価条件を保持する

## Original instruction

````text
2026-08-23 user instruction:

それはその判断が必要なときにお前が思い出せる様になってるの
````

## Amendments

none

## Resolved references

- 「それ」は完了済みClaude CLI compatibility preflight PoCのSol判断を指す。
- 当時の判断はruntime preflightを実装せず、test-only checker・依存flag inventory・help snapshot・live no-AI canaryだけを最小採用するもの。
- 再検討条件は、Claude CLI更新による実互換障害、同種障害での親Codex診断turn反復、またはflag/help drift canaryの実失敗である。

## Purpose

PoC判断をHistoryだけの受動記録にせず、再評価条件が成立した際にPlanから発見できる条件付きtaskとして保持する。

## Contract

- 次のいずれかを一次証拠で観測した場合だけ再評価候補にする
  - Claude CLI更新によるrunner/probeの実互換障害
  - 同種CLI互換障害で親Codexの診断・修復turnが反復
  - test-only preflight / inventory / live canaryが実際のflag・help driftを検出
- activation時は`IMPLEMENTATION_HISTORY.md`の`2026-08-23 Claude CLI compatibility preflight` decision recordと該当failure evidenceを読み、runtime gateのCodex Reduction、Quality Delta、false reject、全task停止riskを再計測する。PoCの通常completion証跡はGit / CIから回収する
- trigger成立だけでproduction実装を確定せず、runtime昇格・最小採用維持・撤退を新しい証拠で判断する

## Must not

- trigger未成立のままACTIVE化しない
- Claude CLI version更新だけを実障害扱いしない
- 古いPoC推奨だけを根拠にruntime fail-closed gateやoverride経路を追加しない
- generic doctor/preflight frameworkへ拡張しない
- GLMにcommit/pushさせない。pushしない

## Acceptance criteria

- activation triggerに該当する一次証拠を明示
- PoC時点の約0.25秒overhead、診断1〜2 turn削減見込み、help format false reject riskを新観測と比較
- runtime gate採否とrollback/override要否をSolが再判断
- production実装を採用する場合だけconcrete Contractとtestを確定してACTIVE候補にする

## Historical invariants

- 完了済みPoCはruntime gate不採用・test/inventory最小採用と判断済み
- `IMPLEMENTATION_HISTORY.md`にはこの非diff decisionと再評価境界だけを残し、PoCのcommit / validation chronologyは複製しない

## Dependencies

none

## Review findings

none

## Current boundary

再評価trigger未観測のためBLOCKED。通常作業では開始しない。
