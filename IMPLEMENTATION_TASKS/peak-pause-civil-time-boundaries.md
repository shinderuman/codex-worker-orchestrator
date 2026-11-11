# Task: peak pause の DST 欠落・重複時刻境界

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534718923

**[P2] DST 開始日に存在しないローカル時刻を、停止開始時刻としてそのまま変換しています。** 例: `America/New_York` の `sun@02:30-03:30`、2027-03-14 は 02:00 から 03:00 に時計が進み、02:30 は存在しません。しかし Go の `time.Date(2027, 3, 14, 2, 30, ..., location)` は **01:30 EST** を返します。この実装では設定した 02:30 より１時間早い 01:30 から GLM 呼び出しを遮断します。タイムゾーンを利用者が設定できる契約なので、欠落時刻と重複時刻の扱いを明示的に定め、その規則に沿って境界を算出し、DST 移行日の回帰テストを追加してください。
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534718923) — `glm-worker/internal/peakpause/policy.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

time.Dateの正規化で存在しない02:30が01:30へ戻り停止を早める。

## External feasibility

status: not-applicable

## Contract

- 指定timezoneのローカル曜日・時分の半開区間を意味契約とし、欠落するローカル分は区間へ含めず、重複する分は両回を含める。除外されるローカル分を跨ぐ場合はintervalを分割する。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- America/New_Yorkの2027-03-14 sun@02:30-03:30は03:00-03:30に対応し01:30で停止しない。全区間欠落は空、fall-backは該当する両回だけを含む。日跨ぎ/曜日/隣接mergeと次回境界の既存契約を維持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
