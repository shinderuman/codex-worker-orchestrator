# Task: 最終検証task順序policyの責務分離

## Original instruction

````text
glm-workerの責務がリポジトリのルールを実装してないか
逆はないかとか総合的に考えてほしい
````

親Codexがレビューで採用した独立finding:

````text
汎用PlanScheduleを所有するtaskcontractが、このrepository固有の022-final-verification.mdとその後続task禁止policyを持っている。production consumerはharnesslintの順序検査であり、repository固有policyをそのownerへ移し、汎用task parserの責務から外す。
````

## Amendments

none

## Resolved references

- `glm-worker/internal/taskcontract/final_verification.go`と`internal/harnesslint/schedule_closure.go`が対象。他repositoryへ無条件にpolicyが適用される障害を確認したわけではなく、code ownershipの改善として扱う。
- 今回はレビューとfinding登録まで。実装修正は開始しない。

## Purpose

repository固有schedule policyと汎用task contractの依存方向を整える。

## Contract

- 最終検証taskの具体pathとrepository固有の順序判定はharness側のownerが持つ。
- 汎用PlanScheduleには一般的なparse/closure/dependency責務だけを残す。
- 本repositoryの順序検査とforeign/inactive harnessの境界を維持する。

## Must not

- generic parserへ別のrepository固有task名を追加しない。
- 旧exportのalias・互換wrapper・推定fallbackを残さない。
- 大規模policy frameworkや新規config形式を導入しない。

## Acceptance criteria

- 汎用taskcontractに022固有知識が残らず、production callerが正規ownerを使う。
- 最終検証後のrunnable task拒否、dependency付きtask、無関係なPlanの扱いを検証する。
- repository markerによる適用境界を保持する。

## Historical invariants

- Codex ReductionとQuality Deltaを同時に評価する。
- current schemaだけを正規入力とし、ユーザーdataの保護・current transaction recoveryと後方互換性を混同しない。

## Dependencies

none
