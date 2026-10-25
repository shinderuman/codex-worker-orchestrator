# Task: Publication前の未達阻止とCandidate/Remote source identityの継続

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「そして最終的なFindingはCodexに実装させようと思う」。

## Resolved references

初期の未達Complete・Remote Pushと撤回（#1356 F01 / #1376 P01）、promote後の修正不能停止（#1376 P02）、Metadata/index不一致（#1359 G08/#1360 H08/#1363 K07）、本Auditで自己promote済みcommitを外部進行と誤認した停止（#1392 V05）。今回のTaskで修正された経路もあるため、必要条件の再現と現行Owner照合が先。

## Amendments

none

## Purpose

Controllerのaccepted candidate、terminal Task/Plan、primaryの既知base/index、remote upstream、真の外部変更を同一source authorityで扱う。

## External feasibility

status: not-applicable

## Contract

Controllerのaccepted candidate、terminal Task/Plan、primaryの既知base/index、remote upstream、真の外部変更を同一source authorityで扱う。Acceptance/実Bundle観測を修正可能段階で検査する。promote後のown commitと外部Pushを区別し、失敗時は正規reentryでsource-bound Review/Validationへ戻す。

## Must not

Remote Push後の未達を通常復旧にしない。Complete後の再開APIは追加しない。外部変更をown candidateとして認めず、Guard/独立Reviewを弱めない。

## Acceptance criteria

正規入口E2Eでpreaccept未達、正当なbase、own promote、真正external advance、post-promote evidence mismatchを別々に再現する。未達時Remoteは不変、修正後はfresh検証・install・Bundle固定・単一公開・Retireが成立する。既存修正で有効なら重複実装しない。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
