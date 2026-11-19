# Task: 予算を増やした再検索で未配信の結果を受け取れるようにする

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

予算を増やした再検索で未配信の結果を受け取れるようにする。

## External feasibility

status: not-applicable

## Contract

glm-worker/internal/app/repo_search.go の printRepoSearchWithSearch は質問、スコープ、パスと行からdigestを作る。buildRepoSearchOutput は同じ入力結果でもbudget不足ならlocatorを返さず、budgetを増やすよう案内する。glm-worker/internal/parentevidence/support.go の FinishReadInScopeResult はrefinementにもSaveLedgerを呼ぶ。同じdecision leaseで指示どおりbudgetだけ増やすと、結果は同じdigestでDecideReadがrender前に重複として拒否する。app/repo_search_test.go の TestPrintRepoSearchRefinementIsDeduplicatedWithinDecisionLease は同じ小さいbudgetを二度使うケースだけを固定している。

## Must not

同じleaseでの重複抑制と未配信の区別を保つ。

## Acceptance criteria

受入条件は同じlease・質問・scopeで「不足→同じ不足」は重複として抑え、「不足→十分」は一度locatorを配信し、「配信済み→十分」は再び抑えること。既存の集約配信とstandalone配信の相互重複判定でも確認する。親に再検索・lease切替を要求する経路が減ったかを、呼出数と配信bytesで測る。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
