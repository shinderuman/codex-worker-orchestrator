# Task: 親向け差分証拠の同一性とdedup

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
親へ届ける差分が変わったときに、同じdigestを理由に既読として省略しない。mode変更とdirectory配下の変更を含む正規path契約を確定する。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の A3。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

親へ届ける差分が変わったときに、同じdigestを理由に既読として省略しない。mode変更とdirectory配下の変更を含む正規path契約を確定する。

## External feasibility

status: not-applicable

## Contract

- parentevidenceのdiff identityを、実際に届ける対象のcontent/type/modeと差分の意味に一致させる。questionや先頭index blobだけで同一と判断しない。
- directory/pathspecを正規入力とするか、明示拒否するかを実装前に親が確定する。許可する場合は全対象をidentityへ含め、regular-file hashを取れない対象を空identityの成功にしない。
- 同一証拠の再送削減は維持し、変更された証拠だけが再送されるようにする。deliveryとreview proofのbindingも確認する。

## Must not

- 旧digestのalias、migration、旧path仕様への互換fallbackを追加しない。
- 常時全量再送やdedup全廃を無検証の解決策にしない。
- 本文だけ更新してdelivery ledgerのidentityを旧方式のまま残さない。

## Acceptance criteria

- mode-only変更と、directory指定後の子file変更で、意味の違う差分がunchanged扱いされない。directoryを非対応にする場合は明示拒否を検証する。
- content/rename/delete/untracked/symlinkを正規path契約に沿って検証する。未変更の再要求は正しくdedupする。
- 表示内容とdigest・delivery/review proofが対応し、変更検出のregression testとquality gateが通る。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
