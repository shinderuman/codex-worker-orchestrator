# Task: worker call outlierを可視化

## Original instruction

````text
## Task 009: worker call outlier可視化

現観測:

- v3 worker-new 41 call: turn median 55 / p95 137
- structured移行task resume: 320 turn / 約20.08

まずtask/phase/session/modelごとのoutlierを追加AI callなしで見えるようにする。

hard turn capやsession rotationはまだ導入しない。
````

## Amendments

- 2026-08-22 parent maintenance:

````text
#### worker call outlier

Task 009は保存済みtelemetryから分析可能であり、machine protocol measurement Task 008が完了しないと成立しないtaskではありません。

Task 008をhard dependencyから外してください。

Task 008を先にやるpriorityを維持すること自体は構いません。
````

- 2026-08-23 cross-repository telemetry baseline:

````text
codex-configの保存済みcurrent raw telemetry 58 model callでは、worker-new turn中央値113 / p95 203、全worker model時間46,591,678ms / 全体57,287,575msだった。task累積では452 turn、444 turn、368 turn等のoutlierが存在する。

Task 009では初回workerだけでなく、worker-explicit-fix / worker-auto-fix / resumeをphaseとして分離し、task単位のcall増幅を表示すること。media-backupは8 callだけなので分布閾値の根拠にはせず、同じ集計がrepositoryごとに適用可能かを確認するcross-repository fixtureとして扱う。

ただし、このGLM側outlierはprovider枠・wall timeの観測であり、Codex ReductionやQuality Deltaの直接証拠ではない。Task 009を最上位目的より優先する根拠や、GLM消費削減自体を成功条件にしないこと。
````

## Resolved references

- 2026-08-22追加指示の`009-worker-call-outlier.md` = 現存する`IMPLEMENTATION_TASKS/009-worker-call-outliers.md`

## Purpose

品質を落とさず長大callの発生条件を測定可能にする。

## Contract

- 保存telemetryからtask/phase/session/model別分布とoutlierを表示
- worker-new / explicit-fix / auto-fix / resumeを分離し、task単位のcall・turn・duration増幅を表示
- raw prompt/responseを保存・表示しない

## Must not

- hard cap、session rotation、benchmark追加callを導入しない

## Acceptance criteria

- median/p95/outlierと対象taskを再現可能に表示
- current/resumeを区別し既知例と整合
- test/race/vet/build/gofmt、独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- History 2026-08-21 canonical telemetry分析

## Dependencies

none

## Review findings

none

## Current boundary

未着手。2026-08-23 baselineでworker-new中央値113 turn / p95 203 turnと複数の累積400 turn超taskを確認。
