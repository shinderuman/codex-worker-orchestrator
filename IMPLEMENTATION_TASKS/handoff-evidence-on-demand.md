# Task: handoffの応答に必要な証拠だけを読み込む

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B08. Avoid eager broad evidence for bounded terminal/recovery handoffs — #1641
> - **Owner/problem:** `app/parent_handoff.go`, `parent_handoff_automation.go`, `parentactioncmd/terminal_envelope.go` first materialize full baseline/snapshot/quality history and only then discard them for terminal/recovery views.
> - **Historical rationale:** full handoff was canonical before bounded terminal (#897) and recovery presentations were layered on top.
> - **Implement/minimum boundary:** choose canonical semantic view *before optional evidence collection*; pass a genuinely narrow internal handoff to terminal/recovery, while full `--handoff` retains its current evidence requirements. Do not add a parallel lifecycle state machine.
> - **Preserve/risk:** handoff/action identity, controller/read scope, current advisory and recovery failure fields, byte caps, full mode source-current validation, fail-closed errors, typed JSON and required proof.
> - **Acceptance:** same accepted full/recovery/terminal outputs, explicit wide vs narrow source visits measured (intermediate bytes and quality-run reads), full evidence consumer unchanged; B03 advisory retained.

## Purpose

handoffの応答に必要な証拠だけを読み込む。

## External feasibility

status: not-applicable

## Contract

appのfull handoffを作った後でterminal/recovery用に削る。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

必須項目とsource再検証を維持し、使用しないquality履歴の読取数と中間bytesをbefore/after比較。B07の履歴を判断をまたいでcacheしない。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

- `IMPLEMENTATION_TASKS/bounded-terminal-packet-fields.md`


## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/pending-controller-handoff-owner.md`
