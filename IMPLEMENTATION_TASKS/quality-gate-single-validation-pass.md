# Task: quality gateの重複検査解消

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexがレビューで採用した独立finding:

````text
runRepositoryQualityGateはharnesslint.Run(root,true)とCheck(root)を連続実行する。Run自体がfix後にcheckRulesとrunExternalChecksを実行するため、同じ検査が二重になる。fix後の正規検査を一度に整理し、coverageとfailure propagationを保つ。
````

## Amendments

none

## Resolved references

- `glm-worker/internal/workflow/quality_gate.go`、`internal/harnesslint/run.go`、同ownerの`quality_surface.go`が対象。現在のwiring検査はRun→Checkという呼出形状も固定している。machine処理の重複はsourceで確認済みだが、wall-clock削減量・Codex token削減率は未測定。
- 今回はレビューとfinding登録まで。実装修正は開始しない。

## Purpose

同じquality gate内の重複検査を減らす。

## Contract

- 正規fixer適用後の必要な内部・外部検査を一度だけ実行し、その結果を呼出元へ返す。
- fixer failure、検査failure、違反report、review snapshot境界を維持する。
- 変更するproduction wiringに合わせ、guardは必要invariantを検証する。古い呼出形状を維持する互換wrapperを作らない。

## Must not

- 異なるsnapshotの検査を再利用したり、検査項目を削って速くしない。
- test/reviewer/Sol gateを省略しない。old APIのaliasやfallbackを追加しない。

## Acceptance criteria

- production gate入口からの各fixer/check呼出回数と順序を検証できる。
- 正常・fixer失敗・検査違反・外部検査errorの結果が正しく伝わる。
- 同一入力でbefore/afterの検査回数と所要時間を比較し、token効果は実測できない場合unknownとする。

## Historical invariants

- Codex ReductionとQuality Deltaを同時に評価する。
- current schemaだけを正規入力とし、ユーザーdataの保護・current transaction recoveryと後方互換性を混同しない。

## Dependencies

none
