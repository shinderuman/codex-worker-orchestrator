# Task: exportの共通fixtureとシナリオを整理する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B13. Canonical evidence-export test fixture + localized scenarios — #1628 #1632 #1642
> - **Owner/problem:** `controller/evidence_export_test.go` mixes ~660 lines of shared fixture/support used by many other tests with >1,000 lines of unrelated live/sealed/target/transcript/negative scenario suites.
> - **Historical rationale:** original integrated tests supplied the fixture; later focused export tests reused it without moving the shared definition or older scenarios.
> - **Implement/minimum boundary:** **one** test-only same-package fixture/support owner; relocate coherent route/target/transcript/validation/archive tests to focused same-package scenario owners as needed; no duplicated helper, alias or generic fixture framework.
> - **Preserve/risk:** all existing test names/assertions, immutable evidence/task/attempt/source identity, failures, missing/unreadable evidence, source mutation, setup/cleanup and `t.Parallel`; no production changes or policy leak into generic fixture.
> - **Acceptance:** all fixture consumers, focused/full Go tests and CI pass, no assertion reduction; representative targeted scenario/fixture reads exclude unrelated catch-all suites. This is **one implementation of D2**, not three.

## Purpose

exportの共通fixtureとシナリオを整理する。

## External feasibility

status: not-applicable

## Contract

controllerのexport fixtureとcatch-all scenarioの移設は一度。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。 B10またはB11で同じscenarioを変更する場合は、先にその変更を確定する。

## Acceptance criteria

fixtureを使う全テストを列挙し、名前・assertion・cleanup・並列性が移設前後で保たれることを照合。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
