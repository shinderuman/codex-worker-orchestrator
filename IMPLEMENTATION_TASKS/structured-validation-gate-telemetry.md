# Task: structured validation gate telemetry

## Original instruction

````text
じゃあ022より前のタスクとして全部積んで対応してくれるか
なおCodexのトークン消費は減らしたいがGLMのトークン消費節約の優先度はそこまでではない
GLMのトークン消費を節約するためにCodexのトークンが増えるみたいなのは本末転倒
````

## Amendments

none

## Resolved references

- 2026-09-03調査で`--test-impact`はretained 11 tasksに対し認識済みtest call 0、652 operationsを`other`とし、compound command/pipelineからsuite identityを復元できなかった
- 対象はtest省略ではなく、実行時点でtest/lint/build/typecheck等を構造化観測する前提整備である

## Purpose

verification品質を保ったまま、将来のtest impact判断と高コストgate特定に必要な証拠を追加AI callなしで収集する。

## External feasibility

status: not-applicable

## Contract

- validation実行pointでgate class、suite/snapshot ID、result、duration、phase、初回/retryをbounded fieldとして記録する
- compound shell文字列の事後推定をprimary authorityにせず、既存validation record/runnerとのsingle sourceを設計する
- `--test-impact`はstructured recordを優先し、coverage、unknown、source locatorを返す
- raw shell command、stdout/stderr全文、test source全文をtelemetry summaryへ保存しない
- existing event/telemetry retentionとschema policyへ統合し、別DB/daemonを追加しない

## Must not

- このtaskでtest selection、省略、samplingをproductionへ導入しない
- test件数やduration削減だけを品質維持の証拠にしない
- unknown suiteを既知categoryへ推定分類しない
- 観測のために同じtestを追加実行し、Codex orchestration turnを増やさない

## Acceptance criteria

- test/lint/build/typecheck、compound execution、retry、failure、unknownのfixtureを検証する
- existing execution一回からstructured recordが得られ、追加test/model callを必要としない
- `--test-impact`が0 recognizedへ不正縮退せずcoverage/unknownを説明する
- 104 test-impact-selectionは品質証拠が揃うまでBLOCKEDを維持する
- 独立reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- full test gateを維持し、suite-level failure / escaped contrastがunknownな間は省略しない

## Dependencies

none

## Review findings

none

## Current boundary

session rotation採否後に実行する。
