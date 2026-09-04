# Task: Parent usage report evidence integrity

## Original instruction

````text
https://github.com/shinderuman/codex-worker-orchestrator/pull/345
CodeRabbit, Greptileのレビュー結果を参照して必要なら対応するタスクを作ってくれ
````

## Amendments

none

## Resolved references

- reviewed head: `a595057f5912ece29a99a778c683619612f9b7a5`
- CodeRabbit activity-boundary comment `3930465308`: `https://github.com/shinderuman/codex-worker-orchestrator/pull/345#discussion_r3930465308`
- CodeRabbit review `5108716163` nitpick: `parentUsageRolloutScan`が`scanCodexRolloutWindow` errorをempty scanへ潰し、read failureをmissing/no-observationと誤分類する

## Purpose

parent Codex usage reportのexecution/finalization区間を重複のないpartitionにし、rollout read failureを証拠不存在と区別する。

## External feasibility

status: not-applicable

## Contract

- `execution.end`と`finalization.start`の共有境界にあるtool event・tool result・compaction・turnを一方の区間だけへ帰属させ、token offset境界と整合するpartition semanticsを定義する
- execution intervalは従来のinclusive endを維持し、finalization intervalのstartをexclusiveにする。turn overlapも同じ共有境界で二重計上しない
- rollout scan errorをempty scanへ変換せず、read/unmarshal/permission等の失敗をdedicated degraded/unreadable evidenceとしてreportする
- 本当にanchor/eventがないmissing/no-observationとread failureをmachine status/reason/source locatorで区別する
- 合算値を推測せず、degraded intervalをavailableとして扱わない

## Must not

- boundary eventをdropまたは二重計上しない
- read failureをzero usage、missing anchor、no-observationへ縮退しない
- token counter reset/partial-anchor防御を緩和しない
- raw rolloutをmachine JSONへinlineしない

## Acceptance criteria

- execution.endと同一timestampのtool call/result、compaction、zero-duration/境界turnがexactly one intervalへ計上されるfixtureがある
- 境界前後の通常eventとtoken deltaが一貫した区間partitionになる
- rollout open/read/parse failureと正常なevidence不存在が異なるstructured outcomeになる
- bundle analysis・parent usage既存test、independent reviewer、Sol review、current snapshot validation、commit/installを完了する

## Historical invariants

- parent Codex実消費は最上位optimization metricであり、不明値をavailableなdeltaへ推測しない

## Dependencies

none

## Review findings

````text
Adjacent intervals double count an event on the shared boundary. parentUsageRolloutActivity treats both bounds as inclusive, while execution.end is also finalization.start. Make one side exclusive and apply it consistently to tool events and compactions.

Distinguish a rollout read failure from absent evidence. parentUsageRolloutScan discards scanCodexRolloutWindow errors and reports an empty scan; propagate the failure into a dedicated degraded-evidence path.
````

## Current boundary

PR 345の2 findingを同じparent usage evidence責務としてcurrent HEADで検証する。
