# Task: 通常FacadeのGuardian審査適格性と実配備policyの一致

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「最終的なFindingはCodexに実装させようと思う」。

## Resolved references

閉鎖済み#320以降も普通のglm-parent-action facadeが審査された複数の現物（#1341 C07、#1346 F07、#1347 J06、#1356 F05）。一方、複合shell/直接書込/本install/remote Pushは権限審査の必要性が異なる。

## Amendments

none

## Purpose

installed managed policy・実CLIバージョン・生argvを照合し、裸の正規Facade操作が意図通り扱われるか調べる。

## External feasibility

status: not-applicable

## Contract

installed managed policy・実CLIバージョン・生argvを照合し、裸の正規Facade操作が意図通り扱われるか調べる。不要なReviewだけを機械的に排除し、重要副作用は従来通りGuardに渡す。

## Must not

全Guardian allow/denyを不要として数えない。特権操作のGuardを一括スキップしない。deny後に別操作が成功しただけで同一拒否のbypassと断定しない。

## Acceptance criteria

裸のfacade/複合shell/実install/publicationの認可結果が現在配備とsourceで一致する。必要なら変更前後実rolloutで呼出回数・Latencyと安全境界を比較する。現状が有効ならコード変更なしで処置できる。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
