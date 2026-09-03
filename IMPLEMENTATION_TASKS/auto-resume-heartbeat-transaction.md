# Task: Auto-resume heartbeat transaction

## Original instruction

````text
HeartBeat時刻過ぎてるぞ
チャンネルかなにか間違えてる
次回は間違えないようにしろ
Heartbeatを消して作業を再開しろ
````

## Amendments

none

## Resolved references

- rate-limit task `01b57694-6b9e-4ded-a859-803cfb4d9ae3`向け予約で、DTSTART付きimmediate createの拒否後に`suggested_create`を使い、候補カード表示を保存済みautomationと誤認した
- `/Users/shinderumanm/.codex/automations/*/automation.toml`には対象task ID・auto-resume keyの保存実体がなく、reset時刻を過ぎてもHeartbeatが発火しなかった
- current `glm-auto-resume.md`はPAUSED placeholder create、同ID update、`--verify-auto-resume`を同一tool orchestrationで行う契約を持つが、親実行がそのtransactionを完遂しなかった
- current thread identityは`CODEX_THREAD_ID`をcommand boundaryの正とすべきところ、親が会話要約由来のthread IDをtool引数へ手入力した

## Purpose

GLM rate-limit自動再開の作成・channel binding・UTC schedule・保存実体検証を分断不能なtransactionにし、未作成や誤channel/誤時刻を予約成功として扱えないようにする。

## External feasibility

status: observation

Codex app automation APIはrepository外境界である。repository側からcreate/update/verify transactionをどこまで機械強制できるかを先に実証し、実行可能な対策だけをimplementationへ進める。

## Contract

- 現行automation tool境界で、PAUSED placeholder create、返却されたexact IDへのUTC one-shot update、current process identityを使う実体verifyを一つの失敗閉包として扱えるかGo/No-Go判定する
- `suggested_create`、候補カード表示、automation名、会話memory、親が手入力したthread IDを予約成功根拠にしない
- create/update/verifyの中間結果をSolへ戻さず、最終postconditionまたはbounded failureだけを返す
- update/verify失敗時は新規placeholderを削除または停止し、半端な予約を残さない
- scheduler時刻はrate-limit packetのoffset付き時刻からUTC DTSTARTへ機械変換し、保存TOMLとSQLite `next_run_at`を照合する
- repository実装で外部automation APIを安全に強制できない場合は、No-Go根拠と外部修正境界を明示し、効果のないprompt増量や疑似schedulerを追加しない

## Must not

- automation安全審査を迂回しない
- shell sleep、daemon、cron、定期pollingを代替として追加しない
- current thread IDを会話要約・task一覧・時刻近接から推測しない
- task/sessionを新規起動して再開を代用しない
- raw automation directiveをユーザーへ操作させない

## Acceptance criteria

- 外部成立性PoCでcreate/update/verify/cleanupの各段階を実automation APIに対して確認し、Go/No-Goを親Solが判定できるbounded artifactがある
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
