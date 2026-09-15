# Task: Auto-resume heartbeat transaction

## Original instruction

````text
HeartBeat時刻過ぎてるぞ
チャンネルかなにか間違えてる
次回は間違えないようにしろ
Heartbeatを消して作業を再開しろ
````

## Amendments

### 2026-09-04

````text
とりあえず恒久的に許可するのでHeartbeatをつくれ
````

````text
Rate LimitTask専用だなんて言ってない
恒久的に許可すると言ってる
すべてのタスクで許可する
````

````text
そこに書いても./install.shで消えるだろ
お前バカなのか
````

````text
HeartBeatのタスクDupeしてねえか
````

````text
そこの記載されて効いてないなら直さないといけないんじゃないの
````

````text
その失敗を再現しないようにするタスクはあるのか
````

### 2026-09-05

````text
今まで問題なく保存できてたのになぜ今回間違えたんだ
これは大昔に検証して再現しないようにしていただろ
````

````text
機械的に再開時間を返すハーネスとかにするべきなんじゃないか
判断はお前に任せるが
````

### 2026-09-15

````text
呼びかけたがtimerで止まっているっぽく応答がないので強制停止した

何を言ってるかわからない
なんでそこでスケジュールを作らず自己判断で強行するんだ
スケジュールを作るルールだろ

お前がそういう事するのが全部バグだって言ってるだろ
ルールに従え
ルールに従えないならそれはバグだから機械でやるようにしろ
そういう自己判断をするな
````

````text
Updateじゃなくていつも新規作成してから更新していただろ
そんなことすら忘れたのか
````

````text
なんで使えないの？今まで使えてたじゃん
使えなくなっているならActiveタスクを進めても何も意味なくない？
````

````text
使えないわけがないだろ
````

````text
作れるじゃねえかよ嘘付いてるんじゃねえよ
````

````text
お前ひょっとして死ぬほどバカか？
まず今の状態でGLMのLimitの回復からどうやって再開するつもりだよ
そして画像に見せたセッションのログを見るなりしてどうやって実現したか確認するべきだろ
````

## Resolved references

- 旧taskは`a595057`で完了扱いされたが、そのAcceptanceには「既存の恒久許可だけをauthorityとし、現在turnで追加許可を受けずにHeartbeatを作成・更新・検証・cleanupできる実機scenario」が含まれていた
- 2026-09-04の次回rate-limit停止では、恒久許可とrepository上のauthorityが存在したにもかかわらず、Codex app automation作成の外部安全判定がGLM利用全般を禁止されたものと解釈し、明示許可の再取得までcreateを拒否した
- 当該create promptはtask IDとrepo rootだけを記載し、`IMPLEMENTATION_RULES.md`が要求する恒久許可、同一session resume、GLMのGit remote write禁止とのscope分離を伝えていなかった
- 明示許可後は同じautomation create/update/verifyが成功したため、保存transaction自体ではなくauthorityのtool-boundary伝達と親の手入力promptが今回の再発境界である
- 現在のin-flight task/sessionは再起動せず、作成済みHeartbeatによるresumeを継続する。本taskはその完了後に再開する
- 2026-09-05、親CodexはGLM rate-limit packet受領後に必須の`glm-auto-resume.md`を読まず、検証済みtransactionを使わずにtimezoneなしの`BYHOUR=14`を手入力してautomation `glm-preflight-task-resume`を作成した
- 当該automationはUTC/JST解釈が未確定なうえ、packetの`auto_resume_key=glm-worker-resume-4b1083bd6f6e-be2df76b`ともIDが一致せず、`--check-wake-coalesce`、PAUSED placeholderからのUTC update、`--verify-auto-resume`を全て省略したまま予約成功と誤報した
- `e1b5c74`はschedulerがTZIDを無視することとUTC DTSTARTへの変換を既に固定し、`a595057`はTOML/SQLite/next_run_at検証を実装済みだったため、既存機構の欠落ではなく親Codexが条件付きinstructionを適用しなかったことが直接原因である
- 正しい二段階作成後の`glm-worker --verify-auto-resume`はread-only検証のはずが`~/.glm-worker`へrepo-rootを書こうとしてsandboxで失敗し、transaction cleanupにより予約を削除した。検証commandの不要なstate初期化も正常系を権限判断へ依存させる再発境界である
- 2026-09-15にユーザーが提示したCodex Desktop画像では、別taskで「1時間ごと」のスケジュールが作成済みで、右ペインのソースに`ChatGPT App Tools`が表示されている。親Codexがcode-mode内のnested tool一覧だけからDesktop全体のautomation作成手段が存在しないと断定したのは誤りであり、正式なschedule tool経路を使わず代替へ分岐したparent orchestrationが再発境界である
- 画像のsession ID `01a0a2e0-fe77-7c81-976b-3a0f1433345f` のlocal rollout logでは、実tool callが`tools.mcp__codex_app__automation_update`である。最初のcreateは`targetThreadId`または`destination=thread`欠落で`isError:true`となり、同じtoolへ`destination:"thread"`を付けたcreateが成功した。親Codexは利用中のtool一覧だけで能力不存在を断定せず、既知の成功sessionのtool call一次証拠から正式経路と必須fieldを回収すべきだった
- `~/.codex/config.toml`では`codex-app-tools@openai-bundled`が`enabled=true`で、file更新時刻は2026-09-15 11:24:00 JST。現在の親thread `01a0a268-73a7-7d70-a60c-8a7d630eb4f8`は09:12:29開始でtool lookupが`undefined`、成功thread `01a0a2e0-fe77-7c81-976b-3a0f1433345f`は11:24:09開始で同toolを実呼出している。automation backendや恒久許可ではなく、plugin有効化前に開始した親threadのtool manifestが再bindingされていないことが直接原因である

