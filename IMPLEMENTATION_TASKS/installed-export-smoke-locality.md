# Task: 再installせずにexportの導入後smokeを分離する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B17. Focus installed export-bundle smoke without reinstalling twice — #1645
> - **Owner/problem:** `tests/install_smoke.sh` combines actual install/uninstall/hook/tool acceptance with ~230-line export-bundle archive, Task-ID, lock-contention and concurrency tests plus embedded flock helper.
> - **Historical rationale:** export proof intentionally reused the *installed binary* fixture and source validation rather than synthetic runtime; separate test concerns cohabited.
> - **Implement/minimum boundary:** focused same-process shell export-smoke source invoked at existing place; share **one** real installer/bootstrap; no extra CI job, duplicate install, source `go run` substitute or runtime change.
> - **Preserve/risk:** validate source before fixture mutation, installed vs source identity, archive/manifest, no state/Git mutation, invalid Task ID, lock/cleanup, Linux/Darwin trap/error semantics, uninstall/reinstall and hooks.
> - **Acceptance:** full existing install smoke, negative concurrency and platform-gated checks pass with no additional heavy bootstrap; source-read locality measured.

## Purpose

再installせずにexportの導入後smokeを分離する。

## External feasibility

status: not-applicable

## Contract

tests/install_smoke.shのexport検証を既存のinstalled fixture内で局所化。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

installer実行回数を増やさず、archive、失敗、並行、uninstallを既存platform条件で検証。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
