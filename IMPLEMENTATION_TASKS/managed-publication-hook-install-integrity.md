# Task: managed publication hook install integrity

## Original instruction

````text
今回の監査作業にこのExternal Reviewの範囲も入れてくれ
````

## Amendments

none

## Resolved references

- current managed-hook refreshはactive snapshotへrequired hookを1件ずつ置換し得るため、途中failureでold/new hookが混在したactive snapshotを残せる
- detached HEADからの初回installer runは、installer-owned managed snapshotが未確立でも条件によってpublication guardを有効化しないままsuccessへ到達でき、pending/legacy recoveryも省略し得る
- live publication candidate installでは、ownership stateなし + `core.hooksPath=.githooks` の既存repositoryに対してinstallerがlegacy `.githooks`をclaimせず、publication postconditionがinstaller-owned managed snapshotを要求したため、同じcanonical retryだけでは成功不能になった
- live recoveryで検討されたlegacy adoptionは、`core.hooksPath`が正確に`.githooks`、required tracked hooksがcurrent `HEAD:.githooks/<hook>`とbyte-identical、non-empty / executable、conflicting ownership / external pathなし、というbounded条件でのみ安全に成立する
- 同recoveryのreviewでは、managed snapshot切替途中の`git config core.hooksPath` failureでpending stateがretryを阻害し得ることと、legacy `.githooks` adoption時にbaselineを`absent`として記録するとretireで元の`core.hooksPath=.githooks`を復元できないことがcorrectness defectとして確認された

## Purpose

publication guard用managed hooksのinstall / refresh / legacy adoption / retireを単一のinstaller ownership contractへ収束させ、partial activation、guard不在success、retry不能なpending state、preexisting ownership破壊を防ぐ。

## External feasibility

status: not-applicable

## Contract

- required managed hooksは、全hookの取得・mode設定・validationをsibling staging snapshotで完了した後だけactive snapshotへ切り替える
- active snapshot切替前のfailureでは従来active snapshotを完全に維持し、old/new hookが混在する状態を公開しない
- installer-owned snapshotが未確立のdetached HEAD / tag / commit-pin installでrequired publication guardを安全に有効化できない場合、successへ縮退せずfail closedまたはmachine-visible blocked/recoveryへ落とす
- pending activation/recovery stateが存在する場合、detached installでも正規recoveryを省略しない
- ownership stateなし + `core.hooksPath=.githooks` のlegacy repositoryは、tracked required hooksとのexact identity、mode、non-empty、ownership conflict absenceをmachineで証明できる場合だけinstaller-owned managed snapshotへadoptできる
- legacy adoption時はpreexisting `core.hooksPath`とhook ownership baselineをlosslessに保存し、retireで元状態へ復元できるようにする
- adoption / activation途中の`git config`やfilesystem transition failureは、次のcanonical retryで安全に収束できるstateを残す。failure後にownership conflictとして自己阻害しない
- existing external/custom hooks pathや内容不一致の`.githooks`をinstaller ownershipへ奪わない
- publication guard installのpostconditionは、実際に有効なinstaller-owned managed snapshotと`core.hooksPath`整合をmachine evidenceで検証する

## Must not

- active managed hook directoryへ1件ずつ直接上書きしてpartial refreshを公開しない
- detached HEADだからという理由だけでguard setupを黙ってskipしsuccess扱いにしない
- ownership stateがない既存`.githooks`を無条件にlegacy扱いしてclaimしない
- preexisting `core.hooksPath` baselineを`absent`へ潰さない
- recoveryのためにuser-owned external hooksを削除・上書きしない
- warningだけでpublication guard不在をsuccessへ縮退させない

## Acceptance criteria

- 2個以上のmanaged hooksをrefreshするfixtureで途中failureを注入しても、従来active snapshotがbyte-for-byte維持される
- successful refreshでは全required hookがvalidated staging snapshotから一度にactiveになり、mode / executable / content postconditionが成立する
- installer-owned snapshot未確立のdetached HEAD / tag / commit-pin fixtureで、guard不在のsuccessが発生しない
- pending activation/recovery stateを持つdetached installが正規recoveryを省略しない
- exact legacy `.githooks` fixtureはbounded条件をすべて満たす場合だけadoptでき、adoption後のretryとretireで元の`core.hooksPath=.githooks` baselineを復元できる
- `git config core.hooksPath` failure等をactivation途中へ注入しても、次retryがownership conflictで永久blockされず安全に収束できる
- content/mode/path/ownership conflictのあるlegacy fixtureはfail closedし、外部ownerを変更しない
- related installer / hook ownership tests、install smoke、Repository LintがPASSする

## Historical invariants

- user-owned external hook configurationをinstaller都合でclaimしない
- publication guardは存在を記録するだけでなくGitから実効的に呼ばれる状態をmachineで検証する
- install / retire / retryで同じownership authorityを使い、第二のhook ownership stateを作らない

## Dependencies

none
