# Task: Metadata変更時に同等な独立Review証拠を再利用する

## Original instruction

2026-11-04 ユーザー指示：

```text
あと再同期した際レビューやり直しになるがMetadataしか更新してなければレビューやり直しにする必要ないんじゃないのか
```

## Amendments

none

## Resolved references

- [Audit #1503](https://github.com/shinderuman/codex-worker-orchestrator/issues/1503)、Bundle `31f4370c-6776-42a3-b3b2-d14bb5bafd54`。Pull `c8acd619→dc099fe3`の変更はPlan NEXT一行と後続Task文書追加のみ。ACTIVE・Rules・Review対象コードは不変で、初回・再Reviewerのtargetsとworktree digestが一致。それでもReview bindingが失効し、再Reviewerに約205秒を費やした。
- [Audit #1420](https://github.com/shinderuman/codex-worker-orchestrator/issues/1420)にもMetadataだけのPull後の再Reviewを観測。既存のparent review evidence、source binding、独立Reviewerの正規ownerを再利用する。

## Purpose

sourceの再同期後もReviewの実質的な対象と判断前提が不変なら、既存の独立Review証拠を再利用してモデル呼出を削減する。

## External feasibility

status: not-applicable

## Contract

- Pull前後のACTIVE/Rules/Task契約・参照閉包、Review対象のコード・テスト・依存source、Worker結果・snapshot・Reviewer targetsを照合し、**元Reviewの判断前提の同等性**を機械的に検証する。「Metadata」というファイル分類だけでは判定しない。
- 同等性が証明された場合のみ、元のReviewer packet、HIGHリスク、未解決指摘、targets、provenanceを正規の新source bindingへ引き継ぐ。元packetの書換え・架空のReviewer応答・PASSへの格上げはしない。
- 変更や不明点があれば旧Reviewの継承を拒否し、通常の再Review・修正・検証を要求する。source不一致・古い証拠の無検証利用を許可しない。
- **Pull後の再同期処理の機械化は本Taskの範囲外**。既存の合法なsource再同期経路だけでも、同等性検証・Review再利用が独立して機能するようにする。

## Must not

- ACTIVE、Rules、Task、参照資料、Review対象の意味が変わる変更を単にMetadataという理由で無視しない。
- 独立Reviewの省略を一般化しない。既存のfail-closedとPublication前source-bound検証を弱めない。
- 第二のReview状態DBや互換fallbackを追加しない。

## Acceptance criteria

- Audit #1503のMetadata-only fast-forwardを再現し、同等性を機械的に証明した上で既存HIGH Review証拠を継承し、**追加Reviewerモデル呼出0回**で親が正規の採否判断へ進める。
- ACTIVE/Rules/Task/参照先/Review対象や依存テストの変更、および同等性を判定できない例では継承を拒否し、再Reviewを要求する。
- 同一条件のbefore/afterでReviewer実call数・モデル消費・実処理時間と検証範囲を比較する。旧packetのprovenanceと最終source bindingを照合する。

## Historical invariants

- `reviewer-packet-correction-reentry.md`の旧source Reviewを無検証で利用しない規則を維持する。同等性の証明を伴う継承だけを許す。

## Dependencies

none
