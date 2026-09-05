# Task: 105後のCodex効率・品質再評価

## Original instruction

````text
タスクに積むタイミングはCuurentタスク終わってからでいいが、105までやったらそこまでのCodexのトークン消費等を再評価するタスクを行え
このセッションの最初で行ったのと同じものだ
そしてそこでFindingsがあれば新しく022より前のタスクとして積んで作業サイクルを続けるようにしろ
もう一度いうが最重要はSol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。
````

## Amendments

- 2026-09-03 user continuation requirement:

````text
さっきの「意味のある停止」というのが何なのか知らないが、そういうのを改善したほうがいいと思うなら随時タスクに積んでいくように
今後も作業中に改善要素を見つけたら随時タスクに積むように
022の前での再評価タスクでも改めてタスクに積むべきものがなかったか精査するように
````

### 2026-09-04 external review PR 345

````text
CodeRabbit: Document that the parent-only evaluation is a bounded exception to codex/AGENTS.md, limited to after 105-session-rotation and before 022, using only approved read-only commands and mechanical projections for data collection while reserving all semantic investigation, Quality Delta assessment, and Go/No-Go decisions for the parent Codex. Explicitly state whether glm-parent-action remains mandatory for any follow-up task actions.

CodeRabbit: Define the comparable cohort identity and required snapshot fields for Codex Reduction and Quality Delta reporting, covering TaskID, SpecSHA256, SessionID, and relevant run conditions before using ResolveFromTaskStats. Specify how mismatched or missing identity fields are classified, and only add duplicate-match handling when evaluation inputs may contain duplicate task records.
````

### 2026-09-04 prose-only control reevaluation boundary

````text
post-105-codex-efficiency-reevaluation.md
で監査するものにそういうものは含まれているか
あまりこのタスクの範囲を増やしすぎて監査がゆるくならないようにはしろ
````

## Resolved references

- 「このセッションの最初で行ったのと同じもの」は、GLM-Worker telemetry、Codex自身の `~/.codex/` 等に残るログを親Codexが直接解析し、目的を改善するtask候補とログ収集command拡充の要否を判断した2026-09-03の調査を指す
- 調査・意味判断をGLM modelへ委譲しない。`glm-worker` commandが機能するか、何を出力するかの確認とread-only projectionの利用は許可する
- 「意味のある停止」は、Task corpus閉包実装が品質ポリシー面を変更したため、GLM自身による品質基準の弱体化を防ぐguardがCodex reviewを要求した状態を指す。この個別停止は意図した安全境界として維持し、反復costや品質影響の新証拠が得られた場合だけ改善候補として再評価する
- PR 345 CodeRabbit comments `3930465346` / `3930465350`: `https://github.com/shinderuman/codex-worker-orchestrator/pull/345#discussion_r3930465346`, `https://github.com/shinderuman/codex-worker-orchestrator/pull/345#discussion_r3930465350`
- 「そういうもの」は、自由言語だけに依存するcontrolの残存・再導入、機械化済みcontrolのprose thinning、改善候補の捕捉とTask化の機械化を指す
- 全completed task/current treeを再監査する責務は`prose-only-control-enforcement-audit.md`、機械化とinstruction削減はその後続taskが持つ。本taskはそれらを再実装・再分類せず、完了後から105までの回帰と実測効果だけを再評価する

## Purpose

105までの改善を実測で再評価し、Sol High相当の品質を維持したままCodex / Sol側の実消費量をさらに削減できる実行可能なFindingを022前に回収する。

## External feasibility

status: not-applicable

## Contract

