# Task: mutable advancement の lossless baseline 拒否証拠

## Original instruction

````text
出典: https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607678

** Minor** · **Data Integrity & Integration** · **Quick win**

**Lossless baselineの拒否条件をAcceptance criteriaに追加してください。**

`attributeExternalPull`は`BaselineTrees.IndexTree`または`BaselineTrees.WorktreeTree`が空の場合に拒否します。現在のContractとAcceptance criteriaはremote-ahead、未コミット変更の衝突、divergence、lease/source freshnessだけを列挙しています。baseline欠落時に`ProjectParentCompletion`が`adopt-mutable`を提示せず、admissionも状態を変更せず拒否するfixtureを追加してください。そうしないと、実行側が拒否する操作をprojectionが提示する可能性を検証できません。
````

## Amendments

none

## Resolved references

- [指摘原文](https://github.com/shinderuman/codex-worker-orchestrator/pull/1479#discussion_r4447607678) — `IMPLEMENTATION_TASKS/publication-adopt-mutable-projection-admission.md`
- 指摘のtool実行ログと重複したAI向けpromptは要求原文から除外し、指摘本文と修正提案を保持する。採否は2026-11-11の親Codex評価による。

## Purpose

baseline欠落時の実拒否はあるがprojectionと状態不変のnegative coverageが確認できない。

## External feasibility

status: not-applicable

## Contract

- 既存adopt-mutable ownerのlossless baseline不変条件を直接検証し、projectionが実行不能操作を提示しないことを固定する。別adoption機構は追加しない。
- 既存ownerを修正し、具体的なpositive/negative caseを固定する。初期調査で前提が変わる場合は根拠を親判断へ戻す。

## Must not

- 本責務と無関係な機能、恒久compatibility bridge、専用operator command、並行stateを追加しない。
- 拒否・欠測・未実行を成功に読み替えず、既存品質gateとsource/authority検証を弱めない。

## Acceptance criteria

- IndexTreeまたはWorktreeTree欠落の各fixtureでadopt-mutableを提示せず、直接admissionもhead/attempt/lease/sourceを変えず拒否する。有効baselineの成功とremote-ahead/conflict/divergence/stale sourceの既存拒否を保持する。
- 要求と実差分を照合し、変更に必要な検証、独立reviewと親採否、runtime変更時のinstall/smokeを実sourceへ束縛する。

## Historical invariants

- 品質維持とCodex実消費削減を両立し、machine時間やfixture数をtoken削減率の代用にしない。

## Dependencies

none
