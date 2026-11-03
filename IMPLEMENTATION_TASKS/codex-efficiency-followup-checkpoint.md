# Task: 次回Codex効率中間評価

## Original instruction

2026-10-29 親Codex採用要求:
中間評価を最大5件のTask完了ごとに継続する。直前の中間評価と既存証拠を利用し、Codex Reduction / Quality Delta、未Task化Finding、internal/appの責務改善、Plan priorityを再評価する。重大incidentでは前倒しし、post-105評価は022直前に維持する。

## Amendments

### 2026-11-03 巻き取り完了後の先頭復帰

原文（順序どおり）:

````text
最後のコミットで
\- [\`IMPLEMENTATION_TASKS/codex-efficiency-post-adoption-checkpoint.md\`]\(IMPLEMENTATION_TASKS/codex-efficiency-post-adoption-checkpoint.md\)\
が先頭に戻るようにしておけよ
````

````text
IMPLEMENTATION_TASKS/codex-efficiency-followup-checkpoint.md
いやこっちだった
````

- 最新の訂正により、このfileを7候補の巻き取り完了後の最初の評価Taskとして再開する。実評価・採否はCodex自身が行い、終了Metadataで評価完了を先取りしない。
- 前回評価は`review-evidence/codex-efficiency-webgpt-adoption-2026-11-03.md`とし、その後のsource・検証・履歴整理・未Task化Findingとの差分を評価する。同じ責務で作った`codex-efficiency-post-adoption-checkpoint.md`の評価要求を本Taskへ統合する。

## Purpose

優先順位と改善の実効性を開発途中で再確認する。

## External feasibility

status: not-applicable

## Execution authority

mode: parent-only

## Contract

- 親Codexだけで評価し、追加GLM・他モデル呼出を行わない。
- `review-evidence/codex-efficiency-webgpt-adoption-2026-11-03.md`を前回評価として、差分と新証拠へ限定する。
- 比較条件不一致・欠損はunknownに保ち、機械処理の短縮をCodex削減率へ外挿しない。
- 全Taskの優先順位と重複を確認し、具体的な改善を計画化する。次回中間評価を最大5件後へ配置する。

## Must not

- production実装、全体再監査、新しい本番A/Bを開始しない。

## Acceptance criteria

- 比較期間・source・使用量・品質の証拠と限界を記録する。
- Planの優先順位、次回checkpoint、022直前のpost-105配置が整合する。
- appの単なる行数削減と責務改善を区別する。

## Historical invariants

- 品質維持とCodex実消費削減を同時に評価する。

## Dependencies

none
