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
- `continuous-improvement-task-capture.md`は2026-09-10のarchitecture auditでdurable candidate state machine・admission blockingを不採用とし、随時捕捉はRules「parent orchestrationのproduct化判断」恒久契約と本checkpointの取りこぼし再精査へ移管して削除した

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

## Bounded evaluation report

- 評価期間は前回checkpoint完了commit `1cc47a16f23482458f8a0bf3162169182461ff71` の2026-09-08T05:30:38+09:00からcurrent HEAD `ea86079d7b2b037364c37bfed25a18e6c7ace6ec` の2026-09-16T11:55:10+09:00まで。telemetry cohortの実task開始範囲は2026-09-10T11:29:59+09:00から2026-09-16T06:14:31+09:00、5 task、37 model calls（worker 25 / reviewer 12）、total prompt 165,879,727 tokens、output 626,320 tokens、top-level turns 1,171。query locatorは`glm-worker --stats --since 2026-09-08T05:30:38+09:00`、telemetry locatorは`/Users/shinderumanm/.glm-worker/sessions/4b1083bd6f6e13220f3e0d653377d694f010b8951c788559f19840a14a0df6d0/telemetry`
- GLM usage coverageは37 stats calls / 37 raw records、missing 0、excess 0、orphan 0で`complete`、usage totalsはknown。parent usage coverageは5 task中available 3、open由来unknown 2で60%。bounded projectionはparent token totalを返さないため、Codex usage量とDirect Codex対Codex + glm-workerの同一cohort A/Bはunknownのままとする。locatorは`glm-worker --call-outliers history --since 2026-09-08T05:30:38+09:00 --compact`
- Quality Delta proxyは`NEEDS_SOL_DECISION` 4、`NEEDS_SOL_REVIEW` 8、reviewer calls 12、fix commands 4、auto-fix rounds 6、parent outcomes accepted 3 / decision 3 / fix 4 / unknown 2、parent fix originはcodex-review 3 / glm-reviewer 1。packet rejectはmissing-field 2 / size 2、snapshot mismatch 0、provider unavailable 0。quality issueを実際に捕捉した区間であり、review/test/Sol gate削減を支持しない
- rate limit 5、resume 4。`auto-resume-heartbeat-transaction.md`はcommit `fa179712c53dc6a503e4ee13791da1f3e5ee5bf3`で完了し、その後のcurrent cohortに新しいfalse-completeまたは正規復旧不能の一次証拠はないため、追加のrecovery taskはNo-Goとする
- rotationは4 taskに記録されたparent Codex session IDが3種で、時系列上2回のsession ID変更を観測した。残る1 taskはparent session link欠損のため正確なrotation回数はunknown。current handoffには未実行のrotation directive 1件があり、reasonは`repeated-events`、trigger evidenceはhigh-risk 1、compaction 1、model-turns 1、tool-output-bytes 1、repeated-events 1。rotationなしcontrolとrotation前後のparent token totalがないため有無比較と増減寄与はunknownであり、rotation調整taskはNo-Goとする。locatorは各task statsの`parent_codex_session_id`と`glm-worker --handoff`の`session_rotation`
- Markdown初回監査locator `0dc7a50fd36ce7a80a2307373ad5193b140ee82e`からcurrent HEADまでのtracked Markdown差分は26 files。`git diff --numstat 0dc7a50fd36ce7a80a2307373ad5193b140ee82e..ea86079d7b2b037364c37bfed25a18e6c7ace6ec -- '*.md'`と同範囲のadded-line確認では、規範contract、exceptional History decision、canonical locatorの追加だけを確認し、手動branch / HEAD / dirty state / ordinary completion chronologyの新規mirrorと未解決authority候補は0件。追加Markdown修正taskはNo-Goとする
- Go: `task-stats-revision-consumer-audit.md`を第1優先に維持する。current revision cohortはcompleteでもparent usage 2/5がopen、旧v3 revision archiveのconsumer別扱いは未確定であり、silent skipとcoverage意味をconsumer単位で判断する既存taskが重複なくcoverする
- Go: 次回中間評価を`codex-efficiency-feedback-loop-checkpoint.md`として同audit直後へ配置する。`post-105-codex-efficiency-reevaluation.md`は105後・022直前の最終safety netとして維持し、`022-final-verification.md`より後ろへ移動しない
- No-Go: telemetry historyの追加修正、session rotation調整、review/test/Sol gate削減、model routing、compaction threshold、実Sol A/Bの新規task化。telemetry coverageはcomplete、rotationとA/Bは比較不能、quality proxyはgate縮小を支持せず、残りは既存BLOCKED taskまたは既存auditでcoverageされる
- BLOCKED / USER_PERMISSION_WAITの8 taskは新しい許可・成立性・quality比較を得ていないため順序と状態を維持する。新規Go Findingは0件、既存taskへの新しいsemantic amendmentも0件
