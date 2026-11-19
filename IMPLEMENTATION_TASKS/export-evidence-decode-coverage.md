# Task: exportごとの証拠復号と観測範囲を一致させる

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B10. Canonical per-export evidence decode and coverage — #1636
> - **Owner/problem:** `controller/evidence_export_analysis.go` and `evidence_export_workflow.go` decode the same exported telemetry/review-round bytes repeatedly, with conflicting partial/malformed/foreign-task coverage (one view may say `collected` while another rejects).
> - **Historical rationale:** independent report/index projections accumulated local parsers around the same immutable export bytes.
> - **Implement/minimum boundary:** decode each export-snapshot source once into typed validity+coverage+identity; feed distinct existing view transformations. Do not change telemetry writers, global history scan, immutable graph or source archive.
> - **Preserve/risk:** strict TaskID/seq/timestamp/window, fail-closed incomplete evidence, valid JSON output semantics and timing analysis, source/raw bytes and immutable graph; no misleading normalized 'complete' for partial data.
> - **Acceptance:** malformed/cross-task rounds are consistently classified, valid-data outputs remain stable and repeated decode calls are countably reduced; focused export/workflow regressions pass.

## Purpose

exportごとの証拠復号と観測範囲を一致させる。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

controllerのexport analysisとworkflowでtelemetry/roundsの解釈が分散。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

同一export内で同じ入力のTask・時刻・coverageを一度検証し、foreign/malformedでview間の矛盾を起こさない。rollout内のtoken counterの連続性は別の計測契約が扱う。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
