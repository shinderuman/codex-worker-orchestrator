# 親Codex 5h Limit自動再開

親実装Codex taskがCodex 5h rate limitへ到達した場合の停止とwake専用taskによる再開。GLM provider側rate limitは`glm-execution.md`のmachine-owned recoveryで扱い、このschedulerを重ねない。

## 不変条件

- wake専用taskは親実装taskとは別threadで、対象repositoryのCodex Desktop projectに所属させる。低コスト・低reasoning modelだけを使う。
- schedulerはwake専用taskだけをtargetとし、1 wake taskにつき1件だけ維持する。automation identityをrepository間で共有しない。
- wake taskには起こす親実装taskのexact thread IDを持たせる。親threadを特定できない状態で送信しない。
- Weekly Limit復旧、追加credit利用、親不在中のGLM単独開発は対象外。

## machine authority

- reset evidence、wake時刻、expected automation identity、create/update/retry/cleanup、schedule、保存実体postconditionはCodex wake machine transactionを唯一のauthorityとする。親は値・順序・retryを再計算・再構成しない。
- transaction outputの`write` / `cleanup`は`boundary:"external-unenforceable"`である。repositoryが直接所有できないCodex app mutationだけを親がlosslessにrelayする。
- external toolのraw responseはmachine response admissionへそのまま返す。responseの意味、automation ID、status、scheduleを親が補正して次stateを作らない。
- machineが`verified`を返した場合だけ成功とする。`failed`、tool/command error、malformed response、保存実体`UNAVAILABLE`はfail closedである。
- tool/capabilityが利用不能な場合はmachineが提供するbounded failure/abort pathを使い、他thread・rollout・plugin transport・remote-control探索へnormal fallbackしない。

## 5h Limit到達時

1. 親Codexが5h limitで停止したら開発全体を止め、実行中GLM taskも安全停止する。新規dispatchしない。
2. exact wake thread identityでCodex wake planを取得する。
3. machine outputが要求するexternal writeだけをlosslessにrelayし、raw responseを同じtransactionへ返す。次writeが返る場合も同じtransactionだけを継続する。
4. `verified`ならmachine-projected wake時刻を報告して終了する。fail closedなら停止を維持し、手動復旧を案内する。

親はtransactionの内部stage、RRULE/DTSTART、placeholder、attempt、exact-ID cleanup、保存実体verifyを理解・再現する必要はない。

## wake専用task

scheduler発火後のwake taskは次だけを行い、repository実装・review・設計判断を行わない。

1. 発火指示のexact automation IDと自身のthread identityをmachine transactionへ渡す。identity不整合・ID欠落・reset evidence不正は送信前にfail closedする。
2. machineが要求するexternal writeだけをrelayし、raw responseをtransactionへ戻す。`verified`になる前に親taskを起こさない。
3. `verified`後だけ、設定済みの親実装threadへ固定短文「作業を続けろ」を1回送る。親thread欠落や送信failureは成功扱いしない。
4. 親送信成功後にwake処理を終了する。既存automationの変更はmachineが明示したwrite/cleanup以外へ広げない。

外部automation mutationとtask間送信はrepositoryから直接強制できないresidual boundaryである。ここで必要なのはlossless relayと成功/失敗の報告だけで、scheduler内部の設計判断ではない。

## 手動復旧

自動化がfail closedした場合、人間はCodex Desktopの既存UIでschedulerを確認・削除・再登録し、親実装taskへ「作業を続けろ」を送れる。repository側は独自UI、launchd、cron、常駐daemon、別scheduler管理系を作らない。

## ownership

- `glm-worker --codex-limit`はrate-limit情報のread-only projectionだけを行う。
- Codex wake transactionはidentity・limit evidence・schedule・retry・postconditionをmachine-ownedとするが、Codex app writeとtask間送信そのものはexternal-unenforceableである。
- GLM provider 5h recoveryをこのschedulerへ統合しない。Greptile schedulerのownershipを変更しない。
- repository固有Plan/task lifecycleをwake schedulerへ結合しない。wake後は親実装taskの既存lifecycleへ戻る。
