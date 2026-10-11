# Task: controller内部Git archive費用評価

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
内部durable Git archiveでrootごとに全reachable履歴をpackする費用を測定し、証拠の耐久性と品質を維持できる改善だけを採否判断する。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の C1。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

内部durable Git archiveでrootごとに全reachable履歴をpackする費用を測定し、証拠の耐久性と品質を維持できる改善だけを採否判断する。

## External feasibility

status: not-applicable

## Contract

- baseline/candidate/accepted seal/terminal各取得のtime、pack/envelope bytes、共通object、disk retentionを代表履歴で測定する。parent再入・再調査と実Codex消費への寄与は、測れた範囲だけ報告する。
- portable Bundle exportと内部durable archiveのownerを区別し、Bundle修正済みという理由で内部重複の費用を解消済みとしない。
- MEASURE-FIRSTとし、効果が小さい場合のno-changeを正規完了にする。改善案に永続化意味・cleanup graphの変更が必要なら、採用前に親が具体設計を確定する。
- 有意な改善を採用する場合もcurrent canonical evidence storeとseal graphを正とし、offline復元、検証、cleanup後の到達性を維持する最小変更にする。

## Must not

- object重複観測だけでCodex削減率や全体bottleneckを断定しない。
- 第二store、旧schema migration、無条件history切捨て、検証省略を先行実装しない。
- 長期archiveや新daemonの増設を評価の既定解にしない。

## Acceptance criteria

- 小規模履歴、大blob削除済み履歴、複数seal/rootの比較で費用内訳を再現可能に示す。
- 採用/no-changeを測定値と品質条件で親が判断する。削減不立証もそのまま記録する。
- 採用時はcleanup後のoffline復元・seal検証・missing object拒否のregressionを検証し、関連quality gateを通す。no-change時も評価の入力・方法・限界が回収可能である。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
