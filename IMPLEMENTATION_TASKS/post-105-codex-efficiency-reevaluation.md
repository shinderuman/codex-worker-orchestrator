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

## Resolved references

- 「このセッションの最初で行ったのと同じもの」は、GLM-Worker telemetry、Codex自身の `~/.codex/` 等に残るログを親Codexが直接解析し、目的を改善するtask候補とログ収集command拡充の要否を判断した2026-09-03の調査を指す
- 調査・意味判断をGLM modelへ委譲しない。`glm-worker` commandが機能するか、何を出力するかの確認とread-only projectionの利用は許可する
- 「意味のある停止」は、Task corpus閉包実装が品質ポリシー面を変更したため、GLM自身による品質基準の弱体化を防ぐguardがCodex reviewを要求した状態を指す。この個別停止は意図した安全境界として維持し、反復costや品質影響の新証拠が得られた場合だけ改善候補として再評価する

## Purpose

105までの改善を実測で再評価し、Sol High相当の品質を維持したままCodex / Sol側の実消費量をさらに削減できる実行可能なFindingを022前に回収する。

## External feasibility

status: not-applicable

## Contract

- `105-session-rotation.md`完了後、022開始前に親Codex自身が実行するparent-only evaluationとする。GLM modelへ調査・分析・採否判断を委譲しない
- GLM-Worker telemetry、bundle/analysis artifact、Codex rollout/session/archived-session等の `~/.codex/` 配下ログを、追加AI callなしのread-only commandと機械projectionを優先して解析する
- Direct CodexとCodex + glm-workerについて、親Codex / Sol側の実token消費を第一指標、Quality Deltaを同格の最上位gateとして比較する。GLM token消費は補助指標とし、GLM token削減のためにCodex tokenやSol判断回数が増える案を採用しない
- parent attribution、history cohort query、timeline retention fallback、review gap、validation gate、packet correction、session rotationまでの新しい観測面を使い、開始時調査と同じ論点を再評価する
- 105までの各taskで発生した停止、retry、fix、review、validation、parent return、過大outputと、作業中に検討したがTask化しなかった改善候補を棚卸しし、022前の取りこぼしがないか再精査する
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

## Acceptance criteria

- Direct Codex対Codex + glm-workerのCodex ReductionとQuality Deltaを、比較可能なcohort、unknown理由、exact source locator付きで報告できる
- Codex / Sol側のinput/cached input/output/reasoning/total、turn/tool/output bytes/compaction等、利用可能な実消費fieldを区間別に評価できる
- 105までの各改善について、期待したCodex削減、品質維持、退行、観測不能を区別して判断できる
- ログ収集・projection command拡充の要否と、必要なら不足field・取得境界・stdout boundednessを具体化できる
- Findingsがあれば全件をGo/No-Go判定し、GoのTaskをPlan上で022以前へ追加する。追加Task完了後も必要なら本評価を再実行するか、同等の再評価gateを最後の追加Taskへ明記する
- 作業中に随時Task化されたFindingと、Task化しなかった候補の双方をsource locator付きで照合し、後者に未評価または根拠不足のまま放置された実行可能Findingがないことを確認する
- Findingsがなければ根拠付きで完了し、022以外の実行可能なunblocked taskが残っていないことを確認する

## Historical invariants

- 最上位目的はSol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減すること
- 最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta
- GLM token消費節約の優先度はCodex token削減より低い

## Dependencies

- `IMPLEMENTATION_TASKS/105-session-rotation.md`
- 105より前にPlan上で実行するCodex telemetry改善taskがすべて完了していること

## Review findings

none

## Current boundary

105完了後まで開始禁止。実行時は親Codex自身が評価する。
