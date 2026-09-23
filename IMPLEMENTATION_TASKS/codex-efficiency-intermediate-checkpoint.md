# Task: Codex efficiency intermediate checkpoint

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

### 2026-09-22

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

````text
glm-workerの責務がリポジトリのルールを実装してないか
逆はないかとか総合的に考えてほしい
````

### 2026-09-22 追加の横断レビュー

````text
他に総合チェック的な観点はないのか
````

````text
じゃあ全部やれ
````

### 2026-09-22 再開

````text
作業再開して
````

### 2026-09-22 作業経路と完了報告の訂正要求

````text
いやPRというかGitHubをお前に作っていいなんて言ってない
全部Mainでやりきれ
そして本当に総合レビューやりきったのか
````

### 2026-09-22 レビューと計画化を先行

````text
トークンを無駄にするな
Git作業はいらない
Main上でまずレビューを全部やれ
````

````text
Guard通らなくてもいいからレビューしてIMPLEMENTATION_PLANSにするのがお前のまず最初のゴールだ
````

### 2026-09-23 Finding保存の優先と計画化・Pushの要求

````text
レビューは全部終わったの？
レビューを全部やってそれをそれぞれIMPLEMENTATION_PLANSにするのがゴールって意味だったんだが
一番やってほしいのはレビューかつそれの記録を何でもいいから残すことだ
お前が書いてるReview.mdでもいい
この作業はLimitで中断されて後の作業を別のモデルにやらせる可能性がある
後の作業とはそこまでのレビュー結果をコミットするだけという意味だ
レビュー自体は他のモデルにやらせない、性能が低いモデルで発見してもいみないから
なのでお前はFindingを1つでも残すことが最上位目的だ
トークンに余裕があればお前にFindingのドキュメント化もさせるがとりあえずそれは後でいい
````

````text
じゃあPushしておいてくれ
````

````text
Pushしろとは言ったがいまの作ったファイルをそのままでいいとは言ってない
ちゃんと全部IMPLEMENTATION_PLANSにしておいてくれ
最初にそう言ったろ
````

### 2026-09-23 Lintと意味保存の要求

````text
そもそもなんでLint通過させてないの
GLM使っていいからちゃんとやれよ
git reset --soft HEAD^^ を俺の方で実行して巻き戻した
コミットまでお前がやれ
Pushは俺がやる
GLMが作業した場合記載した内容の意味が変わってないかはお前がちゃんと確認しろ
````

## Resolved references

- 2026-09-23の最新指示では、保存済み全Findingの個別Task化と改善候補の採否・評価先を既存Planへ反映する。最新追加指示によるGit操作は親Codexがcommitまで行い、Pushはユーザーが行う。単にReview.mdと再現fileをそのままPushするだけでは満たさない。名称IMPLEMENTATION_PLANSは既存の`IMPLEMENTATION_PLAN.local.md`と`IMPLEMENTATION_TASKS/`を指し、別の計画正本を作らない。production修正・新規レビュー委譲・PR作成は含まない。

- 上記は追加要求として原文を保存する。以降はmainで作業し、新規PRや作業ブランチを作らない。GitHubの保護設定は変更しない。レビュー完了の主張は実際のcoverageと残る未検証事項に限定する。

- 「全部」は直前に提示した12観点、すなわち正常taskの親介入、機械化の費用対効果、状態の正本と重複、境界間整合性、中断・再実行・並行実行、証拠の有効範囲、要求保存と契約の乖離、テストの証明力、source/runtime整合、権限とdata境界、rule増殖と廃止、実消費の帰属をすべてレビューする指示。GLM不使用を継続し、確認済み不具合・改善候補・未検証事項を区別する。レビューであり修正実装の開始を意味しない。

- 2026-09-22の総合レビューは親Codex自身がGLMを使用せず実施する。実装変更や既存ACTIVEの実行を含めず、現行schema限定・後方互換性禁止を含む適用規則に照らして問題とCodex Reduction施策を評価する。

