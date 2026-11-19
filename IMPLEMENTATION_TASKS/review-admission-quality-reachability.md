# Task: worker終了からreviewer起動までの品質ゲートを検証する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B06. Verify reachable quality gate before review admission — #1637
> - **Owner/problem:** quality-wiring package scan currently accepts all required method calls anywhere, even if they are in disconnected functions; it does **not** prove actual worker-end → fix-first quality → accepted snapshot → reviewer admission.
> - **Historical rationale:** past work tightened source syntax and binding proof without proving call-path reachability.
> - **Implement/minimum boundary:** a focused executable/behavioral review-admission contract and negative disconnected-helper proof; narrow source-presence surrogate to independently useful mechanically checkable facts rather than building an AST callgraph.
> - **Preserve/risk:** repository opt-in, `harnesslint.Run(root,true)`, single validation, correct snapshot freshness, fail-closed missing/failing gate, fix/review/recovery and independent safety guards.
> - **Acceptance:** disconnected-method fixture no longer suffices to certify review guard; actual reachable success/fail/stale/resume cases verified, no duplicated validation or weakened runtime protection.

## Purpose

worker終了からreviewer起動までの品質ゲートを検証する。

## External feasibility

status: not-applicable

## Contract

B05の表を整理しても、本番review admissionの到達性は別問題。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

未接続helperだけでは合格しない負例と、実際のworker終了→fix-first gate→同snapshot reviewの成功・失敗・resumeを確認。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

- `IMPLEMENTATION_TASKS/quality-wiring-policy-owner.md`
