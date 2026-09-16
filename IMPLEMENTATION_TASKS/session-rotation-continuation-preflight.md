# Task: session rotation continuation preflight

## Original instruction

````text
あと最近それ忘れるの多いな
````

## Amendments

none

## Resolved references

- 「それ」は、前parent thread向けのpending session rotation directiveを処理せず、別threadで現在ACTIVEを開始しようとすることを指す
- 今回は旧thread `01a0a504-b518-72b0-a39c-b82dddb3fb20`向けpending directiveが残り、current threadからの`rotation-claim`はdirective not foundで拒否された。ユーザー承認後に旧threadへclaim/bindだけをqueueして復旧した

## Purpose

pending session rotationを新parent thread側が見落としたままACTIVE startへ進む反復を防ぎ、旧thread claim・新thread bind・claim付きstartの必要操作を正規経路で確実に完了させる。

## External feasibility

status: not-applicable

## Contract

- task開始・再開preflightでpending / claimed / bound rotationとcurrent thread identityの組合せをmachine-readableに判定する
- pending directiveを旧threadだけがclaimできる現contractと、既に別threadが作成済みの場合の正規復旧経路を一次証拠から整理する
- 親がpendingを見落として通常startへ進めないよう、canonical handoff / start admissionのどちらがownerかを確定する
- architecture・thread identity・claim権限・external thread creation boundaryを変更する必要がある場合は、実装前に`NEEDS_SOL_DECISION`へ戻す
- user承認が必要なexternal thread messageを暗黙実行せず、必要性と対象操作を一度で提示できる状態にする

## Must not

- `CODEX_THREAD_ID`の上書き等で旧thread identityを偽装しない
- rotation state fileやDBを直接編集しない
- 重複thread作成、blind queue、通常startによるpending directive bypassをfallbackにしない
- session rotationをGLM task lifecycleやrate-limit recoveryと混同しない

## Acceptance criteria

- pending directiveが旧threadにある状態で別threadから通常startできないことをintegration testで固定する
- 旧thread claim、新thread bind、claim付きstartの成功経路を同一checkoutで検証する
- 別threadが既に存在する場合の許可済み復旧と、外部操作未許可時のfail-closedを検証する
- handoff projectionだけで親が次の合法操作と対象threadを判定でき、同じpending directiveを反復して見落とさない
- 既存のrotation claim/bind/start identity検証を弱めない

## Historical invariants

- external thread creationとmessage送信はrepository外のexternal boundaryであり、親が所有する
- session rotationのstate transitionとadmissionはmachine authorityを正とする

## Dependencies

none
