# Task: Z.ai provider limit classification and self-resume

## Original instruction

````text
5時間系はリトライせず、5時間以外はリトライで、英語メッセージは見ない
ってしたほうが普通なんじゃないか？

7dは仕様として待機じゃなくて終了のほうがいいな
それを待機するとちょっとやりすぎな感じがある

思ったんだが--auto-resume-fallback で5h limitから回復したらそのままCodexに返さずGLMに処理を継続させるならそもそも5h limit時にCodexに返さなくて継続させればより一層Token節約できるんじゃね？
Codexのスケジュールが使えないときのFallbackだったのでスケジュールとの二者択一になるけどなんかもうスケジュール機能捨てていい気がしてきた

そもそもがスケジュールの際にCodexが「なにをすればいいんだっけ」ってToken溶かしまくっていたしオミットしていいとおもった
なのでさっきのGLMのステータスハンドリングの修正と合わせてやるんでいいかと思っている
かつ優先度は致命的なバグ修正のすぐ次でいいとおもう
````

## Amendments

### 2026-09-19

````text
これ--auto-resume-fallback というオプション自体はオミットされるイメージでよいか？
選択肢なくCodexからGLMに依頼した作業は5hLimitの限りでは永久に走り続けるイメージ
ただしCodexの停止ボタンからCodexを止めることはでき、その後改めて「作業を停止しろ」と言えばGLMが停止、「再開」といえば作業が継続される
設定ファイルかなにかで永久レジュームをオプトイン、オプトアウトできてもいいがそれは必要が発生しないと実装しない、つまり実装しない
実装しないのでIssueとかにも書かなくてよい
Codexに「判断」させると「中断できたほうがなにかに対応できるから中断できるモードで起動しよう」とか余計なことするから「判断」はさせない
````

## Resolved references

- 「GLMのステータスハンドリングの修正」は、Z.ai公式business codeをclassification authorityにし、固定英語message全文一致をrate-limit種別判定に使わない方針を指す
- conversation時点の公式reference確認では、少なくともrequest rate limit / temporary overload、5h系usage limit、7d以上のlong quota、契約・model・残高等のaction-required failureが別codeとして存在した。実装開始時に公式referenceを再確認し、staleなcode一覧をこのtaskだけから固定しない
- `1302` request rate limitや`1305` temporary overloadのような短期transientはbounded backoff/retry対象、確実に5hと判定できるquotaはimmediate retryせずreset待機、7d系long quotaは自動待機せず停止、契約・model非対応・残高等のretryで解消しないfailureはstop/action-requiredとする方向で合意した
- 「スケジュール機能」はGLM provider 5h limit用のCodex `automation_update` / `glm-auto-resume` wake経路を指し、Codex自身のrate-limit recovery schedulerまで無関係に削除する要求ではない
- current `--auto-resume-fallback` はreset時刻までmachine-owned blocking waitし、task identity / rate-limited state / checkpoint / reset boundaryを再検証して`glm-parent-action resume`へ進む。これは新しいcanonical self-resume経路で保持すべき安全性の既存referenceであり、CLI option / fallback二重経路そのものは廃止対象とする
- 「5h Limitの限りでは永久に走り続ける」は、同一GLM taskが5h quotaへ繰り返し到達しても、その都度Codex判断を挟まずmachine-owned wait/resumeを反復し、ユーザー停止・7d/long quota・action-required/fatal・state invariant failure等の明示terminal条件まで継続する意味とする。単一processがOS停止を超えて必ず生存する意味ではなく、durable checkpointから同一task継続可能であることを含む
- 永久self-resumeのopt-in / opt-out設定は今回実装しない。必要性が実観測されるまで設定項目・Issue・将来Taskを追加しない
- formal Dogfood `cc4e60d1-8e64-4f86-a66a-8a2acd070163` は約36.7hの間にrate limit 7回 / resume command 7回を実際に踏み、5h boundaryがrare edgeではなく長時間taskの主要mechanical continuation costになることを再確認した

## Purpose

Z.ai provider failureをbusiness code中心に正しく分類し、短期transientはretry、5h quotaはCodexへ返さずglm-worker自身が安全に待機・再開を反復、7d以上やaction-required failureは自動長期待機せず停止する単一のprovider recovery責務へ整理して、Codexのscheduler再解釈turnとtoken消費を除去する。

## External feasibility

status: observation
assumption: Z.aiのcurrent producer responseが公式business codeで短期transient・5h quota・7d以上のlong quota・action-required failureを安定して区別でき、5h reset boundaryをmachineが安全に取得できること

## Contract

