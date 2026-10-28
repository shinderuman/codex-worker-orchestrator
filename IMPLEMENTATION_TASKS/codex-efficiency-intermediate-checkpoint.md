# Task: Codex efficiency intermediate checkpoint

## Original instruction

```text
post-105-codex-efficiency-reevaluation.mdを定期的にやってほしいんだが奥に追いやられて一向に行われないので同様のタスクをなるべく早いタイミングでやってくれ
いまのpost-105-codex-efficiency-reevaluation.md自体は022の直前に残す
だが現段階の再評価をしてほしい
そして再評価したあとにタスクの優先度つけ直しをしてほしい
```

## Amendments

2026-10-28: 「そもそも全履歴の重複を省くんじゃなくて陳腐化したルールは削って原要求だけにするレベルでいいだろ」。過去の監査・作業経路・個別Finding対応に由来する時限的な追加要求は本Taskの実施条件から外す。

## Purpose

開発途中のCodex ReductionとQuality Deltaを再評価し、その結果をもとに今後のTaskの優先順位を見直す。

## External feasibility

status: not-applicable

## Contract

- 現在利用できる実測・telemetry・既存評価を使い、Direct CodexとCodex + glm-workerの効率・品質を比較する。比較不能・未測定の項目は不明とする。
- 効果が出た改善、残る問題、未着手のTaskを整理し、`IMPLEMENTATION_PLAN.local.md`のACTIVE/NEXT/BLOCKEDの優先順位を再評価する。実際の変更が必要な箇所だけ更新する。
- 個別の調査や修正は既存Taskを正とし、完了済みの総合レビューをやり直さない。評価に必要な証拠はGit・CI・Bundle等の正本を参照し、本Taskへ複製しない。
- 中間評価を今後も継続できるよう、次の評価時点をPlanへ配置する。`post-105-codex-efficiency-reevaluation.md`は022の直前に維持する。

## Must not

- 評価を理由にproduction実装、不要な追加モデル呼び出し、大規模な再監査を開始しない。
- 品質低下・検証省略・不明な使用量を、Codex Reductionの改善と扱わない。

## Acceptance criteria

- 比較対象、期間、Codex使用量と品質の証拠・限界を簡潔に記録する。
- 全Taskの優先順位を見直してPlanに反映し、既存Taskとの重複と未解決事項を区別する。
- 次回の中間評価と、022直前のpost-105最終評価をPlanで確認する。

## Historical invariants

- Sol High相当の品質を保ちながら、Codex/Solの実消費量を減らす。

## Dependencies

none
