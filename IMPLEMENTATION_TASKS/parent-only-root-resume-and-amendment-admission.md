# Task: parent-only の Root 復帰と要求同期の正規接続

## Original instruction

````text
親Codexの評価内採用:

完了済み子Taskのresuming-root episodeが残ると、parent-only materializationのtyped commandが欠落する。smoke専用task.idも通常Runtimeと誤認され、sync-task-authorityが存在しないbindingを要求する。本評価の実復旧を通常成功扱いしない。

既存parent-onlyの開始・要求同期をState直接変更なしで成立させる。新しい専用コマンドを作らず、既存ownerへ接続する。
````

## Amendments

none

## Resolved references

- `glm-worker/internal/controller/parent_only_execution.go` / `publication_operation_projection.go` / `root_execution.go`
- `glm-worker/internal/app/controller_execution_projection.go` / `glm-worker/internal/workflow/task_authority_sync.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

完了済み子Taskのresuming-root episodeが残ると、parent-only materializationのtyped commandが欠落する。smoke専用task.idも通常Runtimeと誤認され、sync-task-authorityが存在しないbindingを要求する。本評価の実復旧を通常成功扱いしない。

## External feasibility

status: not-applicable

## Contract

- 既存controller-executionのRoot復帰・parent-only materialization・Task authority同期を接続し、parent-onlyには通常Runtimeを要求しない。既存episode closureとTask/source/attempt bindingを維持する。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 同じRootが子Task完了後にState直接編集なしでparent-only開始へ戻り、Amendmentを同じattemptへ同期してcomplete/Bundle/Publicationへ進む。実model呼出・通常Runtime binding作成をせず、GLM指定Taskへの誤昇格とstale generation/契約を拒否する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
