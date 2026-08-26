# Task: Codex 5h wakeの次回予約lifecycleを修復する

## Original instruction

````text
5h wakeのチャンネルに次のスケジュール作るのはいつやるんだ？
````

## Amendments

none

## Resolved references

- 「5h wakeのチャンネル」はCodex task `01a03a9e-10a0-7f11-801c-f04e5dbd5490`を指す。
- 「次のスケジュール」は、5h wake発火時に`glm-worker --codex-limit`が返した次の5h reset `2026-08-26T10:16:27Z`へ2分marginを加えた次回wakeを指す。
- 2026-08-26 14:16 JSTの実発火では、wake taskは発火済みautomationを先に削除し、親実装taskへ再開指示を送った後、DTSTART付きheartbeatの即時createを試みて拒否された。続けて`mode: suggested_create`を実行し、永続automationではなく候補cardを表示しただけで「次回wake登録カードを表示」と終了した。
- 親実装taskがautomation実体を確認した時点で、次回wake automationは存在しなかった。したがって「親task再開」と「次回wake永続予約」のうち後者がfalse-completeである。

## External feasibility

status: implementation

evidence:

- Codex app実tool responseで、DTSTART付きimmediate `create`が拒否されることを再確認した。
- 既存automationへのDTSTART付き`update`は直前cycleの初回配置で成立し、TOML/SQLiteを`glm-worker --verify-auto-resume`で検証済みである。
- `suggested_create`は候補cardでありautomation実体を作成しないことを、今回のautomation欠損とtool結果で再確認した。

## Purpose

5h wake発火後に親taskを起こすだけで終わらず、同じautomation IDへ次回5h reset由来のone-shotを永続更新し、次回wakeの欠落と候補cardだけのfalse-completeを防ぐ。

## Contract

- wake発火時に既存automationを先に削除せず、親taskへの固定再開指示送信と次回5h reset取得後、同じautomation IDへ次回one-shotを直接updateする。
- update成功後にautomation tool responseと保存実体を検証し、正しいID・target task・ACTIVE・絶対時刻・one-shotが一致した場合だけ次回予約完了とする。
- `suggested_create`を永続予約として使わず、card表示を成功扱いしない。
- reset取得、親送信、update、実体検証のいずれかが失敗した場合は予約済みと報告せず、既存automationを削除しない。安全に停止できる場合は同じautomationをPAUSED化し、明示的な復旧境界を残す。
- 現在automationが欠損しているbootstrapでは、Codex appのcreate制約を一次証拠で扱い、候補cardだけで完了せず、次回wake automation実体を1件だけ成立させる。
- GLM resume automation、Greptile automation、親実装task、wake専用taskのownershipを混同しない。

## Must not

- 発火済みautomationを次回update前に削除しない。
- `suggested_create`、rendered card、空または失敗tool responseを予約成功扱いしない。
- 固定間隔polling、cron、launchd、daemon、複数wake automationを追加しない。
- reset時刻を固定5時間後として推測しない。
- wake taskで実装・review・diff解析・task判断を行わない。
- GLM task state、session、checkpointを破棄しない。

## Acceptance criteria

- wake発火から親task通知、次回reset取得、同一automation update、実体検証までの順序がcanonical instruction/promptへ固定される。
- 旧delete-before-createと`mode: suggested_create` fallbackが撤去される。
- update失敗時に次回予約済みと報告せず、既存automationを失わないfail-closed test/contractがある。
- 次回wake automationがwake専用taskへ1件だけ実在し、取得したreset+marginの絶対時刻・ACTIVE・one-shotとしてmachine verificationを通る。
- 親実装taskへの送信は固定短文1回だけで、GLM/Greptile schedulerを変更しない。
- 関連test、通常quality gate、独立review、親Codex最終採否、commit/push、本配置を完了する。

## Historical invariants

- Codex 5h wakeはrepository固有implementation Planを再構築せず、別の低コスト専用Codex taskから親実装taskを起こす。
- five-hour windowは`window_duration_mins == 300`でprimary/secondaryを固定せず選び、Weekly Limitは対象外である。
- scheduler操作はCodex appの既存automation interfaceを使い、glm-worker Go codeへscheduler管理を実装しない。

## Dependencies

none

## Review findings

- 2026-08-26初回実発火で、旧promptがdelete-before-createを要求し、appのimmediate anchored create制約と合成して次回予約を失った。
- wake taskはcreate failure後に禁止済みの`suggested_create`へfallbackし、永続化postconditionを確認せずcard表示を完了報告した。

## Current boundary

親実装taskへの再開指示は成功し、commit authorization taskの保存checkpointは同じsessionで再開済み。次回wake automationは欠損しているため、現ACTIVE完了直後の最優先NEXTとして修復する。
