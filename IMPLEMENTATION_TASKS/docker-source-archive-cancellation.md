# Task: Dockerのsource archive作成に取消を伝える

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

Dockerのsource archive作成に取消を伝える。

## External feasibility

status: not-applicable

## Contract

**Docker向けsource tar作成中に取消が届かない。** `dockertest/source.go::sourceArchive` はGit操作にctxを渡す一方、`writeArchive`、Git directoryのwalk、`archiveEntry`のcopyには渡さない。大きな作業コピーでは取消後も残りを読める構造。静的確認で、遅延時間は未測定。

## Must not

実装時の現行入口で確認し、旧schemaの推定変換や二重の判定元を導入しない。

## Acceptance criteria

同じctxを列挙・copyに伝え、制御可能なreaderによる取消テストで残り全件を走査せず戻ることを確認する。通常時のdigest、source同一性、cleanupを維持する。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