## Purpose

既存のrepository automation恒久許可をCodex appのautomation安全判定へ毎回同じ形で伝え、親Codexの手入力漏れによって許可済みHeartbeatが拒否され、ユーザーへ再承認を求める再発を防ぐ。

## External feasibility

status: implementation
assumption: Codex app automation APIへ渡すpromptとtool call contextに、repository authority、対象task/session、許可範囲、GLM Git remote write禁止を明示すれば、追加のユーザー再承認なしに既存の恒久許可を正しく評価できる
evidence-source: producer
evidence: 既存transactionによるcreate/update/verify成功と、authority scope欠落時の外部安全判定拒否はResolved references記載の実機結果を正とする
go: 2026-09-15、外部安全判定の受理可否を成功前提にせず、生成済みauthority scopeを渡した実機結果を成功または一次証拠付き外部境界としてfail closedに確定できる設計でproduction実装へ進む

## Contract

- rate-limit packetからautomation create/update/verifyに必要なmachine specを一意に生成し、親Codexがauthority文面・task ID・repo root・同一session resume条件を手入力で再構成しない経路を設ける
- automation promptへ、`IMPLEMENTATION_RULES.md`のrepository automation恒久許可、現在taskに既にある実装継続authority、GLMの禁止範囲がGit remote writeであってGLM実行全般ではないことをboundedな定型文で含める
- 外部安全判定の拒否、`isError:true`、create/update/verify不一致を成功扱いせず、拒否理由と渡したauthority scopeをstructured evidenceとして残す
- 恒久許可を外部安全判定へ伝えた上でなお拒否された場合だけ、repository内で修正可能な境界とCodex app側の外部修正境界を分離して報告する
- PAUSED placeholder create、同一IDへのUTC one-shot update、current thread identityによる保存実体verifyという既存transaction invariantを維持する
- rate-limit packet受領からcoalesce・identity決定・UTC変換・create/update・保存実体verifyまでを単一machine transactionにし、親Codexが条件付きinstructionを読み落としても手入力scheduleへ分岐できないようにする
- stopped task stateを正として、Codex appへ渡すexpected automation ID/name、compact prompt、PAUSED placeholder create spec、絶対時刻UTC update spec、verify command引数を単一のstructured machine specとして生成する。親CodexはRFC3339 offset変換、schedule文字列、automation名、thread ID、promptを手入力・再構成しない
- Codex app tool境界は親Codexが担うが、親の役割は生成済みspecのfieldをlosslessにcreate/updateへ転送し、tool responseを次のmachine verifyへ渡すことだけに限定する。時刻・identity・成功判定を自然言語で補わない
- `--check-wake-coalesce`と`--verify-auto-resume`はread-only projectionとしてsession stateを書き換えず、sandbox内で実行可能にする。外部Codex app writeだけを必要なmutation境界として分離する
- `auto_resume_available:true`では、automation scheduleのcreate/update/verifyを必須のmachine transactionとし、親Codexがtool可視性や待機方法を理由にtimer・手動待機・別の再開手段へ自己判断で分岐できないようlifecycle admissionで強制する。automation tool境界が利用不能ならtask stateを保持してstructuredにfail closedし、代替実行しない
- 現在作成済みのresume automationやin-flight GLM sessionを再起動・置換しない

