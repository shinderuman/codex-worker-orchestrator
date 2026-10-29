# Task: 無変化のin-flight時に親Codexを再入させない

## Original instruction

2026-10-25 ユーザー指示:「最終的なFindingはCodexに実装させようと思う」「FindingをIMPLEMENTATION_PLANに分割する」。

## Resolved references

親の短いwait/poll/無変化進捗に対し繰返し長時間blocking指示が必要だった（#1359 G13、#1360 H13、#1363 K08、#1369 M08、#1372 N07、#1377 R01、#1383 U06）。変更後に改善したsessionもある。

### 2026-10-29 Dogfood #1432 — ユーザーSTOP入力が長時間waitに遮断された実例

- Runtime Task `0b1b3ae0-f403-41f7-9f60-d9f5e847d07b` の生親Codex transcriptはBundle `bound/transcripts/parent/01a18667-b18f-7303-9e3f-01c2f560dee9/0`（attempt側にも同源snapshotあり）。2026-10-29T06:58:57ZにユーザーはCodex直接実装へ切替を指示し、親は06:59:08ZにGLM停止を宣言したが、約21,600,000msの`wait`を要求した。07:31:46Zのユーザー再指示まで`glm-worker --stop`を呼ばず、07:31:55Zに初めて呼んで`interrupted`で成功した。
- **機械経路を分離:** `--stop`自体は正常動作。問題はuser input/STOP後の親待機・割込み・再入であり、停止宣言から実操作まで約32分47秒のユーザー介入が発生したこと。同じin-flight workerの保持・待機を安全に行うContractを維持したうえで、ユーザー入力を無変化待機より優先して処理する境界を実親ログで検証する。
- この事象を`healthy wait`の成功、単なるquota待ち、またはCLI停止機能の不具合と混同しない。既存の「真正のSTOP・ユーザー介入が届く」Acceptanceに包含し、Taskを重複生成しない。観測済みの実行は本Taskがまだ未実装の段階であり、修正後実効性の判定ではない。

## Amendments

none

## Purpose

既存in-flight Worker/Go/remote Processを保持したまま、状態遷移・結果・エラー・ユーザー入力が生じたときのみ親のmodel decisionを再開する。

## External feasibility

status: not-applicable

## Contract

既存in-flight Worker/Go/remote Processを保持したまま、状態遷移・結果・エラー・ユーザー入力が生じたときのみ親のmodel decisionを再開する。OSレベル監視と親model turnを分離し、親の実トークン使用量と待機を観測する。

## Must not

livenessだけでWorkerを再起動しない。第二daemonやscheduler、無意味なpoll APIを作らない。semanticな重大結果を遅延・省略しない。

## Acceptance criteria

実Parentログ上、同一状態の長時間待機中にparent modelの無変化再入が発生しない。真正のエラー・終了・STOP・ユーザー介入は届く。前後のモデルturn/usage/Latencyの比較は観測範囲と条件を明示する。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
