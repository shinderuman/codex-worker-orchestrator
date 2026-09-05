# Task: Parent plan continuation enforcement

## Original instruction

````text
そもそも自由言語でハーネスにしようとしているのが間違いなんじゃねえのか
他に今までやった、これからやるものが自由言語で防ごうとしているものがあるんじゃねえのか
お前にはルールを守る能力がないんだから自由言語で防ぐことは不可能
````

## Amendments

none

## Resolved references

- `task-lifecycle.md`は局所終端を親USER_REQUEST完了と扱わず後続へ継続するよう自由言語で要求する
- `--project-state`は`next_runnable`を機械投影するが、局所task完了後に親がそれを取得・実行せずfinal responseで停止することは拒否できない
- 本sessionではユーザーが複数回、許可済み作業を止めずPlanを継続するよう再指示している

## Purpose

ACTIVE task、automation、install等の局所終端後に、明示継続対象のPlanが残っているのに親Codexが完了終了することをmachine stateで防ぐ。

## External feasibility

status: not-applicable

## Contract

- parent handoff/finalization resultへproject continuation obligationとexact next runnable/blocked reasonを含める
- 明示停止境界、新しい権限、外部状態、semantic user decisionがない限り、次actionをclosed state transitionとして提示する
- terminal Goalまたはユーザー指定停止以外でparent USER_REQUEST completionを成立させないmachine postconditionを設ける
- automationによる将来再開はverified scheduleを継続証拠として扱い、単なるfinal responseと区別する

## Must not

- PlanにNEXTがあるだけで別の未許可roadmapへscope拡張しない
- user decisionが必要なBLOCKED taskを自動開始しない
- proseで「継続する」と書くだけの対策にしない
- liveness reportや定期pollを増やさない

## Acceptance criteria

- 局所PASS、install完了、rate-limit予約、NEXTあり、BLOCKEDのみ、明示停止の各scenarioで合法な終端が機械判定される
- NEXTあり・停止境界なしのfalse completionを拒否する
- verified automation待ちは安全な非実行期間として識別される
- parent turn/token増加なしにhandoffへ必要fieldが出る
- 独立reviewer、Sol semantic review、current snapshot validationを完了する

## Historical invariants

- userが明示した継続範囲を局所終端で打ち切らない
- 未許可scopeへ自動拡張しない

## Dependencies

- `IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`

## Review findings

none

## Current boundary

auditで既存handoff/project-state/finalize-checkとの重複を整理してから実装する。
