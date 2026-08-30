# Task: Codex親instructionの矛盾costを削減する

## Original instruction

````text
単純に文字を削るところもあるが、矛盾した指示をしないようにするというのもある
既存で「GLM実行中はGLMプロセスが完了するまでチャットに応答しない」というこのツール独自の指示とCodex Desktopの「60秒以内に応答を返すこと」という矛盾した指示があるらしい
そういうのがあると単純に余計な応答をしてしまう可能性もあるが、それよりも矛盾によってトークン消費量が増えることを懸念している
````

## Amendments

````text
CommentlintとBundle Diff以外の実装をお前が全部終えてその次にCommentlintをやらせて観測するつもり
````

## Resolved references

- 実機`codex-cli 0.150.0-alpha.8`のbuiltin base instructionsには「ongoing work中60秒以上commentary updateを空けない」と「60秒超のblocking sleep/waitを避ける」が実在する。
- repository側`glm-execution.md`は、健康な長時間GLM処理ではperiodic model re-entry/livenessを避け、`glm-parent-action`の主呼出を長時間blockingさせる契約を持つ。
- 直前のruntime改善はreal dogfoodで成功し、約67.7分と約18.3分の待機が各1 parent tool callで完了した。したがって目的はruntime waitを再実装することではなく、model-visible conflict/reasoning pressureを減らすこと。

## External feasibility

status: not-applicable

## Purpose

親Codexが同一turnで相反するliveness/wait proseを解決するための不要なreasoningと将来のperiodic re-entry regression pressureを減らし、既に成立した長時間blocking behaviorを維持する。

## Contract

- exact current builtin/repository instructionを一次証拠として扱う。
- immutable builtinの一部だけを消すためにbase instructions全置換をしない。
- repository側で機械境界へ移せる待機契約はproseからcommand semanticsへ寄せ、parentがtiming strategyを選ぶ必要を減らす。
- 既存`glm-parent-action`/`glm-worker`のterminal transition、user interruption、rate/provider stop、decision/review boundaryは維持する。
- configurableな重複/矛盾だけを削り、必要なら「Desktop側はimmutable」という結論を明示する。

## Must not

- 60秒ルールへ対抗する新しい長大なprecedence説明を追加しない。
- timer polling、heartbeat model call、短時間status loopを復活させない。
- userが明示的に進捗を求めた場合の応答まで禁止しない。
- model instructions fileによるbuiltin全置換を行わない。

## Acceptance criteria

- repository-managed wait instructionsから、tool側で既に決定済みのtiming/liveness policyを親に再判断させる重複proseがmaterially減る。
- long-running`glm-parent-action`が引き続き同一tool call内でterminal/attention状態まで待てる。
- 次のcommentlint dogfoodで60秒由来のperiodic model re-entry/livenessが再発しない。
- immutable Desktop builtin conflictが残る場合、その残存境界を明示して余計なhackを追加しない。

## Historical invariants

- real dogfoodで成立したlong-blocking runtime behaviorを維持する。
- Quality Deltaを悪化させるprompt削減を行わない。

## Dependencies

none

## Review findings

none

## Current boundary

Repository-managed instruction reductionはPR #171 / main `a96c0df65a5cb560043879e4742199fd35010253`でintegration済み。重複`glm-wait.md`を廃止し、canonical `glm-execution.md`の待機節から6時間・最大wait・`write_stdin`・`functions.wait`等の親timing choreographyを除去し、通常待機のtiming/heartbeat/poll cadenceをtool/runtime ownerへ戻した。explicit user status、terminal transition、失われたtool sessionの`--handoff`/bounded `--watch` recoveryは維持し、repository lint・harnesslint・full Go test/vet/build・offline install smokeを通過済み。残りはimmutable Desktop builtinとの実機組合せを次のfresh-thread commentlint dogfoodで観測し、60秒由来のperiodic model re-entry/livenessが再発しないこととQuality Deltaを確認することだけ。そこまでIssue #162と本taskは未完了のまま保持し、evidenceが実装欠陥を示さない限りproduction instructionへ再突入しない。
