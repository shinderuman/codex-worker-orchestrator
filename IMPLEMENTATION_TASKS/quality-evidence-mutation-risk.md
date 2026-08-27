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

- EVAL最終構造は完了commit `e276599`以後の現行`EVAL.md`を指す。
- 2026-08-28 current Git resolution: 後続のfalse-complete修復で`EVAL.md`は削除済み。deterministic evaluation authorityはproduction tests / scenario corpusへ収束し、live parent/model behaviorだけが`tests/parent-behavior-evals.json`へ分離されている。F4 Track Bはこの現行registryを対象とし、`EVAL.md`を復活させない。

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

none

## Review findings

none

## Current boundary

ACTIVE / VALIDATION。F3はPR #13 Squash Merge commit `627e0dcfa15148a68684c3ff9008a4658c5a2615`としてintegrationへ反映済み。F4 Track Aはquality evidence変更時だけbaseline差分を検査し、Go testはAST由来evidence signatureでassertion削除・negative case削除・expected behavior変更をHIGHへ上げる一方、test追加・identifier rename・same-evidence file renameをLOWに保つ。非Go text/fixtureは内容文字列ではなくsignificant evidence unit数を比較し、同数の機械的更新をLOW、unit削除をHIGHとする。JSON fixtureはparse後の構造unit数で同様に扱う。Track Bは`tests/parent-behavior-evals.json`をcase IDとcontract fieldsで比較し、status-only更新をLOW、case削除/contract変更をHIGHとする。quality evidence変更がない通常reviewでは追加baseline Git probeを行わない。validationはproduction wiring run `33103239842`でworkflow tests / vet / all-package build PASS、cost boundary run `33103429475`でworkflow tests PASS、non-Go narrowing run `33104521707`でworkflow tests / vet PASS。次はこのparent checkpoint後の恒久Repository Lintとfinal reviewを行う。
