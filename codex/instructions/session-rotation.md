# 親Codex session rotation

親context累積をtask境界で切るためのsession rotation。GLM task/session、compaction、rate-limit recoveryとは別のlifecycleである。

## machine authority

rotation要否、directive identity、claim/bind/start admission、retry整合、ack/retireは`handoff.session_rotation`と`control:session-rotation-claim-bind-start`をprocedure authorityとする。親Codexはtrigger閾値、task/risk/usage evidence、claim stateを再計算・再構成しない。

- `pending`ならmachineが返したdirectiveに対してrotationを進める。
- `not-required`ならrotationしない。
- `unavailable`は証拠不足を意味し、親が独自閾値でrotationを発行しない。
- `session_rotation.incomplete_rotations`が空でなければrotationは未完了であり、その間の通常startはmachine admissionで拒否される。次の合法操作と対象threadは、各entryの持ち主thread・state・directive/claim IDから確定する。
- healthyなin-flight GLM呼出をrotation目的で中断しない。

## external thread creation boundary

新しいCodex threadの作成はrepository外のexternal boundaryであり、親が所有する。この外部結果をrepository-side stateへ結び直すために必要な最小relayだけを親が行う。

1. `pending` projectionの`directive.directive_id`だけを使い、`glm-parent-action rotation-claim <directive-id>`を実行する。返された`claim_id`をそのまま保持する。
2. external threadを同じsaved project / local checkoutで1件作成する。別clone/worktreeへ切り替えない。
3. 作成成功時だけ旧threadで`glm-parent-action rotation-bind <directive-id> <claim-id> <new-thread-id>`を実行する。作成失敗が確定した場合だけ`glm-parent-action rotation-fail <directive-id> <claim-id> --creation-result-json <json>`へ事実を渡す。結果不明をfailureへ読み替えず、重複thread作成で補わない。
4. 新threadではrepository authorityとcurrent Gitを再読し、旧会話自由文を要求正本として複製しない。通常startは`glm-parent-action start --rotation-claim <claim-id>`を使う。milestone startがsemanticに必要なら既存staging surfaceを使い、rotation claim以外のtoken/JSON手順を本文から再構成しない。
5. machineがclaimをacknowledgeしてdirectiveをretireした後に旧parent sessionを終了する。

作成順を崩して新threadを先に作ってしまった場合も、同じ正規経路だけで復旧する。新thread側の`rotation-claim`は持ち主でないため拒否され、未完了directiveがある間は通常startもadmissionで拒否される。旧threadで`rotation-claim`と、作成済みthreadを対象とする`rotation-bind`をuser承認付きで実施し、新threadでは`start --rotation-claim`を使う。旧threadへの指示送信はuser承認が必要な外部操作であり、必要性と対象操作は`session_rotation.incomplete_rotations`の持ち主thread・state・IDから一度に提示する。重複thread作成や通常startでのbypassへfallbackしない。

上記のdirective/claim/thread ID以外のtrigger条件、admission条件、retry可否、ack timing、state遷移はproduction state machineを正とする。

## semantic responsibility

親に残る判断は、external thread creationの実施可否・作成結果の事実、new threadへ渡す最小bootstrap context、外部作成失敗時の対応である。rotation trigger自体やrepository-side lifecycleを意味判断として再判定しない。
