# Task: request の外部進行後の復旧 authority

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534705869

<a href="#"></a> **Pulled requests cannot recover**

The new request-backed tasks cannot recover from an external branch advance. After an unpublished request candidate is created, a fast-forward pull selects `rebind`. That path still calls `publicationProjectAdvancement`, which loads committed `IMPLEMENTATION_PLAN.local.md` and project task files. Ordinary repositories admitted here have neither, so recovery fails and the request cannot continue.

Carry request authority through the advancement paths without loading project-only metadata.
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534705869) — `glm-worker/internal/controller/request_authority.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

requestのrebind/adopt経路がproject専用Plan/Taskを要求する。

## External feasibility

status: not-applicable

## Contract

- request authorityを外部fast-forward進行・候補rebind/adoptへ運び、project-backed経路と区別する。request内容とidentityを保持する。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- Plan/Task/harness markerのない実repositoryで、未公開候補後の外部進行とPullから復旧できる。request改変・divergence・stale source/leaseを拒否し、project-backed既存検証を維持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
