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
2. `glm-worker --codex-limit`を実行し、machine JSON 1行の`five_hour.resets_at`と`five_hour.resets_at_rfc3339`を取得する。生のrate-limit JSONの自由解釈や時刻の暗算を行わない。
3. wake_at = `five_hour.resets_at` + 2分の安全マージンとする。「5時間後」「5時間10分後」などの固定相対時間を使わない。
4. 「wake scheduler登録」に従い、wake専用taskのthread IDへ紐付く一回schedulerを登録または更新する。
5. 登録確認が取れたら、停止と再開予定時刻を報告して終了する。`--codex-limit`や登録が失敗した場合も停止だけは必ず行い、scheduler無しの手動復旧を案内する。

## wake scheduler登録

- 期待keyは`codex-5h-wake-<wake専用task自身のthread ID>`とする。新規作成のautomation名もこの期待keyを使う。作成・再利用のどちらでも、扱う実automation IDがこの期待keyと一致することを確認してからupdate・verify・deleteする。
- 再利用できる既存automationは、`target_thread_id`がwake専用task自身のthread IDと完全一致するものだけとする。この列挙は発火前の既存scheduler再利用判定だけに使い、発火後の削除へ流用しない。Codex appのautomation一覧、または`CODEX_CONFIG_DIR`の`automations/*/automation.toml`の`target_thread_id`を読んで列挙する。automation名だけの一致を再利用の根拠にしない。
- 列挙結果が1件で、かつその実automation IDが期待keyと一致する場合だけ、新規作成せずその実automation IDへ絶対時刻update(UTCの`DTSTART:YYYYMMDDTHHMMSS` + `RRULE:FREQ=DAILY;COUNT=1` + status ACTIVE)を行う。`DTSTART;TZID=...`は使わない。実IDが期待keyと不一致の場合と列挙結果が複数件の場合はどれもupdateせずfail closedとし、Codex Desktop UIで人間が確認・整理するまで手動復旧を案内する。
- 列挙結果が0件の場合だけ新規作成する。DTSTART付き即時createはCodex appへ拒否されるため、DTSTARTなし・status PAUSED・`RRULE:FREQ=HOURLY`のplaceholder作成と、成功応答に含まれる実automation IDの確認、その実IDへの絶対時刻update(UTCの`DTSTART:YYYYMMDDTHHMMSS` + `RRULE:FREQ=DAILY;COUNT=1` + status ACTIVE)の二段階で行う。`suggested_create`は候補カード表示のみなので呼ばない。作成応答の実automation IDが期待keyと不一致の場合は、返却されたその実IDだけをbest-effort削除してfail closedとする。
- `automation_update`の返り値全体を文字列として検査する。`invalid`・`error`・`failed`・空文字列・`Rendered suggestion`のいずれかを含む場合は作成・更新失敗とする。content欄だけ読んで空出力を成功扱いにしない。
- 最終確認として、`glm-worker --verify-auto-resume <実automation ID> <wake_atのRFC3339> <wake専用task自身のthread ID>`を実行する。exit 0と結果JSONだけを登録成功の根拠にする。
- 検証失敗時は引数とtool schemaを1回だけ修正して再試行する。それでも失敗する場合は、作成済みautomationを作成時の実automation IDだけを対象に削除または停止し、手動復旧を案内してfail closedとする。

## wake専用taskの処理

schedulerから呼ばれたら、次の5操作だけを行う。

1. 発火済みの前回schedulerを削除する。削除対象はheartbeat発火指示に渡された実`automation_id`だけとする。`automation_id`が渡されていない場合は何も削除せずfail closedで終了する。target一致検索による削除は行わない。
2. 親実装taskのthread IDへ固定短文「作業を続けろ」を1回だけ送信する。thread IDが指定されていない場合は送信せずにfail closedで終了する。送信方法はCodex appの既存task間送信(Greptile専用taskが親taskへ使っている方式)を使う。
3. `glm-worker --codex-limit`で次回5h windowの`resets_at`を取得する。`resets_at`が現在時刻以前の場合は1回だけ再取得し、それでも過去の場合はfail closedで終了する。
4. wake_at = `resets_at` + 2分として、「wake scheduler登録」に従い自taskへ次回schedulerを登録する。
5. `--verify-auto-resume <実automation ID> <wake_atのRFC3339> <自taskのthread ID>`で登録結果を確認して終了する。

- 実装・review・task判断・diff解析・repository全体の再読・状況要約・設計判断を行わない。5操作に必要な読み込み・推論・出力以外を行わない。
- schedulerを追加して増殖させない。常に1件だけを維持する。
- いずれの操作が失敗しても、失敗を報告して終了する。放っておいた未処理queueを作らない。人間は次項で復旧できる。

## 手動復旧

自動化が故障しても、人間が次だけで復旧できる。

- Limit確認: `glm-worker --codex-limit`
- scheduler確認・削除・再登録: Codex Desktopの既存UI
- 親実装task再開: Codex Desktopから親実装taskへ「作業を続けろ」

## 不変条件

- `glm-worker --codex-limit`はrate-limit情報の読み取り専用machine JSON出力だけを行う。scheduler作成・削除・親taskへの送信をglm-workerへ実行させない。
- automationの探索は発火前の既存scheduler再利用判定だけに限定し、対象wake taskの`target_thread_id`完全一致と期待key(`codex-5h-wake-<wake thread ID>`)への実ID一致だけを根拠にする。発火後の削除はheartbeatで渡された実`automation_id`だけを対象とし、欠落時は何も削除せずfail closedする。update・verify・deleteは確認済みの実automation IDだけを使う。固定名・固定IDだけの一致で他repositoryのwake schedulerをupdate・削除せず、一致するautomationが複数件ならfail closedする。
- GLM Resume automation(`glm-worker-resume-*`)とGreptile schedulerのownershipを変更しない。
- 対象repository固有の`IMPLEMENTATION_PLAN`・task lifecycleをこの運用へ結合しない。wake後の作業再開は親実装taskの既存手順へ任せる。
- launchd・cron・常駐daemon・独自UI・人間向けscheduler管理CLIを作らない。
