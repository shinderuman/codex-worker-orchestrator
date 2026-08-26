# Task: 未分類production surfaceのrisk fail-openを廃止する

## Original instruction

````text
F2: self-protection / risk classificationがunknown pathをfail-openする

- 現在はknown critical pathだけHIGH。
- 新しいproduction directory / helper / script / config / file typeが分類表へ未登録ならLOWになり得る。
- 新しいsurfaceを追加する瞬間ほど分類漏れが起こりやすい。

REQUIRED HARDENING

- self-protection classificationをfail-openの列挙方式のまま放置しない。
- known-safe LOWとunknown/new production surfaceを区別する。
- 未分類production-like pathは少なくともSol確認へ上げるfail-closed側を検討する。
- test/docs等まで無条件HIGHにしてノイズ化せず、分類責務を明確にする。
- 新規directory/file type追加時に分類更新漏れがsilent LOWにならないtestを追加する。
````

## Amendments

- 2026-08-26 Product boundary: repo固有`selfprotection.go`へpathを追加するだけではTrack A完了としない。generic化できない場合もTrack Bとして有効なら捨てずに実装する。
- 2026-08-26 Clarification: F2をTrack A/Track B/両方/既存包含/不要へ一次証拠で分類し、実装容易性を理由に間引かない。

## Resolved references

- unknown/new production surfaceは既存分類へ未登録のproduction-like path・directory・file typeを指す。

## External feasibility

status: not-applicable

## Purpose

新しいproduction surfaceが分類漏れでsilent LOWになるfailure classを、任意repo向けrisk boundaryと本repo self-protectionの両面で閉じる。

## Contract

- known-safe LOWとunknown production-like surfaceを機械的に区別し、Track A/Bの適用範囲を明示する。
- test/docsを一律HIGH化せず、未分類追加時のsilent LOWをdeterministic gateで防ぐ。

## Must not

- 全変更HIGH、path列挙追加だけ、Sol callの無制限増加で解決しない。

## Acceptance criteria

- 新規production directory/file typeの分類更新漏れがreview前に失敗または必要Sol gateへ上がる。
- F2のA/B分類とcost/noise影響を記録する。

## Historical invariants

- risk floorとself-protection既存契約を弱めない。

## Dependencies

- `IMPLEMENTATION_TASKS/eval-responsibility-reduction.md`

## Review findings

none

## Current boundary

NEXT。EVAL責務整理完了後にF2をTrack A/Bへ分類して着手する。
