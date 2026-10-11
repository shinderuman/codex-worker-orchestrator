# 2026-10 再監査の証拠

`request.md`は共通原要求、`reproductions.json`はfixture/package/testの対応と元log行付き診断、`*.go.txt`は外部snapshotで実行した限定再現である。意味・影響・限界はルート `Review.md` の「2026-10 再監査」を参照する。

実行は親directoryの `review-evidence/README.md` にあるGo overlay手順を使い、選んだfixtureだけを対応packageの未存在test pathへ割り当てる。A1/A2/A3/A4/A6/A7/A8のtestは当時の不具合に対する安全側assertionでFAIL、C1のtestは重複を観測してPASSした。A5は静的参照、C2はsourceと既存cohortの評価課題である。実GLM・production lifecycleは呼び出していない。

archive元commit以降のsource形状に自動追従しない。実装時は必要なregressionをcurrent正規ownerへ移し、旧API互換層を作らない。ここを恒久test suiteや追加のruntime正本にしない。今回と前回の対応Taskがすべて完了した後、既存cleanup Taskで `Review.md` と `review-evidence/` 全体を削除する。
