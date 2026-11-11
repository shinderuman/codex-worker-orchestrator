# Task: Plan spacing 検査と schedule 表現を揃える

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607618

** Minor** · **Functional Correctness** · **Quick win**

**Match every supported task-item representation.**

If a plan uses two backtick-path bullets with one blank line between them, `planSeparatedTaskItems` does not match either bullet. Lint reports no spacing violation, and `--fix` leaves the gap in place. Match the supported plain-path and backtick-path bullets as well as Markdown-link bullets. The schedule parser accepts all three representations. ([raw.githubusercontent.com](https://raw.githubusercontent.com/shinderuman/codex-worker-orchestrator/1114f182d71fbe286d8d822ec983a9eb1eb64919/glm-worker/internal/taskcontract/schedule.go))

As per coding guidelines, “Planを含むtask scheduling listはunordered marker `-`を使い”.

_Source: Coding guidelines_
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607618) — `glm-worker/internal/harnesslint/markdown_authority.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

spacing regexがMarkdown link以外の正規task bulletを拾わない。

## External feasibility

status: not-applicable

## Contract

- scheduleが受理するplain/backtick/linkの3表現と混在をspacing check/fixでも扱い、要求/順序を変更しない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- 各表現と混在の隣接項目間の空行を検出・正規化し、固定点になる。heading間空行と非schedule文書/コードfenceを壊さず、項目と順序を保持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
