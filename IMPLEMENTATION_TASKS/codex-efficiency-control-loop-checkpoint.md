# Task: Codex efficiency control-loop checkpoint

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

### 2026-09-06

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

### 2026-09-08

````text
あとローテーションのしすぎによるトークン過剰消費の可能性を次回の評価時に行え
````

## Resolved references

- 前回checkpointのbounded reportとpriority decisionは、`codex-efficiency-reevaluation-checkpoint.md`を削除する直前のGit locatorから回収する
- 次回は`telemetry-history-compact-summary.md`、`watch-terminal-error-orphan-exit.md`、`markdown-derived-state-authority-audit.md`、`external-review-a70d35c-43e1da9-follow-up.md`、`auto-resume-heartbeat-transaction.md`の最大5 task完了後に実行する。false-complete、正規復旧不能、大きな重複model消費があれば前倒しする
- `markdown-derived-state-authority-audit.md`完了後は、同taskが残す初回Git locator以後のtracked Markdown差分と未解決authority候補だけを確認する
- `post-105-codex-efficiency-reevaluation.md`は105完了後・022直前の最終safety netとして別に維持する
- session rotationの評価はrotation回数、trigger理由、taskあたりparent turn / tool output / token、rotation直後のauthority/bootstrap再投影量、cache/read attribution、同一task継続時との比較可能性を対象にする

## Purpose

優先配置した5 taskのCodex ReductionとQuality Deltaを短いfeedback loopで再評価し、022前のtask coverageとpriorityを補正する。

## External feasibility

status: not-applicable

## Contract

- 親Codexだけが追加AI callなしで実行し、GLM modelへ分析・採否・priority判断を委譲しない
- parent-only例外は既存evidenceの評価・採否・Plan priority更新までに限定する。新規taskの開始、decision、fix、accept、resumeは通常の`glm-parent-action`を使い、評価を契機にしたsource/test/config修正へ直接実行権限を拡張しない
- 前回checkpointのGit locator以後のCodex/GLM telemetry、parent usage、review/fix/validation、停止/recovery、未Task化Findingを既存bounded machine projectionで比較する
- Markdown初回監査が完了していれば、そのGit locator以後のtracked Markdown差分と未解決authority候補だけを確認する
- session rotationが過剰な頻度で発生し、再bootstrap・authority再投影・cache loss・parent finalization分断によってCodex tokenを増やしていないかをbounded evidenceで評価する
- Direct Codex対Codex + glm-workerのCodex ReductionとQuality Deltaを最上位Evalとし、GLM token削減のためにCodex/Sol消費を増やす案を採用しない
- 新規FindingをGo/No-Goし、Goは022より前の独立taskへ固定し、NEXT/BLOCKED全体を再優先付けする
- 完了時にさらに次のcheckpointを最大5 task完了以内へ配置し、post-105最終再評価を変更しない

## Must not

- raw log、巨大JSON/JSONL、prompt/response全文をSol contextへ再投影しない
- tracked Markdown全体を毎checkpoint再読したり、generic semantic checkerやLLM監視を追加したりしない
- unknown/ambiguousなCodex usageやQuality Deltaを改善扱いしない
- test/review/Sol gate省略をtoken削減として採用しない
- post-105最終再評価を完了・削除・前倒ししない

## Acceptance criteria

- 前回以後の評価期間、cohort、Codex usage coverage、Quality Delta proxy、exact source locatorをbounded reportにする
- 既存task coverageと未Task化候補を全件Go/No-Goし、Plan全体を再優先付けする
- Markdown初回監査完了後は、前回locatorからのMarkdown差分、未解決候補、Go/No-Go結果をbounded reportに含める
- rotationあり/なしの比較可能性、rotation頻度、trigger別件数、rotation前後のparent usageを示し、証拠不足はunknownのまま扱う
- 新規Go Findingを022より前へ追加し、重複は既存taskへlosslessに統合する
- 次の中間checkpointとpost-105最終再評価の双方がPlan上に存在する
- `--project-state`でschedule/dependency整合を確認する

## Historical invariants

- Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する

## Dependencies

none

## Review findings

none

## Current boundary

先行する最大5 taskの完了後にACTIVE化する。incident前倒し条件が成立した場合はPlan priorityを親Codexが再評価する。
