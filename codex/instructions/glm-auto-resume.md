# GLM rate limit自動再開

`glm-worker`が`ZAI_GLM_CODING_PLAN_5H`のrate limitで停止し、同じtaskをreset後も継続すると親がsemanticに判断した場合だけ適用する。

## machine transaction

予約のidentity、task/repository binding、coalesce、時刻、schedule、create/update、retry、cleanup、保存実体verifyは`glm-worker --auto-resume-plan`から始まるmachine transactionを唯一のprocedure authorityとする。親CodexはこれらをMarkdown・会話memory・tool responseから再構成しない。

- 最初に`glm-worker --auto-resume-plan`を1回実行する。wake後にも必要でrepository authorityへ永続化されていないuserのrun-controlだけ、存在する場合に限り`--run-control <原文>`でlosslessに渡す。
- `status=coalesced`、`verified`、`failed`はterminalであり、そのmachine resultをそのまま採用する。
- `status=write_required`は次節のexternal relayだけを行い、lookup/create/update/verifyの途中でSolへ戻らない。
- command/identity/stateがfail closedした場合は値を推測・補完せず停止する。

## external automation relay

Codex appの`automation_update` mutationだけはrepository外の`external-unenforceable`境界である。親の役割はmachine specのlossless relayに限定する。

- `write_required`ではmachine outputの`write`を変更せず`automation_update`へ渡す。ID、name、thread、status、schedule、promptその他のfieldを親で生成・修正しない。
- toolのraw responseは解釈・要約せず、同じtransactionが指定するresponse commandへそのまま返す。次の`write_required`が返った場合だけ同様にrelayする。
- 現在threadに`automation_update`が存在しない場合はmachine outputのexact `fallback_command`を1回実行する。fallback command自身が保存済みtask/repository/thread/reset bindingとexternal automation persistenceを判定する。正しいACTIVE one-shotが既に存在する場合はそれを唯一のwake ownerとしてlocal resumeを起動しない。正しいPAUSED placeholderまたは未作成の場合だけproviderのexact `reset_at`までmachine-owned blocking waitし、wake後にtask stateとexternal wake ownershipを再検証してcanonical `glm-parent-action resume`まで実行する。ambiguous/mismatched persistenceはfail closedする。親はsleep時間、task identity、resume argvを生成しない。
- fallback中はpoll/model-call loop、他threadへの質問/queue、rollout/session探索、plugin/MCP/remote-control直叩き、手作業schedule作成を行わない。
- `cleanup`が返った場合だけ、そのexact specをbest-effortでrelayする。名前・時刻・一覧探索からcleanup対象を作らない。
- external toolの安全判定を迂回しない。

## wake時

wake promptはworkflow authorityではない。現在checkoutのrepository authorityを再読し、promptにmachine生成されているrepository/task identityとlosslessなrun-controlだけをwake対象のbindingとして使う。

wake後のstate確認とresumeは同じtool orchestration内で行い、途中でSolへ戻らない。現在taskがpromptのexpected taskと一致し、machine admissionがresumeを許す場合だけ`glm-parent-action resume`で保存済みcheckpointを継続する。不一致・reset済み・別status・parse/admission failureではresumeしない。実GLM resume automationを作成済みなら、変更はmachineが返すexact cleanup/write specに限定する。

resume後に再び同じ5h rate limitへ到達した場合は、新しい停止済みstateから`--auto-resume-plan`へ戻る。それ以外のterminal resultはcanonical handoffとともに通常のpacket処理へ戻す。新しい`glm-worker "<元依頼>"`を起動しない。

## 不変条件

- current parent thread identityはauto-resume command processの`CODEX_THREAD_ID`を正とし、親はthread IDを探索・手入力しない。
- machine transactionが所有するautomation ID、coalesce条件、schedule grammar、UTC変換、placeholder strategy、prompt形、retry上限、verification grammarをCodex instructionへ複製しない。
- deterministic transactionの中間結果をSolへ返して次stageを選ばせない。
- working tree、task state、worker/reviewer session、resume checkpointを破棄・resetしない。
- 元依頼やSol判断を再構成せず、保存済みstateからだけ再開する。
- userが自動再開前に明示的にreset/停止境界を変更した場合は、その最新authorityを優先し古いautomationから継続しない。
