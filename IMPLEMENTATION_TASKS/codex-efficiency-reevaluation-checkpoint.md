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

## Resolved references

- 2026-09-05の中間再評価完了後、最大5 task完了以内に再実行するcheckpoint
- `post-105-codex-efficiency-reevaluation.md`は105完了後・022直前の最終safety netとして別に維持する
- Markdown確認は`markdown-derived-state-authority-audit.md`の初回inventory完了後、このcheckpointへ固定項目として統合する。`post-105-codex-efficiency-reevaluation.md`の監査範囲は広げない
- 2026-09-06 06:30 JST時点でCodex 5h bucketは45%消費、weekly bucketは68%消費だった。ユーザー観測では同日のGLM rate-limit復帰時点で5h残量は100%だった
- 復帰後区間では、既知のauthority本文、review packet、handoff/finalization JSON、広いdiff/source出力を親model-visible contextへ繰り返し展開し、競合統合時に既review済み範囲も再検証した
- 本checkpoint中の既存task横断`rg`も17,690 token相当を生成し、8,000 tokenで切断された。検索対象を絞らずstdout上限だけを設定しても親Codex消費を防げない再現証拠である
- 長いsessionで細粒度のtool returnを多数発生させ、固定instruction・圧縮履歴・追加証拠を親model turnごとに再入力した。個別要因への厳密配賦は現行telemetryでは不能だが、parent turn増加が消費を乗算するため独立の抑制対象とする
- 2026-09-06の判断ではFindingを`parent-model-visible-evidence-projection.md`へGoとして切り出した。要求と採否の対象は同task fileのGit履歴から回収する

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

- 評価期間は前回checkpoint完了commit `20ab28a0238f670b6f6d584d79f56d59633fd5a3` の2026-09-05T15:06:38+09:00から2026-09-08T05:21:38+09:00まで。current telemetry cohortは6 task、43 model calls（worker 32 / reviewer 11）、prompt 231,701,803 tokens、output 957,726 tokens、top-level turns 1,846。query locatorは`glm-worker --stats --since 2026-09-05T15:06:38+09:00`、telemetry locatorは`/Users/shinderumanm/.glm-worker/sessions/4b1083bd6f6e13220f3e0d653377d694f010b8951c788559f19840a14a0df6d0/telemetry`
- usage coverageは43 stats calls / 43 raw recordsだがorphan file 89件により`incomplete`、usage totalsはunknown。focused outlier queryは48 task-call recordsでeligible outlier 0件だったが、coverage不足とpopulation ruleのため低コスト化の証拠にはしない
- 同期間はrate limit 7、resume 9、decision 6、fix 2、`NEEDS_SOL_DECISION` 5、`NEEDS_SOL_REVIEW` 7、PASS 0、parent outcomesはaccept 5 / decision 5 / fix 2、packet missing-field reject 2、snapshot mismatch 0、provider unavailable 0。Codex-review起点fix 2件を含むため、review/test/Sol gate縮小とmodel routing変更を支持するQuality Delta証拠はない
- 直前task `b4e2882b-3a5e-4e82-a01d-c14a9f9ee519` のparent usageは879,016 tokens、model turn 1、tool call/result各14、model-visible tool output 56,674 bytes、compaction 0。exact rollout locatorは`sessions/2026/09/08/rollout-2026-09-08T04-27-43-01a07d57-3791-7632-9b6a-4b0e687181f3.jsonl:50`から`:159`。parent finalizationは観測時点でopenのため全task parent totalはunknown
- Direct Codex対Codex + glm-workerの同一cohort A/Bは引き続きunknown。2026-09-08のlive 5h usageは40%、weekly usageは20%だったが、durable interval attributionがないため改善達成とは判定しない
- Go: `telemetry-history-compact-summary.md`を第1優先へ移し、raw history / outlier展開とtask別反復を1回のbounded projectionへ置換する。orphan coverageは同taskと後続`task-stats-revision-consumer-audit.md`で扱い、新規重複taskはNo-Go
- Go: `watch-terminal-error-orphan-exit.md`、`markdown-derived-state-authority-audit.md`、`external-review-a70d35c-43e1da9-follow-up.md`、`auto-resume-heartbeat-transaction.md`を次の4件とする。実事故、incremental Markdown checkpointの前提、既知quality finding、7 rate-limit / 9 resumeの順でCodex削減とQuality Deltaを両立する
- Go: `prose-only-control-enforcement-audit.md`以下の既存enforcement chain、`packet-validation-correction-recovery.md`、`structured-validation-gate-telemetry.md`はcoverageを維持する。今回の観測は責務追加ではなく既存taskでcoverされるため新規Finding taskは作らない
- No-Go: review/test省略、model routing、compaction threshold、実Sol A/Bはquality比較またはユーザー許可が不足するためBLOCKEDを維持する。packet compaction 0だけを閾値変更の根拠にしない
- 次回checkpointは最初の5 task完了後となる位置へ`codex-efficiency-control-loop-checkpoint.md`を追加し、`post-105-codex-efficiency-reevaluation.md`は022直前に維持する

## Current boundary

追加AI callなしのbounded再評価、全FindingのGo/No-Go、Plan再優先付け、次回checkpoint追加、dependency metadata修復を完了した。完了同期後は`telemetry-history-compact-summary.md`をACTIVEへ昇格する。
