# 親Codex session rotation

親context累積をtask境界で切るためのsession rotation。GLM task/session、compaction、rate-limit recoveryとは別のlifecycleである。

## machine authority

rotation要否、directive identity、claim/bind/start admission、retry整合、ack/retireは`handoff.session_rotation`と`control:session-rotation-claim-bind-start`をprocedure authorityとする。親Codexはtrigger閾値、task/risk/usage evidence、claim stateを再計算・再構成しない。

- `pending`ならmachineが返したdirectiveに対してrotationを進める。
- `not-required`ならrotationしない。
- `unavailable`は証拠不足を意味し、親が独自閾値でrotationを発行しない。
- healthyなin-flight GLM呼出をrotation目的で中断しない。

## external thread creation boundary

新しいCodex threadの作成はrepository外のexternal boundaryであり、親が所有する。この外部結果をrepository-side stateへ結び直すために必要な最小relayだけを親が行う。

1. `pending` projectionの`directive.directive_id`だけを使い、`glm-parent-action rotation-claim <directive-id>`を実行する。返された`claim_id`をそのまま保持する。
2. external threadを同じsaved project / local checkoutで1件作成する。別clone/worktreeへ切り替えない。
3. 作成成功時だけ旧threadで`glm-parent-action rotation-bind <directive-id> <claim-id> <new-thread-id>`を実行する。作成失敗が確定した場合だけ`glm-parent-action rotation-fail <directive-id> <claim-id> --creation-result-json <json>`へ事実を渡す。結果不明をfailureへ読み替えず、重複thread作成で補わない。
4. 新threadではrepository authorityとcurrent Gitを再読し、旧会話自由文を要求正本として複製しない。通常startは`glm-parent-action start --rotation-claim <claim-id>`を使う。milestone startがsemanticに必要なら既存staging surfaceを使い、rotation claim以外のtoken/JSON手順を本文から再構成しない。
5. machineがclaimをacknowledgeしてdirectiveをretireした後に旧parent sessionを終了する。

上記のdirective/claim/thread ID以外のtrigger条件、admission条件、retry可否、ack timing、state遷移はproduction state machineを正とする。

## semantic responsibility

親に残る判断は、external thread creationの実施可否・作成結果の事実、new threadへ渡す最小bootstrap context、外部作成失敗時の対応である。rotation trigger自体やrepository-side lifecycleを意味判断として再判定しない。
