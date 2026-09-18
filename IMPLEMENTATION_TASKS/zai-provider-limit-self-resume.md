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

none

## Resolved references

- 「GLMのステータスハンドリングの修正」は、Z.ai公式business codeをclassification authorityにし、固定英語message全文一致をrate-limit種別判定に使わない方針を指す
- conversation時点の公式reference確認では、少なくともrequest rate limit / temporary overload、5h系usage limit、7d以上のlong quota、契約・model・残高等のaction-required failureが別codeとして存在した。実装開始時に公式referenceを再確認し、staleなcode一覧をこのtaskだけから固定しない
- `1302` request rate limitや`1305` temporary overloadのような短期transientはbounded backoff/retry対象、確実に5hと判定できるquotaはimmediate retryせずreset待機、7d系long quotaは自動待機せず停止、契約・model非対応・残高等のretryで解消しないfailureはstop/action-requiredとする方向で合意した
- 「スケジュール機能」はGLM provider 5h limit用のCodex `automation_update` / `glm-auto-resume` wake経路を指し、Codex自身のrate-limit recovery schedulerまで無関係に削除する要求ではない
- current `--auto-resume-fallback` はreset時刻までmachine-owned blocking waitし、task identity / rate-limited state / checkpoint / reset boundaryを再検証して`glm-parent-action resume`へ進む。これは新しいcanonical self-resume経路で保持すべき安全性の既存referenceであり、CLI名やfallback二重経路の維持自体は要求しない

## Purpose

Z.ai provider failureをbusiness code中心に正しく分類し、短期transientはretry、5h quotaはCodexへ返さずglm-worker自身が安全に待機・再開、7d以上やaction-required failureは自動長期待機せず停止する単一のprovider recovery責務へ整理して、Codexのscheduler再解釈turnとtoken消費を除去する。

## External feasibility

status: observation
assumption: Z.aiのcurrent producer responseが公式business codeで短期transient・5h quota・7d以上のlong quota・action-required failureを安定して区別でき、5h reset boundaryをmachineが安全に取得できること

## Contract

- provider failure classificationはZ.ai business codeを主authorityにし、固定英語messageの全文一致を種別判定条件にしない
- 実装時の公式Z.ai error-code referenceと実producer evidenceを確認し、各business codeを少なくとも transient retry / 5h self-resume / long-quota stop / action-required stop / unknown-safe-stop にdispositionする
- request rate limit・temporary overload等の短期transientは既存provider recoveryのbounded backoff / probe / retryへ接続し、quota reset待機と混同しない
- 確実に5hとmachine判定できるusage limitはimmediate retryせずdurable resume checkpointを保存し、Codexへcontrolを返さずglm-workerがreset boundaryまで待機する
- 5h wake後はtask identity、task status、resume checkpoint、reset boundaryその他current resume invariantを再検証し、成立時だけ同じGLM taskをcanonical resume経路で継続する
- 5h待機中にprocessが終了してもdurable checkpointから明示resumeでき、作業を消失させない
- 7d系または同等のlong quotaはglm-worker processやexternal schedulerで長期待機せずtaskを停止し、後日の明示resumeへ委ねる
- plan/model/credit/contract等、時間backoffだけでは解消しないprovider failureをtransient retryへ誤分類しない
- GLM provider 5h recoveryのCodex `automation_update` scheduler経路をcanonical flowから除去し、scheduler relay・wake transaction・local fallbackの二重ownershipを解消する
- current `--auto-resume-fallback` が持つstate revalidation、reset binding、external wake競合防止等のうち新しい単一経路でも必要な安全性は保持する。不要になったfallback-specific protocolは残存させない
- 5h rate-limit recovery中にCodex / Solをmechanical wake orchestrationのためだけに再起動・再解釈させない

## Must not

- provider classificationを英語messageの語句変更に依存させない
- 5h quotaを通常transientとして短周期retryし続けない
- 7d以上のquotaを数日間のblocking waitやautomationで自動保持しない
- unknown business codeを推測で5h auto-resumeへ昇格しない
- scheduler撤去のためにdurable checkpointやwrong-task / stale-reset防止を弱めない
- GLM provider recoveryの変更をCodex自身のrate-limit recoveryへ無関係に拡張しない

## Acceptance criteria

- official current Z.ai referenceとproducer evidenceに基づくbusiness-code dispositionがあり、固定英語message変更だけではclassificationが変わらないtestがある
- short transient codeはbounded retryへ入り、5h quota・long quota・action-required codeは同じretry loopへ入らない
- 5h quota fixture/integration pathでCodex handoffや`automation_update`を要求せず、checkpoint保存からmachine wait・wake再検証・GLM resumeまで完結する
- reset前はGLMを再実行せず、reset後もtask/checkpoint/reset invariant不一致ならfail closedする
- 5h待機processを途中終了しても明示resume可能なdurable stateが残る
- 7d系long quotaはauto-wait / auto-scheduleせずterminal stopとして観測できる
- GLM 5h scheduler/fallback二重経路の不要なproduction wiring・instruction・stateが撤去または単一ownerへ縮約され、Codex側に「何をすればいいか」を再判断させるwake turnが残らない
- related Go tests、repository lint、install/smokeの必要範囲がPASSする

## Historical invariants

- 最上位目的はSol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減すること
- provider recoveryはmachine-verifiableなstateとproducer evidenceをauthorityにし、parent proseやmessage wordingをstate machineの代替にしない
- resumeはsame task / same checkpoint / valid reset boundaryを証明できる場合だけ行い、unknownをsuccessへ縮退させない

## Dependencies

none
