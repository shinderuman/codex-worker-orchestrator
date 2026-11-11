# Task: parent diff evidence の入力とpatch生成の実行上限

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534774047

**[P2] diff の要求出力上限を適用する前に対象ファイルを無制限で読み込んでいます。** `diffReadRegularLeaf` の `io.ReadAll(file)` は `DiffRequest.BudgetBytes`（最大262144バイト）とは独立に全ファイルをメモリへ読み込み、`capturedDiffPatch` も完全な `git diff --no-index` パッチを生成します。さらに `captureDiff` は同じ取得を2回行い、`validateDiffSnapshots` が commit 直前に再度 `captureDiff` するため、単一の大きなバイナリやログを `budget_bytes: 1` で要求しても、複数回の無制限メモリ割当・base64等の生成が走ります。小さな出力要求が OOM/極端な遅延を誘発します。ソースサイズ・生成パッチサイズに独立した実行上限を設け、超過時には identity を含む refinement/error を返す等、出力予算が小さくても入力処理が無制限にならない契約にしてください。
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1566#discussion_r4534774047) — `glm-worker/internal/parentevidence/diff_fs_unix.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

表示BudgetBytes適用前のio.ReadAllと完全patchの反復生成に上限がない。

## External feasibility

status: not-applicable

## Contract

- 出力予算と独立した入力/patch生成の有限上限をownerで定義し、超過は実identityを失わないrefinement/errorにする。source検証の必要な二重観測を盲目的に削らない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 小さい出力予算で巨大通常file/バイナリを要求しても入力とpatchが有界で、全件成功に見せない。HEAD/index/worktree identity、no-follow、source変化拒否、commit前再検証を維持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
