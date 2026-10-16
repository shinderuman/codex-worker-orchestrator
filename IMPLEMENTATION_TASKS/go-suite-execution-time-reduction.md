# Task: Go suiteの実行時間短縮

## Original instruction

ユーザー原文:

````text
テストの実行に20分かかるならそれは作業効率に直結するから現Taskじゃなくてもだいぶ早めに直すTaskだと思う
うまく並列実行とかにするべきなんじゃないか
詳細を設計するのはタスク時でいいが
````

## Amendments

## Resolved references

- 「テスト」はrepositoryの固定quality-gateで実行するfull Go suiteを指す。現ACTIVEへ最適化を混ぜず、早い優先順位の独立Taskとして扱う。ユーザーの20分は仮定であり、観測値と区別する。
- 調査起点はquality-gate run `24788cbe21d6560fdbc02ac738d65b68` のrun.jsonとgate.log。既存Runtimeの`quality-gate-runs/`に保全されている。長時間実行の途中で残っていたpackageは`internal/controller`。詳細設計と原因の採否は本Taskの着手時に行う。

## Purpose

品質coverageと正規の証拠帰属を維持したまま、Go suiteのwall-clock時間を短縮し、実装・Review・修正の反復コストを下げる。

## External feasibility

status: not-applicable

## Contract

- 保存済み実行証拠とcurrent suiteを使い、package・test・fixture・機械処理ごとの支配的な実行時間と反復を特定する。
- 現在のGo package並列実行を確認した上で、test単位の並列化、独立性を保った分割、重複処理の削減等を評価する。並列化を原因確認前に唯一の解と決めない。
- Git、Runtime、環境変数、socket、fixture、共有資源の隔離と決定性を維持する。意味のあるarchitecture／quality contract変更は既存のSol判断規則を通す。
- 固定quality-gateのsnapshot binding、run identity、実終了状態、ログ、全suiteの成功判定を維持する。
- 実装前後を比較可能な条件で測定し、coverageを落とさず実行時間が短縮されたことを示す。必要な独立Review、Validation、Lint、buildを行う。

## Must not

- テストの削除・skip、検証範囲の縮小、timeoutの単純延長、必須gateのoptional化を時間短縮の代用にしない。
- staleなvalidationやcacheをcurrent snapshotの成功証拠へ転用しない。
- 環境変数や共有Stateのrace、flakyな並列化、第二State正本、旧schema互換fallbackを追加しない。
- 現ACTIVEの実装中に本Taskを開始しない。GLMへGit remote writeやparent-managed metadata編集を許可しない。

## Acceptance criteria

- 支配的な実行時間と原因を一次証拠で説明できる。
- 採用した改善について、同等coverageのbefore／after測定でwall-clock短縮を確認できる。
- 全suiteの検証義務と正常・失敗時の正規quality-gate証拠を維持する。
- 並列化する場合は、影響する共有資源の隔離と主要な失敗境界を検証する。
- 独立Review、必要なValidation、最終harnesslint違反0、適切なbuildを確認する。

## Historical invariants

- Codex Reductionを最上位目的とし、削減のためにQuality Deltaを悪化させない。
- deterministicな機械処理は既存ownerへ寄せ、追加model再入や重複検証で効率化を代替しない。

## Dependencies

none
