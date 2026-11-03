# Task: Pull後の機械的source再同期と同等Review証拠の継承

## Original instruction

2026-11-04 ユーザー指示:

```text
git pullした際に再同期するのは機械的にやってもいいんじゃないのか
あと再同期した際レビューやり直しになるがMetadataしか更新してなければレビューやり直しにする必要ないんじゃないのか
```

## Amendments

none

## Resolved references

- [Audit #1503](https://github.com/shinderuman/codex-worker-orchestrator/issues/1503)、Bundle `31f4370c-6776-42a3-b3b2-d14bb5bafd54`。Pull `c8acd619→dc099fe3`の変更はPlan NEXT一行と後続Task追加のみ。ACTIVE・Rules・実装差分不変、初回・再Reviewerのtargetsとworktree digestも一致。
- Pull後に既存`adopt-mutable`等を親が探索し、`materialize`のterminal projection overflowとreview binding失効を経由。`accept`が正しく拒否され、Reviewerを再実行した（約205.5秒）。先行ReviewerのHIGHリスク・未解決のDarwin test懸念は残存していた。
- [Audit #1420](https://github.com/shinderuman/codex-worker-orchestrator/issues/1420)にもMetadataのみのPull後のrebindと追加Reviewを観測。現在の`publication_source_continuity`・`publication_mutable_adoption`と`parent_review_evidence`の正規ownerを再利用する。

## Purpose

すでに完了したfast-forward Pullの後処理を機械的に収束させ、Reviewが判断した対象が本当に不変なら、独立Reviewerモデル呼出を無条件にやり直さずに済ませる。

## External feasibility

status: not-applicable

## Contract

- Pull済みprimary/remoteのfast-forward、lease・Task authority・snapshot・未commit変更・source consistencyを既存Controllerで検証し、合法な`adopt-mutable`/rebindと必要な後続状態遷移を機械的に実行する。親CodexにCLI探索、手動State編集、同じ段階の繰返し準備を要求しない。
- Pullの前後で、ACTIVE/Rules/Task契約・参照閉包、review対象のコード/テストと依存source、worker結果・source snapshot・Reviewer targetsを比較する。**同等性を機械的に証明した場合だけ**、元のReviewer packet・risk・未解決指摘・targets・provenanceを保持して新しいsource bindingを正規に確定する。元packetの書換えやReviewer PASSの偽造はしない。
- 参照・契約・review対象に変更がある、差分分類が不明、競合・lease stale・不正なremote advanceの場合は旧Reviewの利用を拒否し、必要な再Review/修正/検証またはSTOPへ進む。ユーザーのPull指示をremote更新の包括的自動許可に拡張しない。
- 親Codexの意味判断と独立Reviewの品質、Publication前のsource-bound検証、Task一件一Commitの条件を維持する。第二Controllerや汎用復旧frameworkを増やさない。

## Must not

- 「Metadata」という拡張子・ディレクトリ分類だけでReviewを省略しない。ACTIVE、Rules、参照文書、review対象の意味を変える変更は同等扱いしない。
- 失効したReview証拠を無検証で復活させず、旧snapshotのdigestを新snapshotのdigestと偽ることもしない。
- Pullを勝手に実行する権限追加、品質Gateの省略、既存のfail-closed判定の緩和を行わない。

## Acceptance criteria

- 監査#1503のMetadata-only fast-forwardを再現し、同一Task・同一review対象・同一実装差分を証明して、**追加Reviewer呼出0回**で機械的source再同期と既存HIGH reviewの正規継承・親採否・Publicationまで完走する。
- ACTIVE/Rules/Task契約、対象コード/テスト/依存sourceが変わる例と、競合・非fast-forward・stale lease等の負例では継承を拒否し、再Review/STOPの正規経路を維持する。
- 同条件のbefore/afterでPull後の親model往復、Reviewer実call、実行時間を測定し、待機と実処理を分ける。新旧sourceに対する正負の証拠と最終差分を残す。

## Historical invariants

- 既存`reviewer-packet-correction-reentry.md`の旧source Review無検証利用の拒否を維持する。同等性が証明されたReview証拠の継承と、別のsourceへ無条件に流用することを区別する。

## Dependencies

none
