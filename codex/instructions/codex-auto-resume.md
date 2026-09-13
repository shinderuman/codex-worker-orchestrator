# 親Codex 5h Limit自動再開

親実装Codex taskがCodex 5h rate limitへ到達したときの停止と、reset後のwake専用Codex taskによる再開。GLM provider側rate limitには`glm-auto-resume.md`を使い、同一taskへ両方のschedulerを重ねない。

## 前提

- wake専用taskは親実装taskと別のCodex taskとし、対象repositoryのCodex Desktop projectへ所属させる。projectlessのPoC taskを本番targetに使わない。
- wake専用taskは低コスト・低reasoning model(Luna Low等)だけで実行する。Sol相当の高コストmodelをwake用途に使わない。
- Codex wake schedulerはwake専用taskだけをtargetとする。親実装task・Greptile専用taskへCodex wake schedulerを置かない。1 taskへ複数schedulerを置かない。
- wake schedulerのidentityはwake専用task自身のthread IDへ紐付ける。automation名・automation IDをrepository間で共有せず、固定名・固定IDの一致だけでの探索・update・削除を行わない。
- wake専用taskのpromptまたは発火指示には、起こす親実装taskのthread IDを指定しておく。既存wake taskの指定が現在の親実装taskを指さない場合は、指定を現在の親taskへ更新するか新規wake taskを作り、automationのtargetをそちらへ向ける。親taskを特定できないままwakeさせない。
- Weekly Limitからの復旧・追加credit利用・親不在中のGLM単独開発は対象外。

## 5h Limit到達時(親実装task)

1. 親実装taskのCodex呼び出しが5h rate limitで停止したら、GLMを含む開発全体を停止する。実行中のglm-worker taskは`glm-worker --stop`で安全停止し、新しいdispatchを行わない。
2. exactなwake専用taskのthread IDを使って`glm-worker --codex-wake-plan <wake-thread-id>`を実行する。5h reset、wake時刻、期待key、create/update mode、target thread、schedule文字列を親で再計算・再構成しない。
3. 「wake transaction relay」に従い、machineが返したwriteだけをCodex app toolへlosslessに渡し、そのraw responseをmachine validationへ戻す。
4. machine outputが`status=verified`になった場合だけ登録成功とする。`failed`、command error、保存実体取得不能を成功へ読み替えない。
5. 登録確認が取れたらmachine outputの`wake_at_rfc3339`を再開予定時刻として報告して終了する。登録が失敗した場合も停止だけは維持し、scheduler無しの手動復旧を案内する。

## wake transaction relay

- `--codex-wake-plan`の`status=write_required`では、`write` objectを変更せずそのままCodex appの`automation_update`へ渡す。親はautomation ID、name、target thread、status、RRULE、DTSTART、modeを補完・修正・再計算しない。
- app toolのraw responseは内容を要約・整形・再解釈せず、同じplan outputの`token`とともに`glm-worker --codex-wake-response-stdin <payload-bytes> <token> [--sha256 <hex>]`へ渡す。stdin payloadは既存のbyte-counted stdin contractに従う。
- machineが次の`status=write_required`を返した場合だけ、そのoutputの`write`を同じ手順で1回実行する。retry回数、transaction identity、create後のexact returned ID、同一automationへのupdateはmachine stateを正とし、親で別transactionを組み立てない。
- machineが`cleanup`を返した場合、best-effort cleanupはそのspecが指すexact automationだけへ実行する。automation名・時刻近接・一覧探索でcleanup対象を広げない。
- `status=verified`だけを成功とする。`status=failed`、tool/command error、malformed response、保存実体`UNAVAILABLE`はfail closedとする。
- `write.boundary=external-unenforceable`は、repositoryからCodex app write自体を直接強制できない境界を表す。machine spec生成・response admission・保存実体postconditionまでをrepository側が所有し、親は外部writeのlossless relayだけを行う。

## wake scheduler登録

expected key/thread identity、UTC one-shot schedule、PAUSED placeholderからACTIVE update、tool responseのfield validation、exact-ID retry/cleanup、保存実体verifyは`--codex-wake-plan` / `--codex-wake-response-stdin`のmachine transactionを正とする。親はこの内部procedureを再構成せず、前節のexternal-unenforceableなCodex app writeだけをlosslessにrelayする。

