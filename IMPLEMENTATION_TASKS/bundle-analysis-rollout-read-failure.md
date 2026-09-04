# Task: Bundle analysis rollout read failure

## Original instruction

````text
今後も作業中に改善要素を見つけたら随時タスクに積むように
````

## Amendments

none

## Resolved references

- parent usage report evidence integrity taskの独立reviewで、`glm-worker/internal/app/bundle_analysis.go:493`の`scanAnalysisRolloutWindow`もrollout scan errorをempty scanへ変換していることを確認した
- parent usage専用pathは同taskでunreadableへ修正するが、bundle analysis pathは別surface・別structured outputのため範囲外として残った

## Purpose

bundle analysisのrollout read/parse failureを証拠不存在へ縮退させず、親Codexが欠損と障害を区別できるようにする。

## External feasibility

status: not-applicable

## Contract

- `scanAnalysisRolloutWindow`のopen/read/parse errorをempty scanへ変換せず、bundle analysisのdegraded/unknown evidenceへ伝播する
- 正常に読めたrolloutにanchor/eventがない状態と、rolloutを読めなかった状態をmachine status/reason/source locatorで区別する
- partial scanが得られてもerror時はavailableなtoken/activity値として採用しない
- parent usage pathで導入済みのunreadable semanticsと語彙・failure attributionを可能な範囲で共有し、独立した互換layerを増やさない

## Must not

- read failureをzero usage、missing、no-observationへ縮退しない
- raw rollout/error transcriptをmachine JSONへinlineしない
- counter reset、partial anchor、timeline fallbackの既存防御を緩和しない

## Acceptance criteria

- rollout open/read/parse failureと正常なevidence不存在が異なるstructured outcomeになる
- failure時に推測token delta・activity countを出さず、exact source locatorを保持する
- parent usageとbundle analysisの同種failureが矛盾する分類にならない
- relevant test、independent reviewer、Sol review、current snapshot validation、commit/installを完了する

## Historical invariants

- Codex実消費の観測不能を利用可能なzero/missing evidenceへ誤分類しない

## Dependencies

none


## Review findings

none

## Current boundary

現在のparent usage task完了後に、残存するbundle analysis surfaceだけを扱う。
