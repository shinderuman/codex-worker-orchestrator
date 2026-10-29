# Task: appのController・runtime遷移調停を単一ownerへ集約する

## Original instruction

2026-10-29 親Codex採用要求:
appがController更新とruntime更新の順序・証拠採取・失敗回収を所有する構造を解消する。既存のjournalと正規遷移ownerを利用し、appはtyped CLI入力と結果出力を担当する。初期化前runtime、同期、復旧、handoffを同じ状態契約へ接続し、今後の機能追加でもappへ個別の復旧分岐を積み増さない。

## Amendments

none

## Resolved references

- 根拠: `review-evidence/codex-efficiency-checkpoint-2026-10-29.md`。
- 具体的な要求sync不整合は`task-revision-worker-admission.md`、Publication sourceは`canonical-publication-source-continuity.md`、輸送は`communication-transport-unification.md`が所有する。

## Purpose

遷移の成功・部分失敗・再実行の意味をCLIから分離し、追加のたびに調停処理がappへ集中する構造を解消する。

## External feasibility

status: not-applicable

## Contract

- Controllerからworkflowへ依存するcycleを作らず、既存の遷移ownerとworkflow接続の責務を親判断で確定してから実装する。
- source/task/generation/leaseとruntime bindingの整合、途中errorの実副作用、正規復旧後のhandoffを一つの遷移契約で扱う。
- appに順序policyや重複判定を残さない。必要な異なるtrust boundaryの再検証は保持する。
- owner単体の失敗点検証と、CLIからの少数の正常・中断・復旧接続testに分離し、production手順をfixtureのstate補正で代替しない。

## Must not

- 別journal/state DB/daemon、汎用巨大framework、旧schema互換を追加しない。
- ファイル移動のみ、error握り潰し、bindingだけを手動補正したテストを改善と扱わない。
- 先行Taskで修正済みの具体的不具合を再実装しない。

## Acceptance criteria

- appのtyped dispatchからownerへ一度委譲し、意味的な遷移手順・復旧がowner内で閉じる。
- 同期前・Controller更新後・runtime更新後の中断からidempotentに回復し、handoffの状態と実副作用が一致する。
- 新しい同期条件を追加する変更例でappへのpolicy分岐追加が不要なことを示す。
- 必要な品質検証を保持し、重いfixture反復を減らした根拠を示す。

## Historical invariants

- source-bound evidence、権限分離、fail closed、親の最終採否を維持する。

## Dependencies

- `IMPLEMENTATION_TASKS/task-revision-worker-admission.md`
- `IMPLEMENTATION_TASKS/canonical-publication-source-continuity.md`
