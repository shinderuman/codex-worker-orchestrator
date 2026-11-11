# Task: テスト時間改善後のCodex効率中間評価

## Original instruction

親Codexの継続採用要求:

```text
中間評価を最大5件のTask完了ごとに継続する。review-evidence/codex-efficiency-followup-2026-11-03.mdを前回評価とし、その後のsource・検証・未Task化Findingとの差分へ限定して、Codex Reduction / Quality Delta、appと他packageのテスト実行時間、Plan priorityを親Codex自身が再評価する。重大incidentでは前倒しし、post-105評価は022直前に維持する。
```

## Amendments

none

## Resolved references

- Original instruction が指す前回評価のhistorical evidenceは[固定コミットの評価本文](https://github.com/shinderuman/codex-worker-orchestrator/blob/ef0b318494cce6a1119c467b1a6292ef114d1db8/review-evidence/codex-efficiency-followup-2026-11-03.md)から取得する。比較対象の過去評価であり、current requirementは本TaskのOriginal instruction・Contractを正とする。
- Publication の親往復の採否・欠測・再評価条件は [評価コミット](https://github.com/shinderuman/codex-worker-orchestrator/commit/af5f0b78033e5490e0f78ecc2d943c761ba34bd9) を参照する。
- 親セッション rotationの採否・欠測・再評価条件は [評価コミット](https://github.com/shinderuman/codex-worker-orchestrator/commit/ab0600d0012d4a1a0d5048ea51a37318d254c40b) を参照する。
- Codex usage の比較可能性の採否・欠測・再評価条件は [評価コミット](https://github.com/shinderuman/codex-worker-orchestrator/commit/220e453396a52f40efdfdd44c483c31a1910810b) を参照する。

## Purpose

品質と実消費・テスト時間の改善が成立したかを途中で再確認する。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

- 親Codexだけで評価し、追加GLM・他モデル呼出を行わない。
- 前回評価と既存artifactを再利用し、新証拠と差分へ限定する。
- 比較条件不一致と使用量欠損はunknownに保ち、fixture数・行数・時間をCodex削減率へ換算しない。
- appと他packageのtest時間、品質coverage、反復machine処理、無変化再入、未Task化Finding、全Taskの優先順位と重複を確認する。
- CIのjob/shard別時間とcritical pathの平準化、importer Panicとparent-only Bundle生成失敗の対応結果を既存証拠から評価する。
- 次回checkpointを最大5件後、post-105を022直前に維持する。

## Must not

- production実装、全体再監査、新しい本番A/Bを開始しない。
- BLOCKEDのtest selection/retirement等の許可条件を解除しない。

## Acceptance criteria

- 比較期間・source・使用量・品質・test時間の証拠と限界を記録する。
- 実行可能な改善を重複なく計画化し、Planと次回checkpointの整合を確認する。
- machine実行時間の短縮とCodex実消費の削減を区別する。

## Historical invariants

- 品質維持とCodex実消費削減を同時に評価する。

## Dependencies

none
