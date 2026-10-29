# Task: ACTIVE AmendmentとWorker source revisionを同期してから委譲

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「最終的なFindingはCodexに実装させようと思う」。

## Resolved references

親がACTIVE要求を追記してもin-flight Workerへ反映されず旧契約で実装した例（#1359 G01）、primary/lane変更確認なしにWorker resumeした例（#1363 K01）。#1347 J03のRule Activation/revision correctionも関係。

## Amendments

2026-10-29 親Codex追加要求: 初回モデル呼出前にprimaryのTask改訂を検出して拒否した場合も、作成済みlane/leaseと未開始runtimeをcanonical handoffが扱えること。sync-task-authority成功後、task.id/runtime binding未生成を破損した実行中runtimeと区別し、追加モデル呼出や別復旧操作の副作用に依存せず正規の次操作を提示する。起動→改訂拒否→sync→handoffまでを同一実経路で検証し、Controllerだけ同期してhandoffがconsistent:falseのまま残る状態を解消する。根拠は2026-10-29中間評価の会話・tool結果と`review-evidence/codex-efficiency-checkpoint-2026-10-29.md`。

## Purpose

tracked Original/Amendments/Resolved referencesとWorkerへ渡すcontract digest、source snapshot、実lane/primaryを同一履歴で検証し、意味変更時は実行前に正規sync/Reviewへ戻す。

## External feasibility

status: not-applicable

## Contract

tracked Original/Amendments/Resolved referencesとWorkerへ渡すcontract digest、source snapshot、実lane/primaryを同一履歴で検証し、意味変更時は実行前に正規sync/Reviewへ戻す。

## Must not

Task文書を後から偽装修正しない。Codex直接実装特例を恒久のWorker入口へ追加しない。異なるTask/sourceの証拠を採用しない。

## Acceptance criteria

Amendment追加中の旧Worker resumeを拒否し、source同期後は同Task継続・独立Review・必要Gateが成立する。primary/laneの真正同一sourceと異なるrevisionを正しく区別する。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
