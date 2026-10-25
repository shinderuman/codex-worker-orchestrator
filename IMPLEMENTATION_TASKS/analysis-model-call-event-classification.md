# Task: analysis-indexのモデル呼出集計から非モデルeventを除外する

## Original instruction

2026-10-26 Dogfood Auditで、Bundleの `analysis-index.model_calls` がモデルを呼び出していないtelemetry eventをモデル呼出として計上している不具合が確認された。親Codexは現行のtelemetry分類契約と全consumerを確認し、集計の正確性を回復すること。修正前後の実データと回帰テストで、Task/Reviewer別の呼出数とsession別集計が同じ対象recordから導かれることを実証する。既存の正規Workflow、独立Review、検証、Publicationを維持し、別の分類正本や旧schema互換経路を追加しない。

## Amendments

none

## Resolved references

- 現象: 2026-10-25 Dogfood Bundle `d2ee35ad-2be8-4933-ad03-4462207ba8c9(1).zip` の `attempt/telemetry.jsonl` には `call_type=task` 14件（worker 10、reviewer 4）と `call_type=event` 2件がある。一方 `analysis-index.json.model_calls` は total 16 / reviewer 6 / worker 10を報告し、`by_session` 合計は14。2件のeventを誤ってreviewer callに算入している。
- 発生箇所: `glm-worker/internal/controller/evidence_export_analysis.go:analysisModelCalls` は `len(logs)` と `ByRole` へ全telemetry recordを算入する。`glm-worker/internal/controller/evidence_export_workflow.go:workflowModelCalls` は明示的に `CallTypeTask` / `CallTypeProbe` を選別する。
- 型の正本: `glm-worker/internal/state/telemetry.go` に `CallTypeTask`、`CallTypeProbe`、`CallTypeEvent` が存在する。過去のtelemetry分類一元化で区別したものがanalysis-indexで再発している。実装時はcurrent Gitを正とし、既存predicate / readerの再利用を優先する。

## Purpose

Dogfood Bundleの `analysis-index.model_calls` が実際のモデル呼出を正しく表し、親CodexのModel usage / Codex Reduction / Review分析を非モデルeventの混入によって歪めない状態にする。

## External feasibility

status: not-applicable

## Contract

- `analysisModelCalls` の集計をcanonicalなtelemetry call-type意味契約へ統一し、`call_type=event`（parent-fix/parent-accept等の機械的記録）をモデル呼出として計上しない。別途event観測を失わせず、raw telemetry自体は変更しない。
- 現行productionで`task`と`probe`が何を表すか、各々のmodel-call / task-call reportへの計上条件を同一の正規ownerから検証する。`probe` をモデル実呼出として含める必要がある場合は`task`との意味差と消費量への帰属を明示し、eventと混同しない。
- `total`、`by_role`、`by_session` の対象record集合を一致させる。同一recordが有効なSessionIDを持たない場合の表示は、呼出数自体と無矛盾になるように定義する。単純にrole値や非空sessionの有無からモデル呼出と推定しない。
- workflowのmodel stages / usage / retry集計、coverage、bundle in-flight等の隣接consumerと正規call-type意味を照合する。既存の分類ownerを再利用し、consumerごとに新しい独立predicateを増やさない。
- 過去の原Taskの境界どおり、malformed / unsupported version / unknown call type は実model callへ黙って昇格させず、正規のunknown/unreadable等の意味を維持する。current schemaのみを対象とする。

## Must not

- `CallTypeEvent` をtask/probeに書き換えて帳尻を合わせない。raw event、model usage、TaskStats、session identityを偽装しない。
- Bundle形式や分析全体の大規模再設計、第二のtelemetry index/分類正本、過去schema向け互換fallbackを追加しない。
- 過去の別consumerで修正済みの分類を再実装しない。後続のCodex efficiency評価Taskを本Taskに取り込まない。
- 一致しない集計を欠損値の0補完や無条件skipで成功にしない。

## Acceptance criteria

- 同じsource-bound fixtureで `task` 14件（worker 10、reviewer 4）と `event` 2件を流したとき、`analysis-index.model_calls` が非モデルeventを除外し、モデル呼出数を14件として出力する。`by_role` と `by_session` の意味・計上集合を同時に検証する。
- `task` / `probe` / `event` と各role/session組合せ、欠落/不正type、unsupported version、二重呼出IDに対する正例・負例をcurrent contractに沿って検証する。
- 必要な差分だけで `analysis-index` と隣接するworkflow集計の意味的不整合を解消し、raw corpus/coverage/usageからeventを失わない。TaskStatsとraw telemetryの意図的な独立照合を維持する。
- 関連するintegration/unit testsとrepository-owned lint/Quality Gateを実行し、current Git snapshotに束縛した独立Reviewer / 親Codexのsemantic判定を得る。
- 修正後の実Bundleで同種のevent混入が再発しないことを観測できなければ、テストPASSをlive effectivenessの証明として報告しない。

## Historical invariants

- 現行Git / `IMPLEMENTATION_RULES.md` / Plan / 本Taskを実装authorityとする。Web GPT管理IssueはCodex/GLMの要求正本にしない。
- 正規GLM実装・独立Reviewer・Codexの最終意味判断・Guard・immutable evidence・1 Task 1 Commitを維持する。
- 実効性未証明をPASSにしない。既存ownerへの最小変更を優先し、不要な後方互換性を導入しない。

## Dependencies

none
