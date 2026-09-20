# 親Codex session rotation

親context累積をtask境界で切るためのsession rotation。GLM task/session、compaction、rate-limit recoveryとは別のlifecycleである。

## machine authority

rotation要否の評価・directive projectionはmachineが一貫したevidence/thresholdから生成するが、未claimの`pending`はcontext-resetのrecommendationでありgeneric task correctnessを構成しない。親Codexはtrigger閾値、task/risk/usage evidenceを再計算・再構成せず、machine projectionを見てrotationを採用するか通常startでdeclineするかを決める。

`rotation-claim`成功後のdirective identity、claim/bind/start admission、target-task identity、wrong-thread/duplicate start防止、retry整合、ack/retireは`handoff.session_rotation`と`control:session-rotation-claim-bind-start`をprocedure authorityとする。このtransactionは独立correctness invariantなのでparent判断でbypassしない。

- `pending`は未claimのrecommendation。rotationを採用するならmachineが返したdirectiveをclaimする。通常startを選んだ場合はmachineがその未claimrecommendationをretireして通常taskへ進む。
- `not-required`ならrotation recommendationは存在しない。
- `unavailable`は証拠不足を意味し、親が独自閾値でrotationを発行しない。
- `session_rotation.incomplete_rotations`の`pending`だけでは通常startをhard rejectしない。`claimed` / `bound`、またはclaim付きretry対象になったrotation transactionは完了までmachine admissionでfail closedする。
- healthyなin-flight GLM呼出をrotation目的で中断しない。

## external thread creation boundary

新しいCodex threadの作成はrepository外のexternal boundaryであり、親が所有する。rotationを採用する場合だけ、この外部結果をrepository-side transactionへ結び直すために必要な最小relayを親が行う。

1. `pending` projectionの`directive.directive_id`だけを使い、`glm-parent-action rotation-claim <directive-id>`を実行する。返された`claim_id`をそのまま保持する。claimしないまま通常startを選ぶこともでき、その場合pending recommendationはretireされる。
2. claimした場合だけexternal threadを同じsaved project / local checkoutで1件作成する。別clone/worktreeへ切り替えない。
3. 作成成功時だけ旧threadで`glm-parent-action rotation-bind <directive-id> <claim-id> <new-thread-id>`を実行する。作成失敗が確定した場合だけ`glm-parent-action rotation-fail <directive-id> <claim-id> --creation-result-json <json>`へ事実を渡す。結果不明をfailureへ読み替えず、重複thread作成で補わない。
4. 新threadではrepository authorityとcurrent Gitを再読し、旧会話自由文を要求正本として複製しない。通常startは`glm-parent-action start --rotation-claim <claim-id>`を使う。milestone startがsemanticに必要なら既存staging surfaceを使い、rotation claim以外のtoken/JSON手順を本文から再構成しない。
5. machineがclaimをacknowledgeしてdirectiveをretireした後に旧parent sessionを終了する。

作成順を崩して新threadを先に作ってしまった場合、まだ`pending`でrotationを採用するなら旧threadで`rotation-claim`し、作成済みthreadを対象に`rotation-bind`して新threadで`start --rotation-claim`を使う。すでに`claimed` / `bound`へ入った後は同じ正規transactionだけで復旧し、通常startでbypassしない。旧threadへの指示送信はuser承認が必要な外部操作であり、必要性と対象操作は`session_rotation.incomplete_rotations`の持ち主thread・state・IDから一度に提示する。重複thread作成へfallbackしない。

上記のdirective/claim/thread ID以外のtrigger evidence、transaction admission条件、retry可否、ack timing、state遷移はproduction state machineを正とする。

## semantic responsibility

親に残る判断は、未claimのrotation recommendationを採用するか通常startでdeclineするか、external thread creationの実施可否・作成結果の事実、new threadへ渡す最小bootstrap context、外部作成失敗時の対応である。trigger閾値やclaim後のrepository-side correctness lifecycleは意味判断として再判定しない。
