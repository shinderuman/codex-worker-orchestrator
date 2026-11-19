# Task: 導入済みCodex指示の適用範囲を検証する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

2026-11-20

> 1672はTaskの時点で大きく間違っているのでPR自体も修正した
> codex/AGENTS.md 等にリポジトリ固有のルールが入っていてそれをinstall.sh経由でユーザーレベルに配置されていることがおかしい
> リポジトリ固有のルールとユーザーレベルのルールの境界線が曖昧になっているので直すというタスクだ
> PRは多少直させたがお前が作業する際に改めて精査してくれ
> PRにかかれていることも鵜呑みにするな
> 確認は実際1672をするときでよい

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ## Additional deferred design concern — installed Codex instructions scope (outside B01–B17)
>
> - **Owner/problem:** `install.sh` calls the Codex installer; `glm-worker/internal/codexinstall/files.go` installs repository `codex/AGENTS.md` as `$CODEX_CONFIG_DIR/instructions/codex-worker-orchestrator.md` (default `~/.codex/instructions/codex-worker-orchestrator.md`). This tool-owned user-level instruction includes repository-specific assumptions such as `IMPLEMENTATION_RULES.md`, parent maintenance, and repository harness semantics. Review whether activation/scoping keeps these rules from being treated as applicable to unrelated repositories. The installer **does not** install or overwrite the user-owned `~/.codex/AGENTS.md`.
> - **Historical reason:** this source-to-user-level installation and its repository-specific rule content already existed before PR #1651; that PR reorganized instruction text but did not introduce this placement or activation design. This is **not a new regression from #1651**.
> - **Deferred boundary:** reconsider the installer, installed instruction entrypoint, and project-scoped activation together if this issue is later implemented. Preserve the user-owned global AGENTS, current explicit opt-in behavior, and valid repository authority/worker constraints; do not treat a simple text move or a condition based only on a matching file name as a complete design fix.
> - **Disposition:** known pre-existing design concern, **no immediate code/document change requested**. Keep it in this single consolidated tracking Issue; it is supplemental to, and does not alter the original 17 workstreams / 22 Phase 2 candidates.

## Purpose

リポジトリ固有のCodex指示とユーザーレベルの指示の責務・配置を分ける。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

最新Amendmentを適用する。codex/AGENTS.md等のリポジトリ固有ルールとユーザーレベルのルールの配置を分け、install.shからリポジトリ固有ルールをユーザーレベルに配置する経路を修正する。配置元、配置先、対象repositoryでの読み込み経路を着手時に確認し、用意されたPRを実際の要件に照らして精査する。

## Must not

ユーザー所有のAGENTSを変更しない。既存のopt-inと専用repositoryのauthority・worker制約を維持し、ファイル名の一致だけで適用しない。PR #1651で新たに入った退行と分類しない。

## Acceptance criteria

インストール後のユーザーレベルの指示にリポジトリ固有ルールが入らないことを確認する。opt-inあり、なし、固有Planなしの3条件で読み込み経路と内容を照合し、対象repositoryの規則とユーザー所有のAGENTSが保持されることを検証する。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
