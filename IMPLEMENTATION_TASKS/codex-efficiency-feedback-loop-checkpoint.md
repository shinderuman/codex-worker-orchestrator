# Task: Codex efficiency feedback-loop checkpoint

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

- 前回checkpointのbounded reportとpriority decisionは、`codex-efficiency-control-loop-checkpoint.md`を削除する直前のGit locatorから回収する
- 次回は`task-stats-revision-consumer-audit.md`完了後に実行する。false-complete、正規復旧不能、大きな重複model消費があれば前倒しする
- Markdown差分は前回checkpointのGit locator以後に限定し、未解決authority候補だけを確認する
- `post-105-codex-efficiency-reevaluation.md`は105完了後・022直前の最終safety netとして別に維持する
- session rotationの評価はrotation回数、trigger理由、taskあたりparent turn / tool output / token、rotation直後のauthority/bootstrap再投影量、cache/read attribution、同一task継続時との比較可能性を対象にする

## Purpose

task stats revision consumer audit後のCodex ReductionとQuality Deltaを短いfeedback loopで再評価し、022前のtask coverageとpriorityを補正する。

## External feasibility

status: not-applicable

## Contract

- 親Codexだけが追加AI callなしで実行し、GLM modelへ分析・採否・priority判断を委譲しない
- parent-only例外は既存evidenceの評価・採否・Plan priority更新までに限定する。新規taskの開始、decision、fix、accept、resumeは通常の`glm-parent-action`を使い、評価を契機にしたsource/test/config修正へ直接実行権限を拡張しない
- 前回checkpointのGit locator以後のCodex/GLM telemetry、parent usage、review/fix/validation、停止/recovery、未Task化Findingを既存bounded machine projectionで比較する
- 前回checkpoint locator以後のtracked Markdown差分と未解決authority候補だけを確認する
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
- 前回locatorからのMarkdown差分、未解決候補、Go/No-Go結果をbounded reportに含める
- rotationあり/なしの比較可能性、rotation頻度、trigger別件数、rotation前後のparent usageを示し、証拠不足はunknownのまま扱う
- 新規Go Findingを022より前へ追加し、重複は既存taskへlosslessに統合する
- 次の中間checkpointとpost-105最終再評価の双方がPlan上に存在する
- `--project-state`でschedule/dependency整合を確認する

## Historical invariants

- Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する

## Dependencies

none

## Bounded evaluation report

- 評価期間は前回checkpoint完了commit `968c3c8df20f7c96393d230e1b2b7f12a8b8d9fc` の2026-09-16T12:11:08+09:00からcurrent HEAD `8f1322dbc9119e21e502cc521a21a7cfec7d2d3e` の2026-09-16T12:59:40+09:00まで。telemetry cohortの実task開始範囲は2026-09-16T03:40:27Zから03:57:27Z、1 task、3 model calls（worker 2 / reviewer 1）、total prompt 2,598,953 tokens、output 41,139 tokens、top-level turns 87。locatorは`glm-worker --stats --compact --since 2026-09-16T12:11:08+09:00`とtask stats `7ef3e6fb-7421-4beb-80e8-f638be1cc0b2`
- GLM usage coverageは3 task calls中turns / duration / usageが各3、missing usage 0、malformed 0でcomplete。parent usage coverageは1 task中available 1、ambiguous 0、unknown 0で100%だが、compact `parent_usage`はcoverage countだけでtoken / activity aggregateを返さない。Direct Codex対Codex + glm-workerのCodex Reductionは量的比較不能でunknownを維持する
- Quality Delta proxyは`NEEDS_SOL_DECISION` 1、`NEEDS_SOL_REVIEW` 1、reviewer call 1、fix command 0、parent outcomes accepted 1 / decision 1、accepted risk HIGH 2。旧archive汎用互換をNo-Goにし、silent skip可観測化だけを独立task化したため、quality gate削減を支持する証拠はない。rate limit / provider unavailable / packet compactionは0
- rotationは前taskから継承したpending directive 1件をcurrent threadへbindしてからtaskを開始し、task内の追加rotationは0。継承directiveのreasonは`repeated-events`で、high-risk / compaction / model-turns / tool-output-bytes / repeated-eventsが各1 triggerとして記録された。旧thread claimが必要でcurrent threadからのclaimは拒否され、外部messageによる復旧を要したため、開始preflightの正規化は既存`session-rotation-continuation-preflight.md`でGoとする。rotationなしcontrolとparent token totalがないため、rotation頻度自体の増減寄与はunknownでありtrigger調整はNo-Go
- Markdown差分は4 files。Plan schedule同期、完了audit task削除、`session-rotation-continuation-preflight.md`と`task-stats-archive-skip-observability.md`の追加だけで、branch / HEAD / dirty state / ordinary completion ledgerの新規mirrorと未解決authority候補は0件。locatorは`git diff 968c3c8df20f7c96393d230e1b2b7f12a8b8d9fc..8f1322dbc9119e21e502cc521a21a7cfec7d2d3e -- '*.md'`
- Go: `session-rotation-continuation-preflight.md`を第1優先にする。反復指摘と実際の開始阻害があり、rotation回数調整ではなくclaim / bind / start admissionの正規化で重複parent操作とfalse startを防ぐ
- Go: `parent-usage-compact-token-totals.md`を第2優先に追加する。前回と今回の2 checkpointでparent usage token total欠落により最上位Evalがunknownのままであり、既存per-task parent usage reportのavailable intervalをbounded aggregateへ投影する必要がある
- Go: `task-stats-archive-skip-observability.md`を第3優先に維持する。旧data受理は行わず、silent skipをcomplete coverageと誤認するQuality Delta riskだけを解消する
- Go: 次回中間評価を`codex-efficiency-intermediate-checkpoint.md`として上記3 taskの直後へ配置する。`post-105-codex-efficiency-reevaluation.md`は105後・022直前の最終safety netとして維持する
- No-Go: rotation trigger頻度調整、review/test/Sol gate削減、旧stats archive互換、単発のparent evidence accept失敗とcompletion guard returnの個別task化。比較証拠不足、quality維持への反証、または既存fail-closed recoveryで収束しており、新しい独立責務を証明しない
- BLOCKED / USER_PERMISSION_WAITの8 taskは新しい許可・外部成立性・比較cohortを得ていないため状態と順序を維持する
