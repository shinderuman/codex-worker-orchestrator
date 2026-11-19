# Task: rolloutの重複確認で同じファイルを繰り返し全読みしない

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

rolloutの重複確認で同じファイルを繰り返し全読みしない。

## External feasibility

status: not-applicable

## Contract

glm-worker/internal/codexrollout/chain.go の ResolveChain → duplicateContent → duplicatePair → filesIdentical は同じサイズの候補を全対比較し、比較のたびに両方のSHAを再計算する。同一サイズで内容の異なるn件ではこの比較だけで n(n−1) 回の全読になる。controller/runtime_evidence_sessions.go と bundleanalysis/bundle_codex.go が呼出元。

## Must not

個別判断間のキャッシュを共有しない。変更中の入力を不変と仮定しない。

## Acceptance criteria

候補グループごとに一判定内でdigestを再利用し、異なるサイズ、同じ内容、読取失敗を同じ基準で扱う。同一サイズ8件なら56回が改善前のコード上の全読回数。実時間とtoken効果は未計測。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