- `105-session-rotation.md`完了後、022開始前に親Codex自身が実行するparent-only evaluationとする。GLM modelへ調査・分析・採否判断を委譲しない
- 本評価は`codex/AGENTS.md`の通常委譲原則に対する、105完了後から022開始前だけのbounded exceptionとする。データ収集は既承認のread-only commandとmachine projectionだけを使い、repository探索、semantic investigation、Quality Delta評価、Go/No-Goは親Codexが行う
- 本評価から新しい実装taskを作った後のstart/decision/fix/accept/resumeは通常規則どおり`glm-parent-action`を必須とし、parent-only exceptionをfollow-up implementationへ拡張しない
- GLM-Worker telemetry、bundle/analysis artifact、Codex rollout/session/archived-session等の `~/.codex/` 配下ログを、追加AI callなしのread-only commandと機械projectionを優先して解析する
- Direct CodexとCodex + glm-workerについて、親Codex / Sol側の実token消費を第一指標、Quality Deltaを同格の最上位gateとして比較する。GLM token消費は補助指標とし、GLM token削減のためにCodex tokenやSol判断回数が増える案を採用しない
- `ResolveFromTaskStats`のTaskIDはstats archive join keyとしてだけ使い、比較cohort identityの代用にしない。Codex Reduction / Quality Deltaの比較は同一`SpecSHA256`と同一の関連run conditions（repository/source snapshot、model・reasoning、評価入力・quality gate条件）を要求する。`SessionID`は独立run identityとして保持し、arm間一致を要求せず同一sessionの重複計上を禁止する
- 必須identity/snapshot/run-conditionが欠けるrecordはunknown、値が異なるrecordは別cohort、同じTaskIDまたはSessionIDに複数候補があり一意に解決できない入力だけをambiguousとして除外する。通常state storeのtask ID一意archiveへ不要なduplicate fallbackを追加しない
- parent attribution、history cohort query、timeline retention fallback、review gap、validation gate、packet correction、session rotationまでの新しい観測面を使い、開始時調査と同じ論点を再評価する
- 105までの各taskで発生した停止、retry、fix、review、validation、parent return、過大outputと、作業中に検討したがTask化しなかった改善候補を棚卸しし、022前の取りこぼしがないか再精査する
- 先行auditのbounded control inventory、continuous improvement candidate/disposition state、mechanized-control registry、installed instruction size/token proxyを直接入力にし、audit完了後から105までに新しい`prose-only`/`partial` controlが導入されていないか、machine guardを自由言語で補っただけのfalse completionが再発していないか、instruction削減がCodex消費とQuality Deltaへどう影響したかだけを確認する
- prose-only controlの全repository/Git履歴監査は再実行しない。先行inventory以後に変更されたcontrol、runtimeで違反・拒否・未処理candidateが観測されたcontrol、source locatorがstaleになったcontrolへ対象を限定する
- 品質はreview outcome、escaped defect、validation/retry、Acceptance充足、原因不明failure等の観測可能なproxyで評価し、token削減だけを成功扱いしない
- 現行commandで判断に必要なfield/source locatorが不足する場合は、ログ収集・projection command拡充の必要性をCodexが判断する
- 実行可能なFindingがあればparent-managed Task fileとして具体的なContract / Must not / Acceptance criteriaを定義し、022より前のNEXTへ追加して作業サイクルを継続する。FindingごとにGo/No-Goを決め、未確定状態のまま実装へ進めない
- 実行可能なFindingがなく、既存BLOCKEDの判断を変えるfresh evidenceもなければNo-Go理由と観測限界を確定して022へ進む

## Must not

- 調査・semantic judgment・Quality Delta評価をGLM modelへ委譲しない
- GLM token節約をCodex / Sol token増加と交換しない
- raw prompt、raw response、tool result全文、巨大JSON/JSONLをSol-visible stdoutへ再投影しない
- attribution不能値、counter reset、複数rollout候補、品質proxy欠損を便宜的に合算・成功扱いしない
- Findingだけを理由に既存Taskへ無関係な変更を混ぜず、022開始後へ改善を先送りしない
- 先行prose-only auditを本task内で最初から繰り返し、広いが浅い再監査にしない

## Acceptance criteria

- Direct Codex対Codex + glm-workerのCodex ReductionとQuality Deltaを、比較可能なcohort、unknown理由、exact source locator付きで報告できる
- Codex / Sol側のinput/cached input/output/reasoning/total、turn/tool/output bytes/compaction等、利用可能な実消費fieldを区間別に評価できる
- 105までの各改善について、期待したCodex削減、品質維持、退行、観測不能を区別して判断できる
- ログ収集・projection command拡充の要否と、必要なら不足field・取得境界・stdout boundednessを具体化できる
- Findingsがあれば全件をGo/No-Go判定し、GoのTaskをPlan上で022以前へ追加する。追加Task完了後も必要なら本評価を再実行するか、同等の再評価gateを最後の追加Taskへ明記する
- 作業中に随時Task化されたFindingと、Task化しなかった候補の双方をsource locator付きで照合し、後者に未評価または根拠不足のまま放置された実行可能Findingがないことを確認する
- audit baseline以後のcontrol delta、未処理candidate、機械guard違反、registry/locator driftだけを対象にprose-only回帰を確認し、全履歴再走査なしで対象件数・判定・exact locatorを報告できる
- instruction thinning前後のinstalled byte/token proxyと、rule miss・retry・parent return・Quality Delta proxyを比較し、文字数削減だけを成功扱いしない
- Findingsがなければ根拠付きで完了し、022以外の実行可能なunblocked taskが残っていないことを確認する

## Historical invariants

- 最上位目的はSol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減すること
- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta
- GLM token消費節約の優先度はCodex token削減より低い

## Dependencies

- `IMPLEMENTATION_TASKS/105-session-rotation.md`
- `IMPLEMENTATION_TASKS/mechanized-control-prose-thinning.md`
- 105より前にPlan上で実行するCodex telemetry改善taskがすべて完了していること

## Review findings

none

## Current boundary

105完了後まで開始禁止。実行時は親Codex自身が評価する。
