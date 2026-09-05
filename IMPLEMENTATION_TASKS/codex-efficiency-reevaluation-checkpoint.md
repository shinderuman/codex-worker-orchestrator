# Task: Codex efficiency reevaluation checkpoint

## Original instruction

````text
post-105-codex-efficiency-reevaluation.mdを定期的にやってほしいんだが奥に追いやられて一向に行われないので同様のタスクをなるべく早いタイミングでやってくれ
いまのpost-105-codex-efficiency-reevaluation.md自体は022の直前に残す
だが現段階の再評価をしてほしい
そして再評価したあとにタスクの優先度つけ直しをしてほしい
````

## Amendments

### 2026-09-05

````text
数ターンごとにやってる監査と同様にMarkdownのチェックをするべきなんじゃないの
````

## Resolved references

- 2026-09-05の中間再評価完了後、最大5 task完了以内に再実行するcheckpoint
- `post-105-codex-efficiency-reevaluation.md`は105完了後・022直前の最終safety netとして別に維持する
- Markdown確認は`markdown-derived-state-authority-audit.md`の初回inventory完了後、このcheckpointへ固定項目として統合する。`post-105-codex-efficiency-reevaluation.md`の監査範囲は広げない

## Purpose

新たな実装・incident・telemetry evidenceを短いfeedback loopで再評価し、Codex ReductionとQuality Deltaに基づいて022前のTaskとpriorityを補正する。

## External feasibility

status: not-applicable

## Contract

- 親Codexだけが追加AI callなしで実行し、GLM modelへ分析・採否・priority判断を委譲しない
- 前回checkpoint以後のCodex/GLM telemetry、parent usage、review/fix/validation、停止/recovery、未Task化Findingを既存bounded machine projectionで比較する
- `markdown-derived-state-authority-audit.md`完了後は、前回checkpointのGit locator以後に追加・変更されたtracked Markdownと前回から未解決のauthority候補だけを確認し、手動current value、重複authority、意味的に陳腐化し得る実装説明の新規混入をGo/No-Goする
- Direct Codex対Codex + glm-workerのCodex ReductionとQuality Deltaを最上位Evalとし、GLM token削減のためにCodex/Sol消費を増やす案を採用しない
- 新規FindingをGo/No-Goし、Goは022より前の独立taskへ固定し、NEXT/BLOCKED全体を再優先付けする
- 完了時にさらに次のcheckpointを最大5 task完了以内へ再配置し、post-105最終再評価を変更しない

## Must not

- raw log、巨大JSON/JSONL、prompt/response全文をSol contextへ再投影しない
- Markdown確認のためにtracked Markdown全体を毎checkpoint再読したり、generic semantic checkerやLLM監視を追加したりしない
- unknown/ambiguousなCodex usageやQuality Deltaを改善扱いしない
- test/review/Sol gate省略をtoken削減として採用しない
- post-105最終再評価を完了・削除・前倒ししない

## Acceptance criteria

- 前回以後の評価期間、cohort、Codex usage coverage、Quality Delta proxy、exact source locatorをbounded reportにする
- 既存task coverageと未Task化候補を全件Go/No-Goし、Plan全体を再優先付けする
- Markdown初回監査完了後の各checkpointで、前回locatorからのMarkdown差分、未解決候補、Go/No-Go結果をbounded reportに含める
- 新規Go Findingを022より前へ追加し、重複は既存taskへlosslessに統合する
- 次回checkpointとpost-105最終再評価の双方がPlan上に存在する
- `--project-state`でschedule/dependency整合を確認する

## Historical invariants

- Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する

## Dependencies

none

## Review findings

none

## Current boundary

2026-09-05中間再評価後、Plan上で先行する最大5 task完了後にACTIVE化する。
