# Task: 通常Task admissionのharness適用範囲

## Original instruction

親Codexによる再監査結果のTask化指示:

````text
通常Task入口がharness inactiveの他repositoryにもcommitted Planを必須にする不整合を修正する。repository固有guardとgeneric executionの境界を親判断で確定する。
````

共通のユーザー原要求は `review-evidence/october-2026/request.md` を明示参照する。同fileは今回レビュー由来の全Task完了まで保持する。

## Amendments

none

## Resolved references

- `Review.md` の「2026-10 再監査」の A8。診断・再現条件・確認限界を参照する。
- `review-evidence/october-2026/README.md` の対応fixtureと記録。レビュー当時の証拠でありcurrent sourceへの互換要件ではない。

## Purpose

通常Task入口がharness inactiveの他repositoryにもcommitted Planを必須にする不整合を修正する。repository固有guardとgeneric executionの境界を親判断で確定する。

## External feasibility

status: not-applicable

## Contract

- marker/Planのない他repositoryへ本repository固有guardを一般化しない規則と、activateWorkflowConfigの無条件controller activationを整合させる。
- 非harness利用で必要な現行task authorityと実行入口を親が確定し、明示controller-activateとのadmission差を説明可能にする。repositoryharnessの既存判定を再利用する。
- harness active側ではcommitted Task/lease/parent metadata不変性の検証を維持する。非Git利用も正規support範囲を明示し、未対応を曖昧なPlan読取失敗へしない。

## Must not

- 旧worker経路、dual authority、Plan自動生成、旧schemaやaliasへの互換fallbackを復活させない。
- markerを消してguard回避する実装やactive側のfail-openを追加しない。
- 今回のfixtureはmodel未呼出しのadmission再現であり、実モデルの成功を主張しない。

## Acceptance criteria

- READMEだけをcommitしmarker/Planがないrepositoryで、通常Task入口が本repository固有Planを暗黙要求しない。
- active/inactive/malformed marker、明示controller操作、正規非Git利用方針の各入口を検証する。
- active task/lease/metadataの既存保護を維持し、正規dispatchまでのscripted runner testとquality gateを通す。

## Historical invariants

- current schema/current ownerだけを正とし、旧形式へのmigration・promotion・推定fallbackを追加しない。
- 機械判定の限界と親の意味判断を分離し、Codex ReductionとQuality Deltaを同時に評価する。
- 再現fixtureは実装開始時の正規ownerへ移す。古いsource shapeの互換層を作らない。

## Dependencies

none
