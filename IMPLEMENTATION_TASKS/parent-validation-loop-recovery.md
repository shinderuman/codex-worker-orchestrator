# Task: parent validation loop recovery

## Original instruction

````text
なんで許可を求めるの
いつも自動でループエンジニアリングしているじゃないか
それができなくなってるのならバグだろ
````

## Amendments

none

## Resolved references

- 「それ」は、worker auto-fix budget後もparent validationが失敗した際、同一ACTIVEを自動修正loopへ戻せず追加のユーザー許可を求めたことを指す
- task `daa10a4c-adb1-49a4-ab6f-a648e32c2b9e`では、parent validation失敗後にrepository lockがfree、taskが`active/stale`、`allowed_actions=[]`、`action_specs={}`となり、`glm-parent-action wait`も`allowed_actions`欠落で失敗した
- working treeを保持した明示`glm-worker --reset`と同一ACTIVEのfresh dispatchで作業は復旧したが、これは通常の自動loopではない

## Purpose

parent validationとworker auto-fixが収束しない場合も、同一ACTIVEの修正loopをmachine-authoritativeなhandoffから継続可能にし、ユーザーへの不要な許可確認やtask state resetを防ぐ。

## External feasibility

status: not-applicable

## Contract

- parent validation failureとauto-fix budget exhaustion後のtask lifecycleをterminalまたは明示的なrecoverable stateへ遷移させる
- canonical handoffは次の合法操作を`required_action`、`allowed_actions`、`action_specs`で欠落なく返す
- 親は同一ACTIVEの修正を追加のユーザー許可なしに継続できる
- 保存済みworker session、checkpoint、validation evidence、task identityを安全に継続できる場合は保持する
- fresh dispatchが必要な場合も、通常のmachine actionとしてworking tree・task要求・原因証拠を保持して遷移する
- architecture、state transition、fix budgetまたはretry上限の意味変更が必要なら実装前に`NEEDS_SOL_DECISION`へ戻す

## Must not

- `stop`をuser interruption以外のfallbackとして使わない
- `glm-worker --reset`を通常のvalidation修正loopにしない
- state file、checkpoint、DBを直接編集しない
- validation failureを成功扱いにせず、無制限retryを導入しない
- rate-limit、provider-unavailable、session rotationのlifecycleと混同しない

## Acceptance criteria

- parent validation failureがauto-fix budget後も残るscenarioで`active/stale`かつ`allowed_actions=[]`にならない
- canonical handoffだけから同一ACTIVEの次の合法なfix/recovery操作を実行できる
- 親が追加のユーザー許可を求めず、修正、再validation、独立reviewへ継続できる
- owner lease喪失、command error、parent validation failureを区別し、それぞれのcheckpointとtask identityを破壊しない
- successful validation、通常のworker/reviewer loop、rate-limit/provider recovery、明示stop/resumeの既存挙動を弱めない

## Historical invariants

- semantic fix内容は親が判断し、transportとlifecycle admissionはmachine authorityが所有する
- validation failureは局所終端であり、現在ACTIVEの完了またはユーザー許可待ちへ自動変換しない

## Dependencies

none
