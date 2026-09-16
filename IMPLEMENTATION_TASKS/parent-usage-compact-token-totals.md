# Task: parent usage compact token totals

## Original instruction

````text
どう考えてもCodexのトークン消費量が多すぎる
なにか進め方がおかしい
直ちに見直せ
````

````text
さっきのGLMのリセットが回復してからのタイミングでCodexの残りも100%だった
一瞬で45%を使い果たしている
どう考えても異常だ
直ちに見直せ
````

````text
過去まで見る必要はない
このGLMリセット後が異常すぎると言っている
直ちに見直せ
````

````text
復活してからの作業がなにかおかしいのは間違いない
直ちに見直せ
````

## Amendments

none

## Resolved references

- 2026-09-16の連続する2回のCodex efficiency checkpointで、compact `parent_usage`はtask coverage countだけを返し、availableなparent taskのtoken / activity totalを返さないため、Direct Codex対Codex + glm-workerのCodex Reductionがunknownのまま残った
- per-task parent usage reportはtask execution / parent finalization intervalごとのinput / cached input / output / reasoning / total tokensとmodel turns / tool calls / compactions / tool output bytesを既に算出する

## Purpose

availableなparent usageだけをboundedに集計し、Codex実消費とrotation / finalization overheadをcheckpointおよび最終Evalで比較可能にする。

## External feasibility

status: not-applicable

## Contract

- compact telemetry summaryのparent usageへ、task executionとparent finalizationを分離したtoken / activity aggregateを追加する
- available / ambiguous / unknownのcoverage countとunknown reasonを保持し、available cohortだけのtotalを全task totalへ昇格しない
- input / cached input / output / reasoning / total tokenの意味とcounter reset / missing anchor / chain分割を既存per-task parent usage reportと共有する
- task数に比例したrollout再scanを追加せず、既存batch scanを再利用する
- public CLI JSON shape変更と最上位Evalでの解釈は実装前に`NEEDS_SOL_DECISION`で確定する

## Must not

- unavailable / ambiguous taskのusageを0として集計しない
- raw rollout、prompt / response本文、thread全履歴をcompact outputへ含めない
- cached tokenを非cached inputへ混ぜたり、provider間で意味が異なるfieldを推定換算しない
- Direct Codex armとの比較可能性がないcohortをCodex Reductionの改善証拠にしない

## Acceptance criteria

- all available、available + unknown、ambiguous、counter reset、missing token fieldのfixtureでcoverageとaggregateを固定する
- task executionとparent finalizationのtoken / activity totalを別々にmachine-readable出力する
- compact outputがboundedで、raw locatorや本文を含まないことを確認する
- batch scan回数がtask数に比例して増えないことをtestで固定する
- post-105最終再評価が同一cohort identityとunknown理由付きでparent usage totalを利用できる

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta
- unknown / ambiguous usageを改善または0消費として扱わない

## Dependencies

none
