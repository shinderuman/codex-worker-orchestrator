# Task: System-Oneの棚卸し・構造改善・実効性に基づく機能拡張

## Original instruction

2026-10-26 ユーザー指示（原文）:

````text
リファクタのタスクを積んでくれ
PR作るところまでやってくれ
Mergeはしない
優先順はお前が判断しろ
ただし2番目以降だ
````

## Amendments

2026-10-26 対象の明確化（原文）:

````text
System-oneのリファクタ、見直し、棚卸し、機能追加、などをやるタスクという意味だ
````

## Resolved references

- 「System-one」は以前のSystem-One campaignで構築・試験・導入判断した機能群全体を指す。直近のPerformance Taskだけの修正でも、リポジトリ全域の無限定なリファクタでもない。
- 現行ownerを調査する入口: `glm-worker/internal/shadoweval/`、`glm-worker/internal/app/shadow_eval.go`、`glm-worker/internal/parentactioncmd/observation_execute.go`、failure-path advisory、review/parent decisionへの接続、対応telemetry・test。列挙に限らずcurrent Gitの実呼出元・consumerから対象を確定する。
- 過去のSystem-One終端Git `44a88bcac46a811bcfbd293e5886f8f2a92fdf1c`、`Review.md`、historical audit #1179 / #1205、adoption No-Go #1176は証拠のlocator。Issueは実装要求の第二正本ではない。過去の14 Bundle再監査・adoption candidate 0 / actual Codex reduction 0の評価を、現行の効果が0だという断定へ読み替えない。
- 既存の関連・別責務: `IMPLEMENTATION_TASKS/analysis-model-call-event-classification.md`、`IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md`、`IMPLEMENTATION_TASKS/guardian-facade-review-effectiveness.md`、`IMPLEMENTATION_TASKS/observation-replay-fence-retention.md`、`IMPLEMENTATION_TASKS/observation-preparation-cancellation.md`、BLOCKEDの`106-review-call-reduction.md`、`104-test-impact-selection.md`、`103-compaction-threshold-change.md`。最新Planと実コードで既存ownerを再確認し、重複実装しない。

## Purpose

System-One機能群を現行の実装・利用実績・効果から総合的に見直す。不要な経路、重複owner、過剰な機械処理・モデル往復、影響が見えないshadow/production接続を整理し、品質を維持してCodexの実消費・実行コストを減らせる同一責務内のリファクタリングと機能改善を実施する。独立した大きな改修・新機能が必要なら具体的な後続Taskへ分割する。「監査報告だけ」や「コード追加だけ」で成功としない。

## External feasibility

status: not-applicable

## Contract

### 現行機能・設計・資産の棚卸し

- System-Oneの機能、historical shadow/No-Go、現在のproduction・shadow・test-only・不使用・廃止の区別を、current sourceから漏れなく整理する。shadow-eval/reference/cohort、evidence filter、failure-path advisory、親/Reviewer feedback、adoption gateと機械処理の実owner・producer・consumer・CLI・artifact・state・model callを追跡する。
- 各機能の責任主体（親Codexのsemantic judgment、GLM Worker/Reviewer、deterministic machine）、source/Task identity、継続・停止・復旧、quality boundary、使われていないadapter/fixture/guardと重複検証を具体化する。
- 「コードが存在する」「liveで呼ばれた」「品質を保ち負荷削減に効いた」を別々に評価する。既存の実装・修正・Audit findingと照合し、歴史的Issueを再起票・再実装しない。

### 実効性・全体コストの比較

- recent bundle / telemetry / review/parent decisionを優先して、モデル実呼出（非モデルeventを除外）、Codex入出力/cache、GLM calls/usage、親往復、correction/re-review、wall-clock、known labels/coverage、誤検出・見逃し・unknownをsource-bound cohortで計測する。過去と比較条件が違えば明示する。
- System-Oneが部分的なtoken節約と引換えにGLM再呼出・parent判断・観測不能な品質riskを増やしていないかを含むend-to-end評価とし、特定モデルの出力tokenや部分処理だけで局所的成功を宣言しない。
- 先行のanalysis-index分類修正・Codex効率checkpointの正しい観測を再利用する。巨大なraw logを親contextへ再投影せず、boundedな機械集計と必要な現物proofを分ける。取れない証拠はunknownとし、計測不能な差分を改善値にしない。

