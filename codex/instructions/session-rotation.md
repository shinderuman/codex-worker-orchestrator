# 親Codex session rotation

親context累積をtask境界で切るためのsession rotation。GLM task/session、compaction、rate-limit recoveryとは別のlifecycleであり、repository task correctnessそのものではなく親session運用のためのpolicy signalである。

## machine authority

`handoff.session_rotation`はrotation policyの判定結果とevidenceをcanonical projectionとして返す。親がrotationを選んだ後のdirective identity、claim/bind/start admission、retry整合、ack/retireは`control:session-rotation-claim-bind-start`をprocedure authorityとする。親Codexはclaim transactionのtask/thread identityやretry条件を再計算・再構成しない。

- `pending`はmachine policy上rotationが推奨されていることを示す。親は外部workflowのsession境界と照合してrotationを実施するか判断する。
- `not-required`はmachine policyからrotation recommendationが無いことを示す。
- `unavailable`はrotation projectionに必要な証拠が不足していることを示す。repository task correctnessのfailureへ読み替えない。
- `session_rotation.incomplete_rotations`は開始済みrotation transactionのrecovery evidenceである。rotationを継続する場合の合法操作と対象threadは、各entryの持ち主thread・state・directive/claim IDから確定するが、claimを伴わない通常new-task admissionを一律に支配しない。
- healthyなin-flight GLM呼出をrotation目的で中断しない。

通常new-taskはrotation claimを指定しない限り通常task lifecycleだけでadmissionされる。pending/claimed/bound/issued markerの存在だけを理由に通常startを拒否しない。`--rotation-claim`を使った時点では明示的にrotation transactionへ参加したものとして、wrong-thread、duplicate start、target-task mismatch、retry整合をmachineがfail closedする。

## external thread creation boundary

新しいCodex threadの作成はrepository外のexternal boundaryであり、親が所有する。親がrotationを実施すると判断した場合だけ、外部結果をrepository-side stateへ結び直すために必要な最小relayを行う。

1. `pending` projectionの`directive.directive_id`だけを使い、`glm-parent-action rotation-claim <directive-id>`を実行する。返された`claim_id`をそのまま保持する。
2. external threadを同じsaved project / local checkoutで1件作成する。別clone/worktreeへ切り替えない。
3. 作成成功時だけ旧threadで`glm-parent-action rotation-bind <directive-id> <claim-id> <new-thread-id>`を実行する。作成失敗が確定した場合だけ`glm-parent-action rotation-fail <directive-id> <claim-id> --creation-result-json <json>`へ事実を渡す。結果不明をfailureへ読み替えず、重複thread作成で補わない。
4. 新threadではrepository authorityとcurrent Gitを再読し、旧会話自由文を要求正本として複製しない。rotation transactionを継続するstartは`glm-parent-action start --rotation-claim <claim-id>`を使う。milestone startがsemanticに必要なら既存staging surfaceを使い、rotation claim以外のtoken/JSON手順を本文から再構成しない。
5. machineがclaimをacknowledgeしてdirectiveをretireした後に旧parent sessionを終了する。

作成順を崩して新threadを先に作ってしまった場合も、rotationを継続するなら同じ正規経路だけで復旧する。新thread側が持ち主でないclaimを使用すればmachineは拒否する。旧threadで`rotation-claim`と、作成済みthreadを対象とする`rotation-bind`をuser承認付きで実施し、新threadでは`start --rotation-claim`を使う。旧threadへの指示送信はuser承認が必要な外部操作であり、必要性と対象操作は`session_rotation.incomplete_rotations`の持ち主thread・state・IDから一度に提示する。重複thread作成やclaim identityの推測へfallbackしない。

明示的なrotation transaction内のdirective/claim/thread ID、admission条件、retry可否、ack timing、state遷移はproduction state machineを正とする。

## semantic responsibility

親に残る判断は、machine policy signalを外部workflowのsession境界へ適用してrotationを実施するか、external thread creationの実施可否・作成結果の事実、new threadへ渡す最小bootstrap context、外部作成失敗時の対応である。repository machineはこのworkflow choiceをgeneric new-task correctnessとして強制しない。
