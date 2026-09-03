# Task: task corpusとPlan scheduleのclosureを強制する

## Original instruction

````text
気になったのがIMPLEMENTATION_PLANSにファイルがあるのにIMPLEMENTATION_PLANに記載されてないやつが多数ある気がする
実装作業開始前に何が起こってるのか確認してくれ
````

## Amendments

none

## Resolved references

- ユーザーの`IMPLEMENTATION_PLANS`は現repositoryの`IMPLEMENTATION_TASKS/`を指すものとして現物照合した
- 2026-09-03時点で、tracked task file 14件のうち4件がPlanのACTIVE/NEXT/BLOCKEDに存在しなかった: `codex-desktop-prompt-overhead-reduction.md`、`codex-instruction-conflict-reduction.md`、`glm-containment-denial-explanations.md`、`parent-finalization-choreography-reduction.md`
- 4件はproduction実装済みだがTask本文がfresh dogfoodによる実機Acceptance未完了を明記しており、完了fileの削除漏れではない
- commit `171c0ff6be81d8609d29adfb684af2860c5e23ba`が4件を「Web GPTの測定・Acceptance tracker」としてPlanから意図的に除外した。後に外部trackerをcurrent implementation authorityとしないlifecycleへ移行しても再同期されなかった
- `CheckFinalHeadPlan`、`active-task-contract`、`--project-state`はPlan列挙taskの存在・contract・dependencyを検査するが、Task directoryからPlanへの逆方向closureを検査しない
- 親CodexのGo/No-Go照合では、4件ともproduction implementationに加えて後続bundle/telemetryとfollow-up commitで残存Acceptanceが成立していると判断し、再実装せず完了同期した

## Purpose

未完了task fileがschedule stateを失って022 final verificationから脱落する状態をfail closedで防ぐ。

## External feasibility

status: not-applicable

## Contract

- Plan管理repositoryではcurrent `IMPLEMENTATION_TASKS/*.md` regular fileの全件がPlanのACTIVE/NEXT/BLOCKEDのいずれかへexactly once列挙されるclosureを機械検証する
- working treeのharnesslint/通常admissionとfinal HEAD postconditionの双方で同じ受理集合を使う
- `--project-state`でもunscheduled、duplicate、missing/non-regular taskをmachine-readable failureとして扱い、runnable/completeへ縮退しない
- schedule parser、task path validation、parent-managed metadata集合の既存ownerを再利用し、別index/file/stateを追加しない
- completed taskは既存どおりtask file削除とPlan entry削除を同じmetadata同期で行う。外部Issue/Web GPT trackerをschedule authorityにしない
- migrationでは判明した4件の状態を先に照合し、COMPLETEとしてfile/Planを同期削除した後の全current taskでclosureを成立させる

## Must not

- 未schedule taskを自動削除、完了扱い、BLOCKED扱いしない
- filename、commit時刻、Issue statusからschedule sectionやpriorityを推定しない
- `IMPLEMENTATION_TASKS/*.md`というPlan内説明用globをtask entryとして数えない
- dependencyから到達できるだけのtaskをschedule済みとみなさない
- Git履歴上の削除済みtaskをcurrent corpusへ復元しない

## Acceptance criteria

- current filesystemとfinal HEADの双方で、unscheduled task、duplicate schedule entry、missing task、non-regular taskをfail closedに検出する
- ACTIVE/NEXT/BLOCKEDへexactly once存在する正常corpusと、task完了時のfile/entry同時削除がPASSする
- `--project-state`はunscheduled taskを無視した正常projectionを返さない
- 既存ACTIVE contract、Goal completed empty schedule、dependency validation、Plan final-head branch/transition検査を維持する
- 現repositoryのtracked/untrackedを含むcurrent task corpusとPlanがclosure成立状態になる
- 独立reviewer、Sol semantic review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- schedule stateはPlanだけを正とし、task fileへStatusを持たせない
- outstanding task fileはPlan scheduleに属し、ordinary completion時はfileとentryを同期して削除する
- parent-managed implementation metadataを外部trackerと二重管理しない

## Dependencies

none

## Review findings

none

## Current boundary

ACTIVE。実装開始前監査とPlan migrationは親Codexが完了し、production closure guardは未着手。
