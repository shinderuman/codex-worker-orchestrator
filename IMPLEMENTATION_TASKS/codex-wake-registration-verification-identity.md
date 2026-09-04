# Task: Codex wake registration verification identity

## Original instruction

````text
https://github.com/shinderuman/codex-worker-orchestrator/pull/345
CodeRabbit, Greptileのレビュー結果を参照して必要なら対応するタスクを作ってくれ
````

## Amendments

none

## Resolved references

- reviewed head: `a595057f5912ece29a99a778c683619612f9b7a5`
- CodeRabbit comment `3930465305`: `https://github.com/shinderuman/codex-worker-orchestrator/pull/345#discussion_r3930465305`
- finding: Codex 5h wake automationは別のwake専用taskをtargetにするが、親task processの`CODEX_THREAD_ID`で`--verify-auto-resume`を実行すると正しい登録をthread mismatchとして拒否する
- Greptile review `5534895674`はreviewed headにactionable defectなしとしたが、このidentity mismatchを反証する具体的evidenceは提示していない

## Purpose

Codex 5h wake登録の検証identityをautomationの実targetであるwake専用taskへ一致させ、親process identityとの混同で正しいscheduler登録を拒否しないようにする。

## External feasibility

status: not-applicable

## Contract

- current HEADでCodeRabbit findingの成立性を検証し、Codex wake登録とGLM同一thread auto-resumeのidentity contractを分離する
- GLM auto-resumeの2引数CLIとcurrent process `CODEX_THREAD_ID`束縛は維持し、Codex wake登録だけに適用するsupported verification pathを設計する
- wake専用task IDはCodex appの作成/選択結果から得たexact identityだけを使い、会話要約・automation名・時刻近接から推測しない
- explicit target IDを受ける場合はCodex wake登録専用のclosed grammarへ限定し、汎用のthread照合迂回や任意automation検証へ拡張しない
- 作成・更新・verify・failure cleanupを既存transaction内で完結させ、正しいwake targetと親task targetの取り違えを両方fail closedする

## Must not

- GLM worker/reviewerへremote write authorityを与えない
- current parent thread IDをwake task IDとして流用しない
- thread IDをenv dump、task一覧近接、automation名から復元しない
- verifyを省略・緩和して登録成功扱いしない
- raw automation directive、cron、daemonを代替にしない

## Acceptance criteria

- parent processとwake専用taskが異なる実機またはintegration scenarioで、正しいwake automationがverify成功する
- parent IDをtargetにしたautomation、誤wake ID、ID欠落、手入力由来の未検証IDをfail closedする
- GLM同一thread auto-resumeの既存2引数/env-bound verificationが退行しない
- Codex wake登録・発火後再予約の両経路が同じidentity contractを使う
- relevant test、independent reviewer、Sol review、current snapshot validation、commit/install/smokeを完了する

## Historical invariants

- Codex wakeは別の低コスト専用taskをtargetにし、親実装taskを直接scheduler targetにしない
- automation更新・verify・deleteは確認済みexact IDだけを対象とする

## Dependencies

none

## Review findings

````text
Use the wake-task thread ID for registration verification. CODEX_THREAD_ID is read from the parent process and compared with the saved automation's target_thread_id. The automation targets the separate wake-only task, so valid registrations can fail verification. Supply the wake-task thread ID through a registration-specific path or run verification in that context.
````

## Current boundary

PR 345の外部review findingをcurrent HEADへ適応して検証する。
