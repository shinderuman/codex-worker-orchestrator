# Task: A/B比較の独立したworktreeを検証する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

A/B比較の独立したworktreeを検証する。

## External feasibility

status: not-applicable

## Contract

**同じ作業ツリーで採取したA/B記録を独立と判定できる。** `abeval/validate.go::ValidatePair` の比較は `filepath.Clean` した文字列。実在する一時directoryとそのsymlinkを両側のworktreeに指定すると、他の条件が妥当なpairとして通った。

## Must not

実装時の現行入口で確認し、旧schemaの推定変換や二重の判定元を導入しない。

## Acceptance criteria

採取時に解決したworktree identityを比較証拠へ結び付ける。保存済み記録の検証時に元パスが存在しない場合も考慮し、証明できない独立性を確定しない。別worktree・同じworktreeの別名・削除済みworktreeを検証する。過去schemaを推定変換しない。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
