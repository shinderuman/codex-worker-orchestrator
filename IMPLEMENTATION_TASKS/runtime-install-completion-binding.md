# Task: Runtime install completion binding

## Original instruction

````text
そもそも自由言語でハーネスにしようとしているのが間違いなんじゃねえのか
他に今までやった、これからやるものが自由言語で防ごうとしているものがあるんじゃねえのか
お前にはルールを守る能力がないんだから自由言語で防ぐことは不可能
````

## Amendments

none

## Resolved references

- Rulesはruntime影響taskのcommit後に`install.sh`本配置、source/installed一致、production smokeを要求する
- `--install-smoke`は実行結果をstructured validationとして保存できるが、task completion/finalize-checkがruntime影響判定とcurrent HEAD対応install evidenceを必須にする機械状態は確認できない
- 過去`023-installed-state-verification.md`とinstall smoke関連taskは個別検証を導入したが、親が実行を忘れることをinstruction以外で拒否する残存境界を監査する必要がある

## Purpose

runtime変更をsourceだけで完了扱いし、installed binary/instructionが古いまま後続dogfoodへ進むことをmachine completion gateで防ぐ。

## External feasibility

status: not-applicable

## Contract

- current task diffからruntime install対象かをbounded manifest/categoryで確定し、親の自由判断だけにしない
- runtime対象ではcurrent HEAD/source digestに束縛されたinstall、installed一致、必要smoke evidenceが揃うまでcompletionを許可しない
- metadata-only taskはinstall不要を機械的に区別する
- install失敗evidenceと再実行は既存structured pathを再利用する

## Must not

- 全taskへ無条件installを課さない
- review/commit前またはdirty implementationを本配置しない
- instructionにinstall手順を追記するだけで完了扱いしない
- stale install-smoke recordをcurrent HEAD証拠として再利用しない

## Acceptance criteria

- runtime変更・metadata-only・mixed diff・stale evidence・install failureのscenarioがある
- runtime変更はcurrent source/installed digestとsmokeが揃うまでfinal completionがfail closedする
- metadata-onlyは不要なinstallを発生させない
- install loopやCodex turnを増やさず既存validation recordへ統合する
- 独立reviewer、Sol semantic review、current snapshot validation、実install smokeを完了する

## Historical invariants

- runtime改善を未配置のまま後続実運用へ進めない
- installは親Codex authorityでありGLMへ実行権限を移さない

## Dependencies

- `IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`

## Review findings

none

## Current boundary

auditでcurrent completion stateとinstall validation bindingの実装gapを確定してから着手する。