## wake専用taskの処理

schedulerから呼ばれたら、次の4操作をこの順序だけを行う。発火済みautomationの削除と新規作成は行わない。

1. 発火指示に渡されたexact `automation_id`とwake専用task自身のthread IDを使い、`glm-worker --codex-wake-plan <wake-thread-id> --fired-automation-id <automation-id>`を実行する。`CODEX_THREAD_ID`とwake threadが一致しない場合、ID欠落、wrong/stale ID、reset evidence不正は親実装taskへ送信する前にfail closedする。
2. 「wake transaction relay」に従い、machineが返した同じautomation IDへのwriteだけを実行し、raw tool responseをmachine validationへ戻す。削除・新規create・ID探索・時刻再計算を行わない。machine outputが`status=verified`になるまで親実装taskを起こさない。
3. machine outputが`status=verified`になった場合だけ、親実装taskのthread IDへ固定短文「作業を続けろ」を1回だけ送信する。thread IDが指定されていない場合は送信せずにfail closedで終了する。送信方法はCodex appの既存task間送信(Greptile専用taskが親taskへ使っている方式)を使う。
4. `status=verified`かつ親送信が成功した場合だけwake処理成功として終了する。`failed`、command error、保存実体取得不能を成功へ読み替えない。

親送信・machine transaction・実体検証の失敗扱いは次による。

- machine transactionまたは実体検証が失敗した場合、親実装taskへ送信せず、次回予約済みと報告せず、machineが明示したcleanup以外で既存automationを変更しない。
- `status=verified`後の親送信だけが失敗した場合、verified済みの次回予約を変更せず、親送信失敗として報告して終了する。親が再開したとは報告しない。
- `automation_update`の応答判定、retry回数、PAUSED化、保存実体検証はmachine transaction outputだけを正とする。親がfailure語、status、schedule、IDを独自判定して続行しない。
- 実体検証`UNAVAILABLE`は成功ではない。UI表示や時刻の目視一致で`verified`へ昇格させず、fail closedとする。

- 実装・review・task判断・diff解析・repository全体の再読・状況要約・設計判断を行わない。4操作に必要な読み込み・推論・出力以外を行わない。
- schedulerを追加して増殖させない。常に1件だけを維持する。
- いずれの操作が失敗しても、失敗を報告して終了する。放っておいた未処理queueを作らない。人間は次項で復旧できる。

## 手動復旧

自動化が故障しても、人間が次だけで復旧できる。

- Limit確認: `glm-worker --codex-limit`
- scheduler確認・削除・再登録: Codex Desktopの既存UI
- 親実装task再開: Codex Desktopから親実装taskへ「作業を続けろ」

## 不変条件

- `glm-worker --codex-limit`はrate-limit情報の読み取り専用machine JSON出力だけを行う。Codex wake machine transactionは同じlimit projection semanticsを再利用するが、scheduler作成・削除・親taskへの送信をglm-worker自身へ実行させない。
- automationの探索は発火前の既存scheduler再利用判定だけに限定し、対象wake taskの`target_thread_id`完全一致と期待key(`codex-5h-wake-<wake thread ID>`)への実ID一致だけを根拠にする。発火後のupdate・verify・PAUSED化は発火指示に渡された実`automation_id`だけを対象とし、欠落時は何もせずfail closedする。wake専用taskは発火済みautomationを削除しない。固定名・固定IDだけの一致で他repositoryのwake schedulerをupdate・削除せず、一致するautomationが複数件ならfail closedする。
- Codex wake登録・発火後再予約の実体検証は`--verify-codex-wake`と同じ閉じたgrammarのidentity/postcondition contractに従う。`glm-worker --verify-auto-resume`はGLM同一thread auto-resume専用であり、Codex wake登録の検証へ使わない。親thread ID・誤wake ID・ID欠落はfail closedする。
- GLM Resume automation(`glm-worker-resume-*`)とGreptile schedulerのownershipを変更しない。
- 対象repository固有の`IMPLEMENTATION_PLAN`・task lifecycleをこの運用へ結合しない。wake後の作業再開は親実装taskの既存手順へ任せる。
- launchd・cron・常駐daemon・独自UI・人間向けscheduler管理CLIを作らない。
