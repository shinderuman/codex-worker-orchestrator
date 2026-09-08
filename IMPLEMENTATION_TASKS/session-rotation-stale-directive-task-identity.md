# Task: Session rotation stale directive task identity

## Original instruction

````text
さっきの作業依頼は別に最優先にしてほしいとは言ってない
お前の方で優先順位を考えろ
このセッションはだいぶ長くなってしまったのでActive作業完遂時に必ずセッションをRotateするようにしてください
````

## Amendments

none

## Resolved references

- 親task `01a07e12-7651-76a0-92d4-c244e4963f62`には、task `38bc5f3b-e938-4540-a798-7af2e506f085`完了時に作られたpending directive `84e3f02e-cf23-4cc8-aaee-f3fb42aad71e`が残っていた
- その後task `7fefc2cd-48a0-4887-a679-50978a0ae237`を完了し、同じpending directiveをclaimした。claim `9115ac5c-d4e1-48b2-8f48-e79a553d396f`、target task `cffc13b4-f630-43e1-a2d8-aa3d8103b34d`、bound thread `01a08268-2c7d-72a0-9aa1-605eed066ce9`
- `rotation-claim`と`rotation-bind`は成功したが、新threadの`start --rotation-claim`はcurrent task IDが古いdirective taskでもtarget taskでもないため、`session rotation開始中のtask identityが一致しません`でfail closedした
- markerのlast evaluationはcurrent completed task `7fefc2cd-48a0-4887-a679-50978a0ae237`、required true、reason `default-two-tasks`を保持している

## Purpose

pending rotation directiveの発行後に追加taskが完了してからrotationする正常経路で、古いdirective task IDとcurrent completed task IDの差によりrotationが回復不能になることを防ぐ。

## External feasibility

status: not-applicable

## Contract

- claim時またはstart admissionで、rotation元として認めるcurrent completed task identityをauthoritative stateから一意に拘束する
- directive発行taskより後のtaskであっても、同じparent threadの最新terminal evaluationがcurrent taskと一致し、rotation requiredであり、taskがcompleteなら、同じclaim・bound thread・target taskでstart可能にする
- unrelated task、active/incomplete task、別parent thread、別bound thread、古いevaluation、target不一致はfail closedする
- current bound claim `9115ac5c-d4e1-48b2-8f48-e79a553d396f`を破棄・再作成せず、修正版install後に同じclaimで正規retryできるようにする
- 専用daemon・DB・汎用migration frameworkを追加せず、既存rotation markerとadmissionを最小修正する

## Must not

- identity guardを単純に削除・常時許可しない
- current task IDだけを信頼し、parent thread・claim・bound thread・target task・terminal evaluation照合を省略しない
- 新しいCodex taskやclaimを重複作成しない
- archived task `01a07e6f-89d0-7510-b075-6c1179915032`を参照・再開しない
- GLM worker/reviewerへGit remote write authorityを与えない

## Acceptance criteria

- pending directive発行後に別taskを1件以上完了し、そのlatest terminal evaluationからclaim/bind/startするscenarioが成功する
- current taskとlatest evaluation不一致、task未完了、別bound thread、別claim、別targetのscenarioはfail closedする
- current bound claimの現物条件と同型のfixtureで、修正版start retryがtarget task IDを開始しclaimをacknowledgeできる
- existing immediate-rotation、retry、claim/bind/issued lifecycle testが維持される
- independent reviewer、必要なSol判断、validation、install、同じclaimでのlive retryを完了する

## Historical invariants

- session rotationは同じsaved project・同じlocal checkoutで行う
- 新threadの最初のparent action成功前は旧threadがownerである
- GLM in-flight taskをrotationのために中断しない

## Dependencies

none

## Review findings

- `ClaimSessionRotation`はclaim元current task identityを保持せず、`admitClaimedSessionRotation` / `StartSessionRotationTask`は古い`Directive.TaskID`またはtarget taskだけを許可するため、pending directiveを後続task境界まで保持すると正規復旧不能になる

## Current boundary

blocking incidentとして最優先ACTIVE。既存bound claimと新threadを維持したまま、修正版install後に同じclaimでstartを再試行する。
