# Task: telemetry履歴cohort query

## Original instruction

````text
じゃあ022より前のタスクとして全部積んで対応してくれるか
なおCodexのトークン消費は減らしたいがGLMのトークン消費節約の優先度はそこまでではない
GLMのトークン消費を節約するためにCodexのトークンが増えるみたいなのは本末転倒
````

## Amendments

none

## Resolved references

- 2026-09-03調査ではtelemetryに433 model calls / 85 model-call task IDsがある一方、`glm-worker --stats`は34 calls / 7 tasksだけを集計し、旧revision 89 filesをorphanとしていた
- 対象はschema非互換を隠さず、current cohortとraw historyを分離して長期傾向をread-only query可能にする改善である

## Purpose

session aging、outlier、再作業の判断母集団を増やし、親Codexがrepository-wide raw log再解析を繰り返さずに済むようにする。

## External feasibility

status: not-applicable

## Contract

- stats / call-outliersのread-only queryへ明示的なcurrent/history scope、task ID、期間境界を追加する
- schema revision、record kind、field coverageごとにcohortを分離し、意味が互換でない値を単一totalへ暗黙合算しない
- 各cohortのfile/task/call count、coverage、除外理由、source locatorを返す
- current default behaviorの意味を保ち、history選択時だけ旧recordをboundedに走査する
- history走査はmodel call、repository mutation、migration、old telemetry rewriteを行わない

## Must not

- revision 0と1の不明fieldをゼロ補完して品質・usage比較へ使わない
- `usage_totals_known=false`を推定値でtrueにしない
- raw JSONL全文をSol-visible stdoutへ出さない
- 別telemetry DB、daemon、恒久version bridgeを追加しない

## Acceptance criteria

- mixed revision fixtureでcohort分離、期間/task filter、coverage/unknown、outlier母集団を検証する
- current scopeは既存結果と互換、history scopeは旧fileをorphan countだけで捨てず利用可能範囲を返す
- malformed/unsupported recordは理由付きcountとなりwhole-document fallbackを行わない
- CLI stdout/error contract、性能上限、既存stats/outliersにregressionがない
- 独立reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- machine-only old schemaは用途に応じskip/rejectし、恒久migrationを目的にしない

## Dependencies

none

## Review findings

none

## Current boundary

親Codex attribution完了後に実行する。
