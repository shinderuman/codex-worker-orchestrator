# Task: Codex efficiency intermediate checkpoint

## Original instruction

```text
post-105-codex-efficiency-reevaluation.mdを定期的にやってほしいんだが奥に追いやられて一向に行われないので同様のタスクをなるべく早いタイミングでやってくれ
いまのpost-105-codex-efficiency-reevaluation.md自体は022の直前に残す
だが現段階の再評価をしてほしい
そして再評価したあとにタスクの優先度つけ直しをしてほしい
```

## Amendments / historical authority

[改訂前の全文（ユーザー原文・日付別Amendment）](https://github.com/shinderuman/codex-worker-orchestrator/blob/c888488bfda8159c48571ba87f3c9b706c2aeedf/IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md)をimmutable sourceとし、以下は実行時に必要な要求の整理であって原文の置換ではない。

- 2026-09-05〜08: Markdown差分、Codex token異常消費（特にGLMリセット後の短期間）、session rotationによる再読・再構築コストを評価する。異常の指摘を過去全期間の無差別再調査へ拡張しない。
- 2026-09-22〜23: GLMを使わない親Codex自身の総合レビューを要求。12観点を網羅し、発見したF1〜F16および改善候補を証拠とともに個別Task化し、Lint・意味保存・Planへの反映を要求した。途中のmain直接作業、PR不要、commit/pushの分担、整備時のみのGLM利用許可は**当時の作業経路の記録**であり、現在のGit操作権限やこの評価のGLM利用許可にはしない。
- 2026-10: `review-evidence/october-2026/request.md` の再監査要求と `Review.md` の「2026-10 再監査」C2に従い、failure-path補助reviewのrepository固有path/token policy、controller移管後のcoverage、追加発見・誤検出・親再調査・model費用を評価する。GLM不使用、並行作業を避けた記録、PRは最終段階という当時の要求も原文で確認する。

## Resolved references

- 前回checkpointのbounded report・priority decision: `codex-efficiency-feedback-loop-checkpoint.md`削除直前のGit object。前回以降のsource・telemetry・Markdown差分に限定し、既存集計と個別Taskの成果を再利用する。
- 当初の前提: `session-rotation-continuation-preflight.md`、`parent-usage-compact-token-totals.md`、task-stats archive skip observabilityの統合後。false-complete、正規復旧不能、大きな重複model消費では前倒し。実際の着手順は現在のPlanを正とする。
- `Review.md` は12観点とF1〜F16・改善2件の根拠、`review-evidence/` は再現証拠、`IMPLEMENTATION_TASKS/` は実装要求、`IMPLEMENTATION_PLAN.local.md` はpriorityの正本。
- `post-105-codex-efficiency-reevaluation.md` は105完了後、022直前の**別の最終評価**として維持する。

## Purpose

Direct Codex対Codex + glm-workerのCodex ReductionとQuality Deltaを途中段階で再評価し、未解決FindingとTaskの優先度を根拠に基づいて補正する。評価・採否・計画化が対象であり、production実装は対象外。

## External feasibility

status: not-applicable

## Contract

- 親Codexのみが追加AI callなしで評価・採否・priority判断を行い、GLMへ委譲しない。原則は既存evidenceの再評価。2026-09の総合レビューのcoverage確認に必要な範囲のみ、親によるsource読取・既存test・一時fixtureを許すが、新しいproduction変更・本番model実行はしない。
- 前回locator以後のCodex/GLM telemetry、parent usage、review/fix/validation、停止・復旧、未Task化Findingを既存のbounded projectionで比較。期間、cohort、データ欠落、source locatorを明示し、Direct Codexとの比較可能性とQuality Deltaを区別する。
- 12観点（正常taskの親介入、機械化の費用対効果、状態正本・重複、境界整合、中断・再実行・並行実行、証拠scope、要求保存、テスト証明力、source/runtime整合、権限・データ境界、rule増殖・廃止、実消費の帰属）と `Review.md` のF1〜F16・改善2件について**既存Taskへのcoverageと未解決差分**を確認する。既に完了した総合レビューを初めからやり直さない。
- Session rotationは回数・trigger・親turn/token/tool output・bootstrap再投影・cache/read attribution・rotation有無の比較可能性を確認。publication往復、rotation再読、A/B比較粒度の詳細再評価は、それぞれ `publication-sequence-roundtrip-evaluation.md`、`parent-session-rotation-cost-evaluation.md`、`codex-usage-comparability-evaluation.md` のownerを尊重し、結果と新deltaのみ取り込む。
- 2026-10のC2は補助reviewの実効範囲・policy owner・対象coverageを既存cohortのQuality Delta、親負担、call費用に照らし、harnessへの責務移管・pilot終了・現状維持を採否する。未観測を「Findingなし」としない。通常reviewを維持し、`106-review-call-reduction.md` のBLOCKEDを解除しない。
- 新規FindingをGo/No-Go判定し、Goなら022前の独立Taskに配置、既存ownerとの重複はlosslessに統合する。NEXT/BLOCKED全体を再優先付けし、Lintと `--project-state` で整合を確認する。
- 任意secretの汎用検出器・全state DB統合は不採用。汎用設定framework、旧path alias、二重policy、補助review callの無条件増設を先行しない。review回数削減・impact test選択は既存BLOCKED条件、System-Oneは適用されるshadow契約を維持する。
- 完了時、次の中間checkpointを最大5 Task完了以内に配置する。022直前のpost-105最終評価は移動・削除しない。

## Must not

- raw log、巨大JSON/JSONL、prompt/response全文やtracked Markdown全体を親contextへ再投影しない。差分と未解決authorityだけ読む。
- generic semantic checker、LLM監視、旧schema互換、無根拠のfallbackを追加しない。後方互換性禁止など現行規則を守る。
- unknownな使用量・Quality Delta、対象未観測、比較不能なcohortを改善・削減達成と扱わない。GLM token節約のためにCodex消費を増やさない。
- test/review/Sol gateの省略や品質低下をtoken削減と扱わず、production実装や他TaskのBLOCKED解除を独断で始めない。

## Acceptance criteria

- 期間・cohort・Codex usage coverage・Quality Delta proxy・exact source locatorを示すbounded report。rotation前後の使用量・trigger・比較可能性、Markdown差分・未解決事項を含め、不明点を明示する。
- C2の対象coverage、追加発見・誤検出・費用、owner判断、harness移管／pilot終了／現状維持の根拠と限界を記録する。
- F1〜F16・改善2件・新規Findingのcoverage/Go-No-Goと重複統合を確認し、Plan全体の優先順位を更新する。意味保存・Lint・`--project-state` を確認する。
- 次の中間checkpoint（5 Task以内）とpost-105最終評価（022直前）の両方をPlanで確認する。

## Historical invariants

- Sol High相当の品質を保ちながらCodex / Sol消費を大幅に減らす。

## Dependencies

none
