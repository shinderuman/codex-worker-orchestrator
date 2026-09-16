# 親Codex session rotation

親context累積をtask境界で切るためのsession rotation。GLM task/session、compaction、rate-limit recoveryとは別のlifecycleである。

## machine authority

rotation要否、directive identity、claim/bind/start admission、retry整合、ack/retireは`handoff.session_rotation`と`control:session-rotation-claim-bind-start`を唯一のprocedure authorityとする。親Codexはtrigger閾値、task/risk/usage evidence、claim stateを再計算・再構成しない。

- `pending`ならmachineが返したdirectiveに対してrotationを進める。
- `not-required`ならrotationしない。
- `unavailable`は証拠不足を意味し、親が独自閾値でrotationを発行しない。
- healthyなin-flight GLM呼出をrotation目的で中断しない。

## external thread creation boundary

新しいCodex threadの作成だけはrepository外のexternal boundaryであり、親が所有する。作成前後のrepository-side state transitionはmachine commandへ委ねる。

1. handoffがpendingならmachine-admitted claim actionを実行する。
2. external threadは同じsaved project / local checkoutで作成し、別clone/worktreeへ切り替えない。
3. 作成結果をmachineのbind/fail actionへlosslessに返す。結果不明をfailedへ読み替えず、重複thread作成で補わない。
4. 新threadではrepository authorityとcurrent Gitを再読し、旧会話自由文を要求正本として複製しない。machineがbindしたclaim付きstart actionからcurrent task/session stateへ接続する。
5. machineがclaimをacknowledgeしてdirectiveをretireした後に旧parent sessionを終了する。

exact command、directive/claim ID、milestone token、retry条件、ack timingはhandoff/action specとproduction state machineを正とし、本instructionへ複製しない。

## semantic responsibility

親に残る判断は、external thread creationの実施可否・作成結果の事実、new threadへ渡す最小bootstrap context、外部作成失敗時の対応である。rotation trigger自体やrepository-side lifecycleを意味判断として再判定しない。
