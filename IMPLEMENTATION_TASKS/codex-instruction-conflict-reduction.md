# Task: Codex parent wait runtime enforcement

## Original instruction

````text
単純に文字を削るところもあるが、矛盾した指示をしないようにするというのもある
既存で「GLM実行中はGLMプロセスが完了するまでチャットに応答しない」というこのツール独自の指示とCodex Desktopの「60秒以内に応答を返すこと」という矛盾した指示があるらしい
そういうのがあると単純に余計な応答をしてしまう可能性もあるが、それよりも矛盾によってトークン消費量が増えることを懸念している
````

## Amendments

### 2026-09-04

````text
そもそも自由言語でハーネスにしようとしているのが間違いなんじゃねえのか
他に今までやった、これからやるものが自由言語で防ごうとしているものがあるんじゃねえのか
お前にはルールを守る能力がないんだから自由言語で防ぐことは不可能
````

## Resolved references

- 旧taskはinstructionから待機choreographyを除きruntime ownerへ寄せたとして完了したが、current `glm-execution.md`は6時間yield、同一cell、`write_stdin`、途中でparentへ戻らないことを再び長い自由言語で要求している
- harnesslintはinstructionに期待文字列が存在することを検証するが、親Codexが短いwaitや途中returnを選ぶこと自体は拒否しない
- ユーザーは本sessionでも最大blocking waitと無変化liveness禁止を複数回再指示している

## Purpose

長時間GLM処理のwait ownershipを親Codexの自由言語遵守からruntimeへ移し、途中return、短周期poll、liveness turn、重複起動を機械的に防ぐ。

## External feasibility

status: observation
assumption: parent action processとCodex tool sessionの範囲でterminal/attentionまでのblocking ownershipを固定できる

## Contract

- parent actionがterminal、user/Sol attention、rate/provider stopまでblocking ownerを維持し、通常経路でpoll cadenceを親modelへ選ばせない
- tool session detach時だけcanonical handoff/watch recoveryへ遷移し、同一task/session再利用をstateで強制する
- wait policy違反をtelemetry観測だけでなく、可能な範囲でcommand admission/state leaseにより拒否する
- immutable Desktop側制約はrepository runtimeで強制可能な部分と分離する

## Must not

- instruction文字列presence testだけで完了扱いしない
- heartbeat model call、短時間status loop、無変化commentaryを導入しない
- user status質問への応答を禁止しない
- healthy in-flight処理を再起動しない

## Acceptance criteria

- 親が短いyield/途中detachをしても同一ownerがterminalまで継続するproduction scenarioがある
- 無変化だけではparent model turnが増えず、terminal/attention/user inputだけが返却境界になる
- session loss時は重複起動せずhandoffへ収束する
- instructionからruntimeで決定済みの手続きproseを削減できる
- 独立reviewer、Sol semantic review、current snapshot validationを完了する

## Historical invariants

- user interruptionは即時処理する
- 同じtaskを新規sessionで再起動しない

## Dependencies

- `IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`

## Review findings

- 過去taskの目的に反してwait choreographyが再び親instructionへ戻っている

## Current boundary

旧taskをfalse-complete/regressionとして再開し、audit後にruntime ownershipを確定する。
