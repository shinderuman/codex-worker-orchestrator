# Task: 無変化のin-flight時に親Codexを再入させない

## Original instruction

2026-10-25 ユーザー指示:「最終的なFindingはCodexに実装させようと思う」「FindingをIMPLEMENTATION_PLANに分割する」。

## Resolved references

親の短いwait/poll/無変化進捗に対し繰返し長時間blocking指示が必要だった（#1359 G13、#1360 H13、#1363 K08、#1369 M08、#1372 N07、#1377 R01、#1383 U06）。変更後に改善したsessionもある。

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
