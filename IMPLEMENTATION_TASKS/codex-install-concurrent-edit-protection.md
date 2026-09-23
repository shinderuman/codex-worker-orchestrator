# Task: Codex installerの事前確認後の編集保護

## Original instruction

````text
GLMは使わずこのリポジトリの総合的なレビューしてほしい
特にCodex Reductionに貢献できそうな施策とか考えてほしい
後方互換性を持たせないとかそういうルールはちゃんと確認して厳守しろ
````

親Codexがレビューで採用した独立finding:

````text
Codex installerはprepareInstallでconfig.toml全体を書換え用に保存し、applyConfigInstallPlanで現物を再検証せず置換する。事前確認後にユーザーが管理外の設定値を編集すると、成功扱いのままその編集を消す。競合する変更を黙って上書きしない更新境界にする。
````

## Amendments

none

## Resolved references

- `glm-worker/internal/codexinstall/install.go`のprepare/apply境界と`config.go`の全体置換が対象。prepare後に管理外keyを書き換え、apply成功後に古い値へ戻る経路で再現した。
- 今回はレビューとfinding登録まで。実装修正は開始しない。

## Purpose

installerとユーザー編集・別installerの競合による設定消失を防ぐ。

## Contract

- preparation時の入力とmutation時の現物の一致を根拠付きで扱い、不一致は安全に拒否するかユーザー編集を保存する。
- 同じ配置先へのinstaller間の並行実行を整合させる。installer lockだけで任意の外部editorも排他できるとは扱わない。
- file/config/stateの書込み・削除・rollbackを含め、競合時の保護範囲と残るraceの限界を明示する。

## Must not

- 管理外keyや事前確認後の編集を黙って消さない。ownership検証を無効化しない。
- old schemaのmigration・alias・推定fallback、新規daemonや汎用transaction frameworkを追加しない。

## Acceptance criteria

- prepare後・apply前に管理外keyが変わる再現で、変更が保存されるかmutation前に明示的に拒否される。
- 管理対象file、config、削除候補、ownership stateの競合とrollback時の追加編集を対象別に検証する。
- 同じ配置先への並行installerで不整合なfile/state組合せを成功扱いしない。

## Historical invariants

- Codex ReductionとQuality Deltaを同時に評価する。
- current schemaだけを正規入力とし、ユーザーdataの保護・current transaction recoveryと後方互換性を混同しない。

## Dependencies

none