### リファクタリングと責務整理

- 機能の重複・死蔵、役割を失った旧experimental code、二重state/adapter、過大なparent側orchestration、非決定的手順の手作業再実装を検出し、canonical ownerに集約する。
- 実証可能かつ同一責務内の改善はproduction code・testを修正する。不要な仕組みを残したままwrapperを増やさない。既存featureの挙動・拒否条件・データ契約・source binding・レビュー責任を維持して構造を単純化する。
- 独立した複数の巨大変更は一括して実装せず、親Codexが変更範囲と受入条件を具体Taskへ分割しPlanへ登録する。今回のTaskでも、実行可能な一貫した改善単位は実装する。合理的なno-change部分は根拠と再評価条件を示す。

### 機能追加と採用判断

- 既存のSystem-Oneの目的に対し不足する機能を評価する。候補例はboundedなevaluation cohort/reference作成・照合・集計、機械で省ける親往復、shadow/advisoryの効果観測、review feedbackの品質を落とさない利用など。候補の列挙は無条件の実装命令ではない。
- 各候補に利用者・呼出経路・測定条件・品質risk・導入/保守コスト・代替案・failure/rollbackを明示する。同一責務で成立する追加は実装・検証し、それ以外は具体的Taskに分けるか、証拠不足としてadoption保留する。
- shadow/No-Goを自動productionに昇格させない。従来のcanonical audit、独立Reviewer、親Codexのsemantic gateは維持する。Review削減、test impactによる省略、compaction thresholdは既存BLOCKEDの許可・実効証拠なしに実装しない。

## Must not

- System-One以外の無関係な全repositoryの大規模改修、現ACTIVEへの介入、既存Taskの再実装を混ぜない。
- 専用daemon・第二DB・旧schema互換fallback・無制限の再帰model call・新しい汎用classifier基盤を理由なく追加しない。
- モデルの意味判断や根拠のあるReview/Quality Gateを、単純なdeterministic出力や不確実なshadow結果へ置換しない。
- 省略・timeout延長・誤分類・unknownの0補完で性能や品質を偽装しない。re-review不要という期待だけでreviewを省略しない。
- cosmetic renameやtest fixtureの形だけの整理を、実効性改善と報告しない。歴史的なNo-Goを新しい証拠なしに覆さない。

## Acceptance criteria

1. System-Oneの現行構成と「有効・実際に実行・実効あり」を区別したowner/consumer棚卸し、死蔵/重複候補、既存Task所有への採否表をcurrent Git locator付きで残す。
2. 現行cohortで総親/GLM消費、親往復、モデル/非モデルevent、時間、品質の主要指標を評価し、過去baselineとの差・未計測条件・unknownを分離する。実効性を証明できない機能を本番で効果ありと判定しない。
3. System-Oneの単一責務内で実証された構造問題について、必要なら削除・統合・実装を伴う具体的なリファクタリングを行い、変更前後の責務と振舞いをtestで示す。実装しない対象はno-change理由を示す。
4. 機能改善・追加候補は実装、限定shadow評価、既存ownerへの統合、独立Task化、No-Goのいずれかに根拠付きで収束させる。同一責務で採用した機能は実経路の正/負例、停止・復旧・source不一致拒否を検証する。独立Taskを立てる場合はparent-managed Task/Planへ要求と優先度を明記する。
5. 局所的なコスト節約だけでなくend-to-endでCodex ReductionとQuality Deltaの両方を評価する。必要な正規validation、独立Review、親Codexのsemantic評価、Publication/Retireを保つ。未達成を成功扱いしない。

## Historical invariants

- current Git、`IMPLEMENTATION_RULES.md`、Plan、親Codexのsemantic authority、GLMにPlan/Taskを編集させない責務分離を維持する。
- current schema限定、後方互換性禁止、1 Task 1 Commit、source-bound証拠、fail closed、正規Quality Gate/Reviewer、Codex実消費削減と品質維持を守る。

## Dependencies

none
