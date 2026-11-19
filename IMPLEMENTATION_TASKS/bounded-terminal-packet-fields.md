# Task: 上限付きterminal応答でもpacketのadvisoryを配信する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。 [Issue #1649](https://github.com/shinderuman/codex-worker-orchestrator/issues/1649) の下記項目を統合した。

> ### B03. Packet-owned semantic fields in bounded parent terminal — #1633
> - **Owner/problem:** `packet/contract.go` defines statuses/result fields but `parentactioncmd/terminal_projection.go` repeats a status whitelist. A bounded overflow can silently drop emitted `failure_path_advisory`.
> - **Historical rationale:** #897 introduced bounded transport downstream of canonical packet schema.
> - **Implement/minimum boundary:** project terminal semantic fields from **one narrow packet-owned contract**, preserving wrapper-owned terminal additions; keep byte-budget machinery with terminal transport.
> - **Preserve/risk:** schema/status decisions, provenance/advisory, `targets`/`artifacts`, machine JSON, safe overflow/refinement, exact size caps, no second schema or repository policy.
> - **Acceptance:** force actual advisory-bearing PASS/review/decision packets through overflow and prove retention or explicit safe overflow; all valid/invalid terminal contracts pass.

## Purpose

上限付きterminal応答でもpacketのadvisoryを配信する。

## External feasibility

status: not-applicable

## Contract

packetの状態別契約とterminal側の `terminalProjectionFields` の二重化。後者にadvisoryがなく本文が落ちる。省略キーを記録するので完全に無通知とは断定しない。

## Must not

元Issueの当該項目にある保護対象を引き継ぐ。旧形式への移行、廃止した入口へのalias、再検証や独立した証拠の省略を追加しない。

## Acceptance criteria

実際にoverflowさせ、advisoryの意味が届くか安全なrefinementになること、最終byte上限を確認。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
