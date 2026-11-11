# Task: request Publication 成功時の terminal 判定

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534705861

<a href="#"></a> **Published requests report failure**

After a request is successfully published, `projectActiveCompletion` returns `PublicationOperationNone`. But `completionTerminalBoundary` treats that operation as “completion stopped before terminal publication.” The normal `glm-parent-action` completion loop therefore returns `blocked` after publishing and acknowledging the request, without the successful completion handoff.

Recognize a completed request as a successful terminal boundary when the result contains matching publication evidence.
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534705861) — `glm-worker/internal/controller/publication_operation_projection.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

公開済みrequestがPublicationOperationNoneとなり、completionTerminalBoundaryが失敗へ読み替える。

## External feasibility

status: not-applicable

## Contract

- 完了済みrequestは実Publication証拠とowner一致を確認してterminal成功へ収束させる。noneの一般的な成功化はしない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 通常requestを実親CLIでacceptからpublish/ack/handoffまで通し、remote/final OID・transition/generation一致を確認する。未公開・欠測・不一致noneは成功せず、新requestを自動開始しない。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
