# Task: 検索cacheと再構築で同じ件数上限を適用する

## Original instruction

リポジトリのHEADに対して監査する→Issueの内容とMergeする→タスク化する

## Amendments

none

## Resolved references

監査対象は `6279e0379b9abc9ac1154cbd1a83a2e7c2e38dd1`。

## Purpose

検索cacheと再構築で同じ件数上限を適用する。

## External feasibility

status: not-applicable

## Contract

glm-worker/internal/reposearch/document.go の rebuildIndex はスキップ対象も含む列挙数をMaxFilesと比較し、cache.go の cacheCountsValid はIndexedFilesだけを上限と比較する。テキスト1件とbinary1件を上限2で構築後、同じ内容を上限1で読めばhitの判定と再構築の判定が一致しない。現行worker/reviewerは既定上限を指定し、親の2入口はcache無効。影響を異なる上限を指定したSearch呼出に限定する。

## Must not

既定値を使う本番入口まで影響があると仮定しない。

## Acceptance criteria

受入時は同じfixtureでcache有無と上限変更の両順序を照合する。

## Historical invariants

現行形式と正規の判定元を使う。実消費が観測できない計測値は代理指標として扱う。

## Dependencies

none
