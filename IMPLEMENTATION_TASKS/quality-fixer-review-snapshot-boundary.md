# Task: quality fixerとreview snapshotの境界整合

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexがレビューで採用した独立finding:

````text
reviewUntilStableはworker-end snapshotを保存してからquality gateを呼ぶ。quality gateはmachine-fixでworking treeを変更できるため、検査が成功してもverifyReviewStartSnapshotが整形前後の差分を外部変更と判定し、reviewerを呼ばずwaiting-sol-reviewへ停止する。正規fixerの変換と外部変更を区別し、前者が不要な親Codex復帰を発生させないようにする。
````

## Amendments

none

## Resolved references

- `glm-worker/internal/workflow/review_flow.go`の`reviewUntilStable`、`quality_gate.go`の`runRepositoryQualityGate`、`workflow.go`の`verifyReviewStartSnapshot`が対象。
- 今回はレビューとfinding登録まで。実装修正は開始しない。再現は実モデルを使わず、既存workflow fixtureとGo formatterによるworking tree変換で確認した。

## Purpose

正規のmachine-fix後に不要なSol reviewを発生させず、独立reviewへ進める。

## Contract

- worker出力、正規machine-fix、review入力それぞれのsnapshot責務を明確にする。
- 正規fixerの変更を根拠付きでreview対象へ含める。snapshotの取り直しだけで外部変更を黙認しない。
- quality gate失敗時の既存recoverable経路、review中の外部変更検出、parent metadata保護を維持する。

## Must not

- snapshot guard、independent review、semantic acceptanceを省略しない。
- old stateのmigration、alias、推定fallbackを追加しない。
- Codex Reductionのために全変更をtrusted fixer由来と推定しない。

## Acceptance criteria

- machine-fixが実際にfileを変更して検査成功した場合、不要な親復帰なしにreviewerが変更後の内容を確認できる。
- fixer以外の変更、quality gate failure、review中の変更が引き続き適切に停止する。
- formatterとsnapshot検証の合成をproduction workflow経路で検証する。各機能の独立mock成功だけで十分としない。

## Historical invariants

- Codex ReductionとQuality Deltaを同時に評価する。
- current schemaのみを正規入力とする。

## Dependencies

none
