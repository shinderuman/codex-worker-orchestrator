# Task: staging JSON 文書終端の厳密検証

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534718921

**[P2] strict JSON decoder が余剰の閉じ括弧を受理します。** `Decoder.More()` はトップレベル文書の EOF 判定ではありません。Go で `{"schema_version":1,"token":"100","args":["./..."]}]` または末尾が `}}` の入力を与えると、最初の `Decode` は成功し、`More()` は `false` になるため `ParseStagingRequest` は不正な request を受理します。これは staging protocol の厳密な入力拒否契約と矛盾します。２回目の `Decode(&extra)` が `io.EOF` であることを要求し、余剰 `]`/`}` を含む回帰テストを追加してください。
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534718921) — `glm-worker/internal/dockertest/staging.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

Decoder.Moreはtop-level EOF検査ではなく余剰閉じ括弧を受理する。

## External feasibility

status: not-applicable

## Contract

- request/response共通decoderは最初の値の後のdecodeがio.EOFであることを確認する。unknown fieldとschema/token検査を維持する。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 有効な単一文書と末尾空白を受理し、余剰]/}/別JSON値/garbageをrequestとresponseの両方で拒否する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
