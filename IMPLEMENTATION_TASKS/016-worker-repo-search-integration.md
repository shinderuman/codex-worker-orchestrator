# Task: worker repo-search integration

## Original instruction

````text
## Task 016: worker repo-search integration

workerが対象不明時だけrepo-searchを使うproduction routingを実装。

毎回BM25を強制しない。
````

## Amendments

- 2026-08-22 parent maintenance:

````text
## 13. repo-search fallbackを複雑化しない

Task 016にあるfallbackは単純にしてください。

repo-searchはnavigation aidであり、検索失敗時に新しいrecovery state machineを作る必要はありません。

原則:

```text
target既知
→ repo-search不要

target不明
→ repo-search

repo-searchが利用不能 / 十分な候補なし
→ 既存の通常repo inspection経路へ戻る
```

程度で十分です。

retry tree、複数search backend、embedding fallback等を追加しないでください。
````

## Purpose

一次探索tokenを削減しつつ既知targetの無駄なsearchを避ける。

## Contract

- target既知ならrepo-searchを使わず、target不明時だけ使い、利用不能または候補不足なら既存の通常repo inspectionへ戻る
- BM25 core/fingerprint修正は再実装しない

## Must not

- 全task強制search、retry tree、複数search backend、外部search API、embedding fallbackを追加しない

## Acceptance criteria

- production prompt/dispatch因果、known/unknown target scenario
- search failure fail-safeとtelemetry
- test/race/vet/build/gofmt、独立reviewer、risk/contractに応じて必要なSol品質gate、commit

## Historical invariants

- BM25 coreとfingerprint `87fb116`

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。F8 integration `6ccae8dc001a002fec0aa790f3f2bca71de16c9d`を開始点とし、既存BM25 coreを再実装せず、production worker dispatchでtarget既知/不明を判定して必要時だけrepo-searchへrouteする最小境界を調査・実装する。fallbackは利用不能/候補不足時に既存normal repo inspectionへ戻すだけとし、telemetryとknown/unknown/failure scenarioを固定する。
