# Task: 同じ失敗根拠での長時間Go Gate再実行を防ぐ

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「IMPLEMENTATION_PLANは既存含め優先度を考え直す」。

## Resolved references

同じsource・古いinstalled CLIでGo FAILを同一blocking facade内に二回投入（#1346 F06、追加約642秒）。同じsource/timeoutの約20分Go FAILを二回（#1356 F02）、後続類例（#1369 M03）。現行開放中のCI高速化PR #1385、Go版整合/速度PR #1389とは実装ownerを重複させない。

## Amendments

none

## Purpose

Gate admissionはsource OID/tree、installed tool SHA、argv/timeout、環境・Snapshot、既存失敗原因と再投入の新情報を照合する。

## External feasibility

status: not-applicable

## Contract

Gate admissionはsource OID/tree、installed tool SHA、argv/timeout、環境・Snapshot、既存失敗原因と再投入の新情報を照合する。wrapper内自動反復と親の再投入を同じ意味境界で判断し、必要なら先に修正・再配置のnext actionを示す。

## Must not

変更した候補に必要なfresh Go/race、source-bound evidence、独立Reviewを省略しない。失敗の一時waiverを恒久skipとして扱わない。

## Acceptance criteria

不変の失敗source/tool/timeoutなら無意味な二度目を実行せず、変更済みsource/tool/timeoutや不安定原因の有意な診断なら合法に再実行する。実行削減と誤skip防止、品質維持を実測する。既存PRで同ownerが修復済みなら重複変更しない。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
