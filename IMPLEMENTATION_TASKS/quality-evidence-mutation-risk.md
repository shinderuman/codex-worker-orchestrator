# Task: quality evidence縮退をrisk boundaryへ反映する

## Original instruction

````text
F4: EVAL / test / test harness変更がrisk floor上LOWになり得る

- `EVAL.md`、`*_test.go`、`testdata/`、`tests/`等は現在non-critical扱い。
- GLMが実装へ合わせてnegative case/assertionを弱める、quality gate自体を削る変更でもLOWへ落ち得る。
- 特にEVAL責務整理後、この境界が安全であることを再確認する必要がある。

REQUIRED HARDENING

- EVAL整理後の最終構造を前提に、EVAL/test/scenario/test harness等のquality evidence surfaceをどうrisk分類するか再設計する。
- production sourceでないことだけを理由にLOWへしない。
- assertion削除、negative case削除、coverage縮退、expected behavior緩和等をrisk判断へ反映できる境界を検討する。
- 全test変更を無条件HIGHにするだけの雑な解決は禁止。
````

## Amendments

- 2026-08-26 Product boundary: EVALをHIGH扱いするだけではTrack A完了としない。generic化できないrepo固有quality surface保全はTrack Bとして別計上する。
- 2026-08-26 Clarification: Track A/B両方を評価し、片方だけの修正で他方のfailure classを完了扱いにしない。

## Resolved references

- EVAL最終構造は`IMPLEMENTATION_TASKS/eval-responsibility-reduction.md`完了後の状態を指す。

## External feasibility

status: not-applicable

## Purpose

negative case・assertion・coverage・expected behaviorの縮退を単なるnon-production変更としてLOWへ落とすfailure classを閉じる。

## Contract

- quality evidenceの追加・強化と削除・緩和を区別できる最小risk signalをTrack A/Bで設計する。
- 全test変更一律HIGHを避け、EVAL整理後のsurfaceへ適用する。

## Must not

- pathだけの一律HIGH、substring pin増殖、quality gate削減で解決しない。

## Acceptance criteria

- assertion/negative case/coverage/expected緩和の代表caseがsilent LOWにならない。
- 通常のtest追加・機械的更新に不要なSol負荷を増やさない。
- F4のA/B分類とcostを記録する。

## Historical invariants

- EVAL coverageと既存quality gateを維持する。

## Dependencies

- `IMPLEMENTATION_TASKS/eval-responsibility-reduction.md`

## Review findings

none

## Current boundary

NEXT。EVAL責務整理完了後のsurfaceを正としてF4へ着手する。
