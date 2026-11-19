# Task: Docker診断の不完全な応答を成功扱いしない

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

Docker診断の不完全な応答を成功扱いしない。

## External feasibility

status: not-applicable

## Contract

**Docker診断が不完全な応答を成功として返す。** `dockertest/staging.go` の `ParseStagingResponse` はschemaだけを検査し、`StagingClient.Run` は整数の初期値をそのまま返す。正しいtokenで、終了コード欠落、null、明示0とfailure併記の3ケースがすべて終了0になった。

## Must not

実装時の現行入口で確認し、旧schemaの推定変換や二重の判定元を導入しない。

## Acceptance criteria

完成した応答には明示的な終了コードを要求し、失敗理由がある成功を拒否する。正常な0と非0を区別する。runnerは完成ファイル名に直接書くため、書きかけのJSONを読んだ瞬間に確定失敗させない。別tokenの応答の隔離も維持する。これはstaged診断の再現で、実際にpublicationが誤って通ったとする根拠はない。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
