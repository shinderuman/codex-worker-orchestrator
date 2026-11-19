# Task: PlanのTask表記をreview判定とscheduleで一致させる

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B09. Use canonical repository Plan task-entry grammar — #1630
> - **Owner/problem:** `taskcontract/schedule.go` accepts plain/backtick/Markdown-link Plan entries; `workflow/review_source_equivalence.go` independently parses NEXT-only metadata fast-forward and rejects valid plain-path bullets.
> - **Historical rationale:** byte-exact special review-source fast-forward code originally implemented a bespoke section parser.
> - **Implement/minimum boundary:** reuse canonical Plan task-entry grammar or a narrowly shared parser at the repository-specific `taskcontract` owner; keep byte-exact non-NEXT comparisons in review-equivalence.
> - **Preserve/risk:** exact index/HEAD/worktree/ancestor equivalence, only added NEXT entries and unchanged old NEXT order, no broadening to unrelated metadata or review target changes; generic workflow does not interpret new implicit repo policy.
> - **Acceptance:** all three canonical entry forms agree at Plan admission/equivalence; modified ACTIVE/context, reordered NEXT or changed source remain rejected.

## Purpose

PlanのTask表記をreview判定とscheduleで一致させる。

## External feasibility

status: not-applicable

## Contract

`taskcontract/schedule.go::scheduleEntryPath` はplain pathを許可し、`workflow/review_source_equivalence.go::reviewPlanTaskPath` は拒否する。同関数はACTIVE抽出にも使われる。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

plain/backtick/linkの正規形、変更されたACTIVE、NEXT並び替え、無関係なmetadata変更の拒否を照合。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
