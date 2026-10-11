# Task: 親向け証拠の最終出力上限

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
親向けevidenceはbodyだけでなくmetadataを含む最終serialized output全体へ上限を適用し、未送信証拠を配信済みにしない。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の A4。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

親向けevidenceはbodyだけでなくmetadataを含む最終serialized output全体へ上限を適用し、未送信証拠を配信済みにしない。

## External feasibility

status: not-applicable

## Contract

- Project/Commitのrelease前に最終serialized bytesを制限する。metadataだけで上限を超えるmanifestも正規に処理する。
- 全partを収容できない場合のbounded refinement/errorを定義し、出していないpartをdelivered/reviewedとしてledgerへcommitしない。
- existing repo-search result context budgetとはparent evidence最終releaseの別責務として扱う。個々のbody上限だけの調整では完了にしない。

## Must not

- 上限を引き上げるだけ、切れたJSONのstdout出力、silentなpart欠落で解消しない。
- omitted proofの成功扱いや旧無制限outputとの互換モードを追加しない。

## Acceptance criteria

- 64KiB未満の480-part manifestで98,304 bytesを超えた再現を正規ownerのtestへ移し、成功・error双方の出力がcurrent budget内の有効JSONになる。
- metadata、長いlocator、multibyte text、body省略後、validation metadataの境界を検証する。
- 失敗・省略した出力がdelivery/review proofを成立させず、正常な部分取得/再要求が可能である。related testsとquality gateが通る。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
