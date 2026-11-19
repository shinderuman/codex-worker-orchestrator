# Task: 親証拠のGit差分取得と出力予算の責務を整理する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B12. Localize parent-evidence diff capture **and** output budget — #1629 #1640 #1646 #1648
> - **Owner/problem:** `parentevidence/projector.go` co-locates manifest/provider/lease/ledger, raw Git HEAD/index/worktree capture and output-budget degradation. Four Issues rediscovered the **same diff capture** root; #1629 additionally covers budget.
> - **Historical rationale:** #1049/#1067 package extraction followed by diff fidelity/bounded execution/JSON cap improvements co-located implementation in `projector.go` while `diff_patch.go` became focused.
> - **Implement/minimum boundary:** **one** same-package `diff_capture` owner for raw diff read/parse/untracked/mode checks alongside `diff_patch.go`; **one** focused output-budget/stripping owner; keep `Projector` and `Project`/`Commit` sequencing, manifest, leases and ledger central. Do not add a second diff implementation, forwarding shim, package or persistence authority.
> - **Preserve/risk:** captureDiff double-read, commit-time `validateDiffSnapshots` under ledger lock, exact HEAD/index/worktree/file-mode/path/source digest, symlink/binary/untracked, error/timeout/byte limits, JSON cap, token/part status and claim-on-success. Distinct TOCTOU checks are **not** redundant.
> - **Acceptance:** exact existing Git/manifest/body/ledger semantics on normal/error/racy/platform cases, diff/budget tests and required CI; before/after focused review surfaces recorded. This is **one implementation of D1**, not four.

## Purpose

親証拠のGit差分取得と出力予算の責務を整理する。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

Git diff取得4提案の実装は一度。parentevidenceのbudget/strippingも同作業内の別責務として残す。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

二度のcaptureとledger lock下のcommit時再検証、mode/untracked/symlink、serialized budgetと未配信claimを維持。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