- 前回checkpointのbounded reportとpriority decisionは、`codex-efficiency-feedback-loop-checkpoint.md`を削除する直前のGit locatorから回収する
- 次回は`session-rotation-continuation-preflight.md`と`parent-usage-compact-token-totals.md`の完了、およびtask-stats archive skip observabilityに対応するcurrent-tree changeの統合後に実行する。false-complete、正規復旧不能、大きな重複model消費があれば前倒しする
- Markdown差分は前回checkpointのGit locator以後に限定し、未解決authority候補だけを確認する
- `post-105-codex-efficiency-reevaluation.md`は105完了後・022直前の最終safety netとして別に維持する
- session rotationの評価はrotation回数、trigger理由、taskあたりparent turn / tool output / token、rotation直後のauthority/bootstrap再投影量、cache/read attribution、同一task継続時との比較可能性を対象にする

## Purpose

優先配置した3 task後のCodex ReductionとQuality Deltaを短いfeedback loopで再評価し、022前のtask coverageとpriorityを補正する。

## External feasibility

status: not-applicable

## Contract

- 保存済み総合レビューと計画化成果物はLintを通す。最新追加指示により、この整備・検証にはGLM利用を許可するが、Findingの意味・影響・限界・修正責務・後方互換性禁止を保持し、GLM変更の意味保存と最終採否は親Codexが確認する。以前のGLM不使用・Guard非必須は総合レビュー当時の境界であり、整備・検証にはこの追加指示を適用する。

- 親Codexだけが追加AI callなしで実行し、GLM modelへ分析・採否・priority判断を委譲しない
- parent-only例外は既存evidenceの評価・採否・Plan priority更新までに限定する
- 2026-09-22の総合レビュー指示に限り、上記の既存evidence限定を拡張し、親Codexによるsource読取、対象を限定した既存test、一時fixtureでの再現検証を含める。production変更・本番model実行は含めない。
- 前回checkpointのGit locator以後のCodex/GLM telemetry、parent usage、review/fix/validation、停止/recovery、未Task化Findingを既存bounded machine projectionで比較する
- 前回locator以後のtracked Markdown差分と未解決authority候補だけを確認する
- session rotationとparent finalizationがCodex token / tool outputを増やしていないかをbounded evidenceで評価する
- publication往復、rotation再読/cache、A/B測定粒度の個別評価は、それぞれ`publication-sequence-roundtrip-evaluation.md`、`parent-session-rotation-cost-evaluation.md`、`codex-usage-comparability-evaluation.md`へ分離する。本checkpointはそれらを再調査せず、結果と新しいdeltaからpriorityを判断する。比較可能な実消費がない段階でproduction変更や削減率を確定しない。
- Review.mdのF1〜F16は各々1つの修正責務としてTaskを持ち、再現資料はreview-evidenceへ保持する。改善2件（quality二重検査・022固有policy）も個別Taskを正とする。Review.mdを未Task化TODOの代用にしない。
- 任意secret用の新汎用検出器と全state DB統合は、今回の不具合の直接修復でなく費用対効果の証拠もないため不採用。review回数削減・impact test選択は既存BLOCKEDの許可・品質条件を維持し、System-Oneは既存ACTIVEのshadow契約を維持する。
- Direct Codex対Codex + glm-workerのCodex ReductionとQuality Deltaを最上位Evalとし、GLM token削減のためにCodex/Sol消費を増やす案を採用しない
- 新規FindingをGo/No-Goし、Goは022より前の独立taskへ固定し、NEXT/BLOCKED全体を再優先付けする
- 完了時にさらに次のcheckpointを最大5 task完了以内へ配置し、post-105最終再評価を変更しない

## Must not

- raw log、巨大JSON/JSONL、prompt/response全文をSol contextへ再投影しない
- tracked Markdown全体を再読したり、generic semantic checkerやLLM監視を追加したりしない
- unknown / ambiguousなCodex usageやQuality Deltaを改善扱いしない
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
