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

## Resolved references

- rate-limit task `01b57694-6b9e-4ded-a859-803cfb4d9ae3`向け予約で、DTSTART付きimmediate createの拒否後に`suggested_create`を使い、候補カード表示を保存済みautomationと誤認した
- `/Users/shinderumanm/.codex/automations/*/automation.toml`には対象task ID・auto-resume keyの保存実体がなく、reset時刻を過ぎてもHeartbeatが発火しなかった
- current `glm-auto-resume.md`はPAUSED placeholder create、同ID update、`--verify-auto-resume`を同一tool orchestrationで行う契約を持つが、親実行がそのtransactionを完遂しなかった
- current thread identityは`CODEX_THREAD_ID`をcommand boundaryの正とすべきところ、親が会話要約由来のthread IDをtool引数へ手入力した
- `IMPLEMENTATION_RULES.md`の`repository automation authority`は、Rate Limitに限定せず現在および将来のrepository taskでautomation単位の再承認を不要としている
- 既存の恒久許可があるにもかかわらず、親Codexは当初それを適用せず、誤ったcreate手順後のautomation安全審査拒否を新たな許可不足として扱った
- `global-heartbeat-authority.md`は既存authorityと同じ許可を別taskへ重ね、さらにユーザーの「すべてのタスク」を「すべてのrepository」へ拡張したため、要求を本taskへ統合して削除する

## Purpose

Heartbeatの恒久許可が再承認なしで実際のautomation作成経路へ適用されることを確認した上で、GLM rate-limit自動再開の作成・channel binding・UTC schedule・保存実体検証を分断不能なtransactionにし、許可の再取得、未作成、誤channel、誤時刻を成功として扱えないようにする。

## External feasibility

status: observation
assumption: Codex app automation APIがPAUSED placeholder create、exact ID update、current-thread binding、persisted schedule verifyを許容し、repository側からcreate/update/verifyを一つのtransactionとして実証できる

## Contract

- 現行automation tool境界で、PAUSED placeholder create、返却されたexact IDへのUTC one-shot update、current process identityを使う実体verifyを一つの失敗閉包として扱えるかGo/No-Go判定する
- `IMPLEMENTATION_RULES.md`の恒久許可だけが存在し、現在turnで許可を再表明していない条件でも、親Codexとautomation APIが再承認要求なしにtransactionを開始できるかを実証する
- 既存authorityが実効しなかった原因を、親Codexのauthority適用漏れ、誤ったcreate/update手順、automation安全審査の可視性・外部境界に分けて特定し、成立する原因層を修正する
- repository authorityを実際のtool境界へ伝える追加のsupported surfaceが必要な場合だけ、その正本と`./install.sh`後の配置を設計する。配置先`~/.codex/AGENTS.md`だけへの手編集や、同じ許可taskの重複作成で代用しない
- `suggested_create`、候補カード表示、automation名、会話memory、親が手入力したthread IDを予約成功根拠にしない
- create/update/verifyの中間結果をSolへ戻さず、最終postconditionまたはbounded failureだけを返す
- update/verify失敗時は新規placeholderを削除または停止し、半端な予約を残さない
- scheduler時刻はrate-limit packetのoffset付き時刻からUTC DTSTARTへ機械変換し、保存TOMLとSQLite `next_run_at`を照合する
- repository実装で外部automation APIを安全に強制できない場合は、No-Go根拠と外部修正境界を明示し、効果のないprompt増量や疑似schedulerを追加しない

## Must not

- automation安全審査を迂回しない
- 現在turnでユーザーへ同じ許可を言い直させることを正常系にしない
- shell sleep、daemon、cron、定期pollingを代替として追加しない
- current thread IDを会話要約・task一覧・時刻近接から推測しない
- task/sessionを新規起動して再開を代用しない
- raw automation directiveをユーザーへ操作させない

## Acceptance criteria

- 外部成立性PoCでcreate/update/verify/cleanupの各段階を実automation APIに対して確認し、Go/No-Goを親Solが判定できるbounded artifactがある
- 既存の恒久許可だけをauthorityとし、現在turnで追加許可を受けずにHeartbeatを作成・更新・検証・cleanupできる実機scenarioがある
- 恒久許可が認識されない場合、その原因層を修正した後に同scenarioが通る。repository外の変更が不可欠なら、実効するsupported authority surfaceと外部修正要求を特定し、単なるRules追記を完了扱いしない
- Goの場合、候補カードのみ、wrong thread、wrong UTC DTSTART、PAUSED、SQLite/TOML不一致、update失敗、verify失敗を予約成功として扱わないtest/evalがある
- Goの場合、rate-limit packetから同一session resumeまでの正常transactionを追加AI callなしで機械検証できる
- No-Goの場合、repository側で保証不能な境界と既存manual fallbackを明示し、production変更を行わない
- harnesslint、関連test、独立review、必要なcurrent snapshot validationを完了する

## Historical invariants

- repository automation authorityは既存の恒久許可を正とし、automationごとの再許可を要求しない
- auto-resumeは同一checkout・同一task・同一sessionだけを再開する

## Dependencies

none

## Review findings

none

## Current boundary

未発火Heartbeatの原因を「保存実体未作成」と確定済み。実装前に外部automation APIの強制可能範囲を再確認する。
