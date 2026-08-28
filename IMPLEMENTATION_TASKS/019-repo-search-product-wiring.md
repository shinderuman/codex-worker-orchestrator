# Task: repo-search feature flag、CLI、install integration

## Original instruction

````text
## Task 019: repo-search feature flag / CLI / install integration

- feature flag
- CLI
- install.sh distribution
- managed instruction
- installer smoke

をproduction wiring。

既存BM25 pure coreを壊さない。
````

## Amendments

none

## Purpose

repo-searchを管理可能なproduction featureとして配布する。

## Contract

- default/flag/CLI/help/config/installの一貫性
- disabled時に既存挙動不変

## Must not

- core再実装、外部依存追加を行わない

## Acceptance criteria

- feature on/off、CLI、installer preflight/smoke、managed現物一致
- test/race/vet/build/gofmt、独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- installer fail-closed、BM25 core
- 016 worker repo-search integrationはPR #22のSquash Merge commit `da468541683a832b102acecc60770678452e6fa4`で充足済み。既存production routingとBM25 coreをfeature wiringで再実装しない
- exhaustive search gateは完了済みであり、019開始時の未充足dependencyではない

## Dependencies

none

## Review findings

none

## Current boundary

未着手。
