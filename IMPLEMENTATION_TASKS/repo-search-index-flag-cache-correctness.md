# Task: 検索cacheとtracked実contentの一致

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
assume-unchanged等のindex flagがあっても、repo-searchのcache-hitを古い内容の正当化に使わない。検索corpusとfingerprintの意味を一致させる。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の A7。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

assume-unchanged等のindex flagがあっても、repo-searchのcache-hitを古い内容の正当化に使わない。検索corpusとfingerprintの意味を一致させる。

## External feasibility

status: not-applicable

## Contract

- 一時repoでassume-unchangedを付けたtracked fileをcache構築後に変更すると、cached検索だけ0件になる不具合を修正する。
- tracked raw contentを反映する方式か、対応できないflagを入口で明示拒否する方式かを親が確定する。skip-worktree/sparse checkoutの対応範囲も明示する。
- cache keyだけでなく、検索前後の変更検査とexhaustive表示も同じcorpus契約へ合わせる。controller側の別guardをstandalone検索入口の代替にしない。

## Must not

- stale cacheから0件を成功として返さない。unsupported状態を通常empty resultへ縮退しない。
- 旧cacheのmigration、推定promotion、追加model callによる再検索fallbackを追加しない。
- 毎回のrepository全走査を費用確認なしで採用しない。

## Acceptance criteria

- assume-unchanged付きfileのoldneedle→newneedle変更で、cached/uncached検索が同じ正規結果になるか、unsupportedとして明示拒否される。
- skip-worktree/sparseの有無、flag切替、検索中変更、通常cache-hitを検証する。
- correctnessとexhaustive表示を保ち、代表的corpusでcache費用への影響を測定し、related testsとquality gateを通す。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
