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
履歴が見つからなかった場合の話だがAcceptanceに限らず、どういう状態のファイルなのかわからないため全体的にそのまま作業するべきではないと思う
前段としてそのタスクのGo/No-Goを決めてから作業するべきだと思う
````

````text
Codexの無駄はこれから全然削っていくところだと思っている
つまりCodexになにかタスクを与えて動かしてなにか無駄がないか確認するタスクが常に必要
今回のBundleを出させるタスクもその1つだ
````

````text
CommentlintとBundle Diff以外の実装をお前が全部終えてその次にCommentlintをやらせて観測するつもり
````

````text
Codex-Worker-Orchestratorで作業するときだけ注入しない、では不十分。このツールを使って実装するtarget repositoryでCodex tokenを節約できることが目的。全体共通でよい設定と、このorchestratorを使う作業時だけ有効にしたい設定は分離する。必要ならenable/disable script等の明示切替も許容する。
````

## Resolved references

- 実機Codex Desktop rolloutは`codex-cli 0.150.0-alpha.8`を記録している。
- 同rolloutの初期contextには約7〜8 KBのSkills catalog、約1 KBのplugin instructions、約3.3 KBのrecommended pluginsがあり、対象task/session中にplugin/app/skill系tool callは観測されなかった。
- current upstream Codex sourceでは`skills.include_instructions`がmodel-visible Skills catalogの自動注入toggleとして存在し、2026-04-20導入済みで実機versionより前の機能である。
- current/upstream issue evidenceでは`recommended_plugins=false`単独はrecommendation blockを止めず、`features.plugins=false`が既知の有効な停止境界である。実機workflowでpluginsを使用していないことを前提に採否する。
- current Codex config loaderにはcwd/trustに基づくproject-local `.codex/config.toml` layerがあり、user/global configより高い優先順位でtarget repository単位のoverrideが可能である。
- thread/session開始時にconfigを解決・refreshする経路があるため、prompt削減設定は新規threadでの反映を基本とし、Desktop全体再起動は実機で必要性が確認された場合だけ要求する。

## External feasibility

status: not-applicable

## Purpose

親Codex/Solが各turnで再処理する固定Desktop contextのうち、**glm-workerを使って実装するtarget repositoryの通常workflowで不要な大きなoptional surface**を安全に削減し、次のcommentlint dogfoodで実token削減を観測可能にする。orchestrator自身のsource repositoryだけを軽くして完了とはしない。

## Contract

- 現在の実機Codex versionと公開sourceで実在・有効性を確認した設定だけを使う。
- 最初の変更は大きく、かつorchestrated workflowで未使用と実証できたoptional contextへ限定する。
- `background_terminal_max_timeout`のように全Codex作業で共通利益がある既存設定と、Skills/Plugins等のorchestrated workflow専用token削減設定を分離する。
- token削減profileは**glm-workerを使うtarget repositoryへ適用できること**を必須とする。
- 第一候補はtarget repositoryのCodex project-local `.codex/config.toml`。ただしtarget worktree汚染、既存project configとの衝突、GLMによる改変可能性などを評価し、運用上不適切なら明示的なenable/disable scriptまたは同等のbounded profile切替を採用する。
- profile切替方式を採る場合、unrelated repositoryの通常Codex Skills/Plugins利用を壊さず、元設定へdeterministicに復元できること。
- user-owned/global configの無関係なkey/table/commentを破壊しない。
- project-local configを使う場合も既存target repo設定を上書き・消失させない。
- 設定適用後は新規Codex threadを開始してrendered promptを比較する。Desktop再起動が必要かは実機で確認し、必要性がなければ要求しない。
- 次のCodex dogfood bundleで削減後の実際のmodel-visible contextを再監査する。

## Must not

- orchestrator source repositoryでだけ効く設定を入れて「このツール利用時のtoken削減」とみなさない。
- built-in base instructions全体を差し替えない。
- permissions/environment/sandbox/Guardian判断に必要なcontextをtoken削減だけの理由で消さない。
- AGENTS/managed repository instructionsを先に削らない。
- current Codexで効かないflagを「効くはず」として設定しない。
- unusedである根拠のないapp/automation/runtime surfaceを一括無効化しない。
- global configを書き換えたまま、orchestratorを使わないrepositoryの通常Codex機能まで黙って無効化しない。

## Acceptance criteria

- glm-workerを使う任意のtarget repositoryで、Skills catalog自動注入を無効にできる再現可能な適用境界を持つ。
- plugin機能を使わないorchestrated workflowではplugin/recommended-plugin contextを無効にでき、既存Desktop automation・permissions・environment境界を維持する。
- unrelated repositoryでは通常のSkills/Plugins挙動を維持できる。
- project-local config方式を採る場合、既存`.codex/config.toml`とのmerge/共存とrepository cleanliness/authorityを検証する。profile切替方式ならenable/disableのidempotenceと元設定復元を検証する。
- 新規threadで設定反映を確認し、Desktop再起動の必要/不要を実機証拠として残す。
- repository lint/test/buildと関連validationが通る。
- 次の実Codex runでbefore evidenceより固定promptがmaterially小さいことをbundleから確認できる状態にする。

## Historical invariants

- 最上位EvalはCodex ReductionとQuality Deltaであり、文字数削減だけを成功条件にしない。
- sandbox/Guardian/lifecycleの安全境界をtokenのために弱めない。
- このツールの価値はorchestrator自身を開発するときではなく、target repositoryでCodex+GLM作業をするときのCodex Reductionとして成立する必要がある。

## Dependencies

- `IMPLEMENTATION_TASKS/unscheduled-task-state-reconciliation.md`

## Review findings

none

## Current boundary

Production implementationはPR #169 / main `04b10110ab1b5793f0cf392c22557dbb37ca8402`でintegration済み。`glm-codex-context enable|disable|status`、target-repository local profile、Git cleanliness/conflict fail-closed、unit/lint/build/offline install smokeまで確認済み。残りは実機evidenceだけであり、他のpre-dogfood改善を完了した後、commentlintをfresh Codex threadで実行してbundleから設定反映、fixed-context reduction、Desktop restart要否、sandbox/Guardian/lifecycle維持、Quality Deltaを確認する。そこまで本taskとIssue #161は未完了のまま保持し、evidenceが実装欠陥を示さない限りproduction implementationへ再突入しない。
