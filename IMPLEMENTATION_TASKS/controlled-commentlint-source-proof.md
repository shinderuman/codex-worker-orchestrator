# Task: controlled commentlint の対象source検証証拠

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1489#discussion_r4452300564

** Minor** · **Functional Correctness** · **Quick win**

**Assert the intended comment violation.**

The target-root `golangci-lint` file is unclassified. `commentlint.Run` reports that violation before it scans `subject.go`, so this assertion passes without testing comment scanning. Move the fake executable outside the target root and assert a `commentlint/comment` violation for `subject.go`. ([raw.githubusercontent.com](https://raw.githubusercontent.com/shinderuman/codex-worker-orchestrator/fcc1fcfd22ba94b04e02bd3460c4871cada20bad/glm-worker/internal/commentlint/commentlint.go))
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1489#discussion_r4452300564) — `glm-worker/internal/harnesslint/controlled_autofix_fixed_point_test.go`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

未分類のtarget-root fake golangci-lintだけで違反assertが成功する。

## External feasibility

status: not-applicable

## Contract

- fake executableを分類対象外へ置き、意図したsubject.goのcommentlint/comment違反をexactに検証する。controlled toolの選択とtarget executable非実行の責務も保持する。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- subject.go違反のpath/kindをassertし、その違反を消すと当該assertが成功しない。target-owned tool未実行とcontrol-owned policy選択が引き続き確認できる。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
