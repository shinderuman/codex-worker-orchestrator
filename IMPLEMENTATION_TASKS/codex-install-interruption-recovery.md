# Task: Codex installerのprocess中断復旧

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexがレビューで採用した独立finding:

````text
Codex installerのrollback退避はメモリだけにあり、file/configを更新した後でownership stateを書き込む。state保存直前にprocessが終了すると、通常のInstall再実行が自身の更新をユーザー変更と判断して拒否する。current install transactionの中断を証拠付きで復旧できるようにする。
````

## Amendments

none

## Resolved references

- `glm-worker/internal/codexinstall/transaction.go`の`applyInstallWithStateWriter`が対象。state writer境界で子processを終了し、通常Installの再実行拒否を再現した。`internal/settingsmerge/transaction.go`のdurable recoveryは既存例だが、別ownerを無条件に統合しない。
- 今回はレビューとfinding登録まで。実装修正は開始しない。

## Purpose

更新中断による手動修復と不必要な親Codex介入を減らす。

## Contract

- current transactionについて、process終了後にも正当なpre/post imageを識別できる復旧境界を持たせる。
- 中断後のユーザー編集、不正な復旧記録、別配置先の記録を推定で上書きしない。
- 通常error時のrollbackとprocess中断時の復旧の責務を明確にする。

## Must not

- ownership stateを削除して強制再配置する方法を正常復旧としない。
- 旧stateのmigration、旧schema reader、互換fallbackを追加しない。
- repo全体のinstaller frameworkや新規daemonへ拡張しない。

## Acceptance criteria

- file/config置換後・ownership state保存前の子process終了から通常入口で安全に復旧できる。
- 各durable mutation境界の中断を検証し、部分更新を成功扱いしない。
- 中断後のユーザー編集と破損・不正schemaの記録は安全停止し、編集を失わない。

## Historical invariants

- Codex ReductionとQuality Deltaを同時に評価する。
- current schemaだけを正規入力とし、ユーザーdataの保護・current transaction recoveryと後方互換性を混同しない。

## Dependencies

none
