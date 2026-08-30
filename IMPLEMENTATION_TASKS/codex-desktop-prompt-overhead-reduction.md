# Task: Codex Desktopの固定prompt overheadを削減する

## Original instruction

````text
Codex Desktopが無闇矢鱈に入れているトークンが大量にあるらしい
そしてそれを削る方法もあるらしい
初回の話しかけ時にはチャットの本文が600文字に対してCodex Desktopが入れている文字列が51400文字ぐらいあるらしい
限界までカリカリにチューニングしたいわけじゃないが流石にある程度は削ってトークン節約したい
そういう計画っていまのところあるか？
````

## Amendments

````text
Codexの無駄はこれから全然削っていくところだと思っている
つまりCodexになにかタスクを与えて動かしてなにか無駄がないか確認するタスクが常に必要
今回のBundleを出させるタスクもその1つだ
````

````text
CommentlintとBundle Diff以外の実装をお前が全部終えてその次にCommentlintをやらせて観測するつもり
````

## Resolved references

- 実機Codex Desktop rolloutは`codex-cli 0.150.0-alpha.8`を記録している。
- 同rolloutの初期contextには約7〜8 KBのSkills catalog、約1 KBのplugin instructions、約3.3 KBのrecommended pluginsがあり、対象task/session中にplugin/app/skill系tool callは観測されなかった。
- current upstream Codex sourceでは`skills.include_instructions`がmodel-visible Skills catalogの自動注入toggleとして存在し、2026-04-20導入済みで実機versionより前の機能である。
- current/upstream issue evidenceでは`recommended_plugins=false`単独はrecommendation blockを止めず、`features.plugins=false`が既知の有効な停止境界である。実機workflowでpluginsを使用していないことを前提に採否する。

## Purpose

親Codex/Solが各turnで再処理する固定Desktop contextのうち、このrepositoryの通常workflowで使わない大きなoptional surfaceを安全に削減し、次のcommentlint dogfoodで実token削減を観測可能にする。

## Contract

- 現在の実機Codex versionと公開sourceで実在・有効性を確認した設定だけを使う。
- 最初の変更は大きく、かつこのworkflowで未使用と実証できたoptional contextへ限定する。
- repository installerが既に管理している`~/.codex/config.toml`境界へ再現可能に統合する。
- user-owned configの無関係なkey/table/commentを破壊しない。
- managed設定を繰り返しinstallしても重複・TOML invalid化しない。
- 次のCodex dogfood bundleで削減後の実際のmodel-visible contextを再監査する。

## Must not

- built-in base instructions全体を差し替えない。
- permissions/environment/sandbox/Guardian判断に必要なcontextをtoken削減だけの理由で消さない。
- AGENTS/managed repository instructionsを先に削らない。
- current Codexで効かないflagを「効くはず」として設定しない。
- unusedである根拠のないapp/automation/runtime surfaceを一括無効化しない。

## Acceptance criteria

- install後configでSkills catalog自動注入が無効になる。
- plugin機能を使わない本workflowではplugin/recommended-plugin contextを無効にし、既存Desktop automation・permissions・environment境界を維持する。
- installerのconfig mergeが既存top-level key、`[skills]`、`[features]`の有無にかかわらずidempotentに成立するtestを持つ。
- repository lint/test/buildとinstaller関連validationが通る。
- 次の実Codex runでbefore evidenceより固定promptがmaterially小さいことをbundleから確認できる状態にする。

## Historical invariants

- 最上位EvalはCodex ReductionとQuality Deltaであり、文字数削減だけを成功条件にしない。
- sandbox/Guardian/lifecycleの安全境界をtokenのために弱めない。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。次のCodex+GLM commentlint dogfoodより先にWeb GPT側で完了する。
