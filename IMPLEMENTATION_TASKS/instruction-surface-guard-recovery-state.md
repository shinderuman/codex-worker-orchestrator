# Task: instruction-surface guard後のtask recovery整合

## Original instruction

````text
Task 019で、workerが`codex/AGENTS.md`を変更したcallをinstruction-surface guardが復元・拒否した後、task stateが`active`かつ`pending-decision`となった。`--decision`・`--fix`・新task・`--resume`の全経路が拒否し、working treeを保持した`--reset`でしか継続できなかった。同一task recovery契約とCLI state transitionを整合させる。
````

## Amendments

none

## Purpose

instruction-surface guard failure後も、保持可能なresult・checkpoint・working treeから同一taskを安全に再開できる状態を保証する。

## External feasibility

status: not-applicable

## Contract

- recoverableなafter-call instruction mutationではtask status、pending decision、resume checkpoint、completed worker resultを単一の再開可能状態へ遷移させる
- `--status`の`task_status`・`pending_decision`・`resume_available`と、`--decision`・`--fix`・`--resume`・新task開始の受理集合を矛盾させない
- guardが復元したinstruction identity、task baseline、working tree保持、review独立性を維持する

## Must not

- instruction-surface mutationの受理、guard緩和、汚染session再利用、暗黙reset、working tree破棄で解決しない
- Git authority・network transport・parent-managed metadata guardへscopeを拡張しない

## Acceptance criteria

- worker/reviewer各phaseのafter-call instruction mutationで、result保存可否を含む全terminal pathのstate transition regression test
- status表示と各recovery CLIの受理集合をprocess-level testで固定
- same-task recovery後に必要な独立reviewへ進み、resetなしでterminal outcomeを閉じる
- test/race/vet/build/gofmt、独立reviewer、必要なSol品質gate、commit

## Historical invariants

- instruction-surface guardはmutationを呼出前snapshotへ復元し、task-wide instruction identityをfail closedで保護する
- guard recoveryはunsafe working-tree drift、baseline divergence、実authority mutationでfail closedを維持する

## Dependencies

none

## Review findings

none

## Current boundary

Task 019で再現・保存済み。未着手。
