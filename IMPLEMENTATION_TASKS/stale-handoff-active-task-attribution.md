# Task: stale handoff active task attribution

## Original instruction

````text
じゃあその訂正を検出したことがバグだろ、それは起票しないとだめだろ
````

## Amendments

### 2026-09-17 first priority correction

````text
NEXTでいいわけないだろ優先順位を勘違いするなよ
````

### 2026-09-17 final priority correction and emergency stop

````text
勘違いしまくってるので緊急停止した
このタスクはActiveの次の次でいい
お前なんで全体の優先順位を確認しないの？
````

最新指示により、このtaskはcurrent ACTIVEの直後ではなく、その次に実行する。Planでは既存NEXT先頭の直後へ置き、それ以前の優先解釈をoverrideする。

## Resolved references

- 「その訂正」は、親Codexが`glm-worker --handoff`の`task_status=complete`を現在のACTIVE taskの状態と誤認し、「ACTIVE taskの実装・review・親acceptは完了済み」と説明した後、`glm-worker --project-state`の`continuation.state=unknown reason=active-task-mismatch`とGit現物から、handoffが直前taskのlifecycleを示しており現在のACTIVE taskは未実装だと訂正した事象を指す
- 誤認時のhandoffはtop-levelで`consistent=true`、`task_status=complete`、`required_action=none`を返す一方、PlanのACTIVEはhandoffが示す完了taskとは別taskへ遷移済みだった
- 親Codexはhandoffのtask identityとauthorityが示す現在のACTIVE taskの一致を確認せず、過去taskのterminal stateを現在taskへ帰属させた
- 親Codexはtask起票時と最初の優先訂正時にPlan全体のACTIVE / NEXT順、既存taskの優先理由、ユーザーが指定した停止境界を同時に比較せず、局所的なblocker評価だけでNEXT先頭化とACTIVE化を行った

## Purpose

過去taskのcanonical handoffが残る状態でPlanのACTIVEが別taskへ遷移していても、親Codexが過去taskの完了状態を現在taskへ帰属させず、開始・再開・完了状況を正しく判定できるようにする。

## External feasibility

status: not-applicable

## Contract

- canonical handoffまたはその正規利用手順で、handoffが所有するtaskとPlanが示す現在のACTIVE taskの対応を機械判定可能にする
- 両者が一致しない場合、過去taskの`task_status`、terminal、parent actionを現在のACTIVE taskの状態として解釈できる成功projectionにしない
- rotation、completion、ACTIVE遷移の途中状態でも、過去taskの未処理actionと現在ACTIVEの未開始状態を混同せず、それぞれのownerと合法な次操作を示す
- 親Codexの通常手順が、自由文の推測ではなくcanonical machine projectionからactive-task mismatchを最初の判定で検出するようにする
- current ACTIVEの開始可否を判定するために、別commandの後追い実行やGit履歴からの推測を必須にしない
- task priority変更時はPlan全体のACTIVE / NEXT順、既存taskの優先根拠、ユーザー指定の停止境界を比較してから配置を確定する

## Must not

- 過去taskのhandoffを暗黙に現在ACTIVEへ付け替えない
- `consistent=true`または`task_status=complete`だけでcurrent ACTIVE完了と解釈可能な曖昧な状態を残さない
- active-task mismatchを無視して通常start、resume、completeを許可しない
- session rotationのowner bindingや既存fail-closedを迂回しない
- 親promptの注意書きだけを唯一の対策にしない
- 局所的な緊急度だけでPlan全体を確認せずACTIVEまたはNEXT先頭へ割り込ませない

## Acceptance criteria

- 完了済みtaskのhandoffが残ったままPlanのACTIVEが別taskへ遷移した再現scenarioで、過去taskの完了をcurrent ACTIVE完了として報告・処理できないことを固定する
- 同scenarioで、過去task identity、current ACTIVE identity、不一致理由、合法な次操作がcanonical projectionから判定できる
- task identity一致時のstart / resume / decision / review / completeの既存handoff contractを維持する
- session rotation pendingを伴うactive-task mismatchでも、owner外claimによる試行錯誤を要求せず正規復旧経路を示す
- integration testが、親Codexによるcurrent ACTIVE誤帰属を防ぐproduction prompt / dispatch / machine projectionの因果を固定する
- priority変更の経路がPlan全体と明示停止境界を入力に含め、局所判断だけによる誤った割込みを防ぐことを固定する

## Historical invariants

- PlanのACTIVE task fileが現在実行対象の要求正本である
- 過去taskのterminal stateは、task identityが一致しない現在ACTIVEの進捗証拠ではない
- lifecycle mismatchは推測で成功へ縮退せずfail closedする
- task scheduleとpriorityはPlan全体の順序を正とする

## Dependencies

none
