# GLM watchのorphan terminal待機契約

`glm-worker --watch`をread-only attach recoveryへ使っている途中で、対象taskの実行processが既に存在しない状態を機械判定してwatchが終端した場合だけ適用する。通常のwatch待機・完了待機は`glm-execution.md`側の契約を使う。

## orphan terminalの機械条件と終端形式

- watchは毎tickで対象repositoryのtask status・最新model call material・repository lockを機械判定する。taskが`active`でも、最新の非probe model call materialの`outcome`が`error`で、対象repository lockにlive ownerがいない(flockが取得可能。PID記載だけのstale lock fileはfree)場合は、実行processの不在をorphan terminalとして扱う。
- 誤ったrunning processのstale扱いを防ぐため、最終orphan判定は対象repository lockをnonblockingで取得し、そのleaseをtelemetry・material・parent action planの再読から`watch_exit`書込み完了まで保持する。判定途中にlive ownerがlockを取得すればlease取得は失敗して既存attach待機へ戻る。leaseを取得できない・lock状態が不明な場合もorphan扱いせず待機側へ倒し、platform差分でfalse orphanを出さない。
- watchはlock leaseの間もlock fileの内容変更・削除・task state変更を行わない。lock fileが存在しない場合だけ空fileを作成し、PID等は書かない。
- orphan terminalは失敗ではない。watchは既存JSON Lines stream上の`watch_exit` eventに`status: "orphan-terminal"`を出力してexit 0で終端する。stderr error JSON・non-zero exit・新しいerror kindへは出さない。

## terminal eventの語彙

- `watch_exit`の`orphan-terminal`は、`glm-worker --handoff`のrecovery出力と同じ語彙`consistent`・`inconsistency`・`required_action`・`allowed_actions`・`resume_kind`・`last_material`・`artifact_dir`を同じ意味で持つ。watchとhandoffが同じ状態を矛盾して表現しない。
- 同一stateからparent action planを機械投影できない場合、`watch_exit`は`consistent: false`とboundedなinconsistency reasonを明示する。`required_action`・`resume_kind`のnullと空`allowed_actions`だけを見て正常なnoneと読み替えない。
- `last_material`は最新の非probe model call log(`call_id`・`call_type`・`phase`・`outcome`等)である。terminal error直後のincident stateでは`consistent: true`・`required_action: "none"`・空`allowed_actions`となり、機械次actionが存在しないことは機械値としてそのまま読む。
- `task_id`・`artifact_dir`・`last_material`がrecovery locatorである。詳細診断が必要な場合だけ`--status`を追加する。

## 親の取り扱い

- 親は`orphan-terminal`を受けて、同じstateへ`--watch`を再attachして待機を再開しない。経過時間や自由言語の解釈でorphan・次actionを推測せず、`required_action`・`allowed_actions`と必要なら`glm-worker --handoff`の同名fieldsだけをcanonical next-action authorityとして使う。`consistent: false`のときはhandoffの`consistent: false`と同じ扱いとし、次actionを実行しない。機械actionが存在しない場合はSol/userへの報告に留まる。
- task stateをreset・deleteせず、error後のworker/reviewer sessionとresume checkpointを破棄しない。同じstateへの再開・修正は`glm-execution.md`・`glm-stop-isolate.md`の既存正規経路だけを使う。
