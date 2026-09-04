# Task: Telemetry query bounded-time integrity

## Original instruction

````text
https://github.com/shinderuman/codex-worker-orchestrator/pull/345
CodeRabbit, Greptileのレビュー結果を参照して必要なら対応するタスクを作ってくれ
````

## Amendments

none

## Resolved references

- reviewed head: `a595057f5912ece29a99a778c683619612f9b7a5`
- CodeRabbit comments `3930465316` / `3930465321`: `https://github.com/shinderuman/codex-worker-orchestrator/pull/345#discussion_r3930465316`, `https://github.com/shinderuman/codex-worker-orchestrator/pull/345#discussion_r3930465321`
- `TelemetryQueryArgs.view`はparsed filterのfractional secondsを`RFC3339`表示で失い、`CoversTime`はbounded until-only queryへzero timestamp recordを含める

## Purpose

telemetry history queryの表示境界と選択境界を一致させ、日時不明recordをbounded periodの実測値へ混入させない。

## External feasibility

status: not-applicable

## Contract

- parsed `Since` / `Until`のfractional secondsを`RFC3339Nano`でlosslessにmachine viewへ返す
- filterがSinceまたはUntilのいずれかを持つbounded queryではzero timestamp recordを期間内と扱わない
- unbounded queryではzero timestamp recordの既存取扱いを維持し、coverage/unknownとしての可視性を失わない
- half-open period semanticsとcohort集計、outlier selectionで同じ`CoversTime`契約を使う

## Must not

- filter入力を秒へ丸めて検索範囲を広げない
- timestamp不明recordへ実時刻を推測付与しない
- zero timestamp recordを全queryから無条件削除しない

## Acceptance criteria

- nanosecond境界のSince/Untilがviewとselectionで一致する
- until-only、since-only、両側bounded queryでzero timestampが除外され、unboundedでは既存どおり扱われる
- current/history statsとcall-outliersの両surfaceに回帰testがある
- relevant test、independent reviewer、Sol review、current snapshot validation、commit/installを完了する

## Historical invariants

- bounded telemetry reportはsource timestampで証明できるrecordだけを期間集計へ含める

## Dependencies

none

## Review findings

````text
Format both query bounds with time.RFC3339Nano so fractional seconds from TelemetryQueryFilter are preserved. Exclude undated records from bounded queries; a zero StartedAt currently passes an until-only filter.
````

## Current boundary

PR 345の外部review findingをcurrent HEADへ適応して検証する。
