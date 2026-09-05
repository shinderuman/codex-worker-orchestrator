# Task: telemetry history compact summary

## Original instruction

````text
さっきの「意味のある停止」というのが何なのか知らないが、そういうのを改善したほうがいいと思うなら随時タスクに積んでいくように
今後も作業中に改善要素を見つけたら随時タスクに積むように
022の前での再評価タスクでも改めてタスクに積むべきものがなかったか精査するように
````

## Amendments

none

## Resolved references

- 2026-09-03のinstalled smokeで `glm-worker --call-outliers history` は64,102 bytesを返し、Codex tool output上限でJSONが途中切断された
- 必要なcohort count / outlier countだけを得るためにschema key確認を含む追加projection turnが発生し、親Codex tokenとtool turnを増やした
- 詳細report自体は診断用に必要なので既定出力を削除せず、parent evaluation向けの明示的compact projectionを追加する
- 2026-09-05の中間再評価では期間内24 taskのmodel/token/fix/review集計に`--stats --task`を24回、親Codex attribution coverage集計に`--parent-usage`を24回実行する必要があり、history query 1回では最上位Evalに必要な合計とcoverageを得られなかった

## Purpose

telemetry historyを再解析する親Codexが巨大reportを受け取らず、1回のread-only commandでcohortとoutlierの採否判断に必要なbounded summaryを取得できるようにする。

## External feasibility

status: not-applicable

## Contract

- history stats / call-outliersの既存parser・scan・cohort計算を再利用し、明示的なcompact summary projectionを追加する
- summaryはquery、scan status、file/task/call count、cohort version/schema revision、coverage/unknown、excluded reason、outlier eligibility/countを返す
- current schema cohortごとにworker/reviewer/fix/resume/rate-limit、model別prompt/output/turn、parent outcome/fix origin、Sol packet bytesの期間合計と、parent usageのavailable/ambiguous/unknown task countを返す
- 詳細特定が必要な場合だけ、決定論的にboundedな上位N件のtask/call IDと既存source locatorを返し、全session/task/call配列を含めない
- stdoutはsingle JSON objectとし、entry上限とbyte上限または同等の機械的boundedness、truncated/omitted countを明示する
- 既存の詳細出力とcurrent default behaviorを維持し、compact指定時だけprojectionを変更する
- model call、repository mutation、telemetry rewrite、別DBを行わない

## Must not

- 親Codexにraw詳細JSONを一度出してから後turnでcompact化させない
- compact summaryで異なるcohortを合算しない
- unknown/partial/excludedをゼロまたは正常値へ縮退しない
- boundednessのためにoutlier判定、coverage、source authorityを再実装しない

## Acceptance criteria

- 64KB級fixtureでもcompact出力が定めた上限内のvalid single JSON objectとなり、truncated/omitted countが正しい
- current/history、task/期間filter、複数cohort、partial、unknown、outlierなし/ありをsummaryで判別できる
- installed実dataで1回のcommandから、2026-09-03 smokeで複数turnを要したcohort・records・outlier countを取得できる
- installed実dataで、2026-09-05に48回のtask別commandを要した期間合計とparent attribution coverageを1回のbounded commandで再取得できる
- 既存詳細出力、stats、call-outliers、stdout/error contractにregressionがない
- 独立reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta
- structured machine evidenceは最初のtool call内で必要fieldへprojectionし、raw全文をSol-visible stdoutへ出さない

## Dependencies

none

## Review findings

none

## Current boundary

telemetry観測Task完了後、105より前に実装する。
