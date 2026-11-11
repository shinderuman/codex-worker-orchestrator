# Task: quality wiring の package scan でbindingを検証

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1489#pullrequestreview-5748993675

**The package-level scan does not check bindings. A shadowed `harnesslint` or receiver name can satisfy the wiring.**

`scanQualityWiringCalls` matches only on identifier names. It uses the import name and the `Workflow` receiver name. It does not check that the identifier resolves to the `*types.PkgName` or the receiver parameter.

In the following cases, a call can satisfy `harnesslint.IsViolation(qualityReport)` or `w.qualityGate(...)`:
- a local `harnesslint := fake{}` declared inside a `Workflow` method;
- a local variable that shadows the receiver name.

`qualityWiringIsImportedCall` already rejects these cases for `quality_gate.go`. The new tests, for example "shadowed package cannot impersonate canonical import", cover only that path. The package check has the same impersonation risk.

Reuse `qualityWiringPartialBindings`. Require `Uses[ident]` to be a `*types.PkgName` for the harnesslint prefix. Require it to match the receiver's `Defs` object for the `workflow` prefix.
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1489#pullrequestreview-5748993675) — `glm-worker/internal/harnesslint/quality_surface.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

package scanが名前一致だけでharnesslint import/Workflow receiverを認める。

## External feasibility

status: not-applicable

## Contract

- 既存partial bindingsをpackage-level scanにも接続し、実importのPkgNameとreceiverのDefs/Uses identityで判定する。importer/export-data実行を再導入しない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- package aliasの正常例を受理し、local harnesslintとreceiver shadow、偽selector、コメントのdecoyを拒否する。既存quality_gate.go検査とproduction wiringのpositive coverageを維持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