- provider failure classificationはZ.ai business codeを主authorityにし、固定英語messageの全文一致を種別判定条件にしない
- 実装時の公式Z.ai error-code referenceと実producer evidenceを確認し、各business codeを少なくとも transient retry / 5h self-resume / long-quota stop / action-required stop / unknown-safe-stop にdispositionする
- request rate limit・temporary overload等の短期transientは既存provider recoveryのbounded backoff / probe / retryへ接続し、quota reset待機と混同しない
- 確実に5hとmachine判定できるusage limitはimmediate retryせずdurable resume checkpointを保存し、Codexへcontrolを返さずglm-workerがreset boundaryまで待機する
- 5h wake後はtask identity、task status、resume checkpoint、reset boundaryその他current resume invariantを再検証し、成立時だけ同じGLM taskをcanonical resume経路で継続する
- 同一taskが再度5h limitへ到達した場合も同じmachine-owned wait/resumeを反復し、5h limit自体をparent/Codexへ返すterminal conditionにしない
- `--auto-resume-fallback` CLI optionと「external schedulerかlocal fallbackかをCodexが選ぶ」protocolは廃止し、5h self-resumeを単一canonical behaviorにする
- Codexへ5h recovery modeの選択・中断可否の判断・scheduler利用判断をさせない。Codexが「念のため中断可能なモード」等を選べる分岐を残さない
- userがCodex UIの停止操作でparent実行を止めることは妨げず、その停止やprocess終了でdurable task/checkpoint stateを破壊しない
- userが停止後に明示的に「作業を停止」と指示した場合は既存の正規停止経路でGLM taskを停止でき、「再開」と指示した場合はdurable stateから同一taskを正規resumeできる
- 5h待機中にprocessが終了してもdurable checkpointから明示resumeでき、作業を消失させない
- 7d系または同等のlong quotaはglm-worker processやexternal schedulerで長期待機せずtaskを停止し、後日の明示resumeへ委ねる
- plan/model/credit/contract等、時間backoffだけでは解消しないprovider failureをtransient retryへ誤分類しない
- GLM provider 5h recoveryのCodex `automation_update` scheduler経路をcanonical flowから除去し、scheduler relay・wake transaction・local fallbackの二重ownershipを解消する
- current `--auto-resume-fallback` が持つstate revalidation、reset binding、stale/wrong-task防止等のうち新しい単一経路でも必要な安全性は保持する。fallback-specific protocolやexternal wake ownershipは不要なら撤去する
- 永久self-resumeのopt-in / opt-out config、CLI switch、repository policy toggleを今回追加しない。必要性がproducer/Dogfood evidenceで発生するまで実装対象にも予約対象にも含めない
- 5h rate-limit recovery中にCodex / Solをmechanical wake orchestrationのためだけに再起動・再解釈させない

## Must not

- provider classificationを英語messageの語句変更に依存させない
- 5h quotaを通常transientとして短周期retryし続けない
- 7d以上のquotaを数日間のblocking waitやautomationで自動保持しない
- unknown business codeを推測で5h auto-resumeへ昇格しない
- scheduler撤去のためにdurable checkpointやwrong-task / stale-reset防止を弱めない
- GLM provider recoveryの変更をCodex自身のrate-limit recoveryへ無関係に拡張しない
- Codexへ5h self-resumeを使うかどうかのsemantic choiceを渡さない
- 将来必要かもしれないという理由だけでself-resume opt-in / opt-out設定を追加しない

## Acceptance criteria

- official current Z.ai referenceとproducer evidenceに基づくbusiness-code dispositionがあり、固定英語message変更だけではclassificationが変わらないtestがある
- short transient codeはbounded retryへ入り、5h quota・long quota・action-required codeは同じretry loopへ入らない
- 5h quota fixture/integration pathでCodex handoffや`automation_update`や`--auto-resume-fallback`を要求せず、checkpoint保存からmachine wait・wake再検証・GLM resumeまで完結する
- 同一taskが複数回5h quotaへ到達するfixtureで、各reset後にCodex判断なしで継続し、5h到達回数そのものではterminalizeしない
- reset前はGLMを再実行せず、reset後もtask/checkpoint/reset invariant不一致ならfail closedする
- 5h待機processを途中終了しても明示resume可能なdurable stateが残る
- user/parentによる明示停止とその後の明示resumeが、5h self-resumeの単一canonical behaviorと競合せず成立する
- 7d系long quotaはauto-wait / auto-scheduleせずterminal stopとして観測できる
- GLM 5h scheduler/fallback二重経路の不要なproduction wiring・instruction・state・CLIが撤去または単一ownerへ縮約され、Codex側に「何をすればいいか」「どのモードにするか」を再判断させるwake turnが残らない
- self-resume opt-in / opt-out設定が新設されていない
- related Go tests、repository lint、install/smokeの必要範囲がPASSする

## Historical invariants

- 最上位目的はSol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減すること
- provider recoveryはmachine-verifiableなstateとproducer evidenceをauthorityにし、parent proseやmessage wordingをstate machineの代替にしない
- resumeはsame task / same checkpoint / valid reset boundaryを証明できる場合だけ行い、unknownをsuccessへ縮退させない
- 5h recoveryはmachine-owned mechanical continuationであり、Codexのsemantic judgmentを要求しない

## Dependencies

none