## Must not

- automation安全審査を迂回しない
- ユーザーへ既存の恒久許可を日時・予約・task単位で言い直させることを正常系にしない
- 「GLMに関しては禁止」をGLM実行全般の禁止へ拡張せず、Git remote write禁止とのscopeを混同しない
- repository Rulesの追記だけ、会話memory、親Codexの自由文promptを再発防止の完了根拠にしない
- shell sleep、daemon、cron、定期polling、新規GLM sessionを代替にしない
- automation scheduleを作成せず、親Codexのone-shot timer・長時間tool cell・手動時刻待ちを代替にしない
- 別repositoryや未許可操作へautomation authorityを拡張しない

## Acceptance criteria

- authorityを含むautomation specがrate-limit evidenceから機械生成され、必須authority field/textの欠落・scope混同・task/session不一致をfixtureでfail closedにできる
- 親Codexが自由文を組み立てず、生成済みspecをlosslessにautomation tool callへ渡せるinstruction/protocolがある
- `isError:true`を含む外部拒否responseをstructuredに検出し、成功payloadとの混同を防ぐtest/evalがある
- 既存の恒久許可だけをauthorityとし、追加許可を受けない実機scenarioでcreate/update/verify/cleanupが成功するか、authorityを正しく渡しても残る外部修正境界が一次証拠付きで確定する
- wrong thread、wrong UTC DTSTART、PAUSED、SQLite/TOML不一致、update/verify失敗を成功扱いしない既存coverageを維持する
- packetの`auto_resume_key`と異なるautomation ID、timezoneなしBYHOUR、coalesce未実行、verify未実行の各状態を予約成功として返せないfixtureがある
- read-only verifyがrepo-root/state書込みを行わず、sandbox write権限なしでもTOML/SQLite postconditionを検証できる
- `auto_resume_available:true`のrate-limit terminalからは、verified automation scheduleまたは外部tool境界のstructured failureだけへ遷移でき、schedule未作成のtimer・手動待機・直接resume pathをmachine fixtureで拒否できる
- `auto_resume_at_rfc3339`のoffset境界・日付跨ぎを含むfixtureで、生成specのUTC anchor、期待ID、prompt、placeholder/update、verify引数が一意になり、親入力なしで実Codex app transactionへ渡せる
- current in-flight task完了後に同じtask fileをACTIVEへ昇格し、独立reviewer、Sol semantic review、current snapshot validation、必要なinstall/smokeを完了する

## Historical invariants

- repository automation authorityは既存の恒久許可を正とし、automationごとの再許可を要求しない
- auto-resumeは同一checkout・同一task・同一sessionだけを再開する
- GLM worker/reviewerはGit remote writeを行わない

## Dependencies

none

## Review findings

- `a595057`の完了判定はauthority再伝達scenarioの再発を防げておらずfalse-completeだった
- `e1b5c74`と`a595057`の正しいinstruction/testが存在しても親Codexがrate-limit分岐でそれを読まなければ全transactionを迂回でき、自由言語だけでは再発防止になっていない
