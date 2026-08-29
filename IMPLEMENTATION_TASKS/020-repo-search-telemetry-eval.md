# Task: repo-search telemetryとEval hooks

## Original instruction

````text
## Task 020: repo-search telemetry / Eval hooks

Direct/orchestrated A/Bへ接続できるtelemetryを実装。

本番benchmark自体は許可待ち。
````

## Amendments

none

## Purpose

search導入のCodex ReductionとQuality Deltaを測定可能にする。

## External feasibility

status: not-applicable

## Contract

- query category、hit/miss、result count、fallback、durationを秘密なしで記録
- A/B schemaへ接続し実runは分離

## Must not

- raw query/result本文、無許可本番A/Bを保存・実行しない

## Acceptance criteria

- telemetry加法整合とfixed Eval hook
- test/race/vet/build/gofmt、独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- eval-ab read-only、telemetry exact-once
- Task 019でdefault-on feature flag、read-only CLI、managed instruction、install wiringが完了済み。既存feature on/offとBM25 coreをtelemetry実装で再設計しない

## Dependencies

none

## Review findings

none

## Current boundary

未着手。
