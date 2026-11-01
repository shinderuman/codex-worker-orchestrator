# Task: workspace処理cost testのlane分離

## Original instruction

````text
TestWorkspaceProcessCostAcrossMaterializationAndSourceImportのsource-import subtestがwrite pathを実行するよう、操作ごとのlaneを作成する。
````

## Amendments

none

## Resolved references

- CR-1440-N1 · https://github.com/shinderuman/codex-worker-orchestrator/pull/1440#pullrequestreview-5727231289（nitpick: `glm-worker/internal/controller/workspace_process_cost_test.go:27-39`）
  原文: "**The `source-import` subtest does not exercise the write path.** Both operations share one `lane`. The `materialize` subtest populates `lane` first. In the `source-import` subtest, `applySourceTreeEntries(lane, entries, entries)` then finds every file already matching `after`. It never calls `writeParentSourceEntry`. The process-count and content assertions therefore pass without testing blob reads for source import on an empty lane. Create a fresh lane inside each operation closure."

## Purpose

testが意図する測定対象（空laneへのsource import）を実際に実行する。

## External feasibility

status: not-applicable

## Contract

- 各操作closureが独自のworktree laneを作成し、source-importが空laneへの書込み経路を実行する。測定assertionの枠組みは変更しない。

## Must not

- 閾値や測定構造を変えない。

## Acceptance criteria

- source-import subtestで`writeParentSourceEntry`経由の書込みが発生することを検証するtest構成。

## Historical invariants

none

## Dependencies

none
