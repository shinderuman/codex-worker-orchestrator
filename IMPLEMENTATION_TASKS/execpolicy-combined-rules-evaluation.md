# Task: Execpolicy combined-rules evaluation

## Original instruction

````text
https://github.com/shinderuman/codex-worker-orchestrator/pull/345
CodeRabbit, Greptileのレビュー結果を参照して必要なら対応するタスクを作ってくれ
````

## Amendments

none

## Resolved references

- reviewed head: `a595057f5912ece29a99a778c683619612f9b7a5`
- CodeRabbit comment `3930465331`: `https://github.com/shinderuman/codex-worker-orchestrator/pull/345#discussion_r3930465331`
- current `execPolicyAllows`はmanaged rules fileを1件ずつ別processで評価してany-allowを返し、実際の複数rules同時load時のprecedence/conflictを検証しない

## Purpose

managed Codex execpolicyのtestを実運用と同じcombined rule contextで評価し、個別fileのallowを全体policyのallowと誤認しないようにする。

## External feasibility

status: not-applicable

## Contract

- 全managed rules pathをrepeatable `--rules`引数として単一の`codex execpolicy check`へ渡す
- single JSON decisionをparseし、combined contextの最終allow/denyだけをtest判定に使う
- rule orderingをdeterministicに保ち、個別file any-allow fallbackを削除する
- installed rules smokeとsource rules testが同じcombined semanticsを検証するか確認し、重複実行を増やさず不足側だけ補強する

## Must not

- production ruleをtest都合で緩和しない
- deny/error/malformed outputをallowへ縮退しない
- rules fileごとの独立結果からcombined decisionを推測合成しない

## Acceptance criteria

- 複数managed rulesを1 processで評価することをtest helper自身が保証する
- 片方だけがallowするcase、unknown command、malformed/CLI failureがcombined contextで期待どおり判定される
- current installed facadeのclosed grammarとGLM remote-write禁止が退行しない
- relevant test、independent reviewer、Sol review、current snapshot validation、commit/install-smokeを完了する

## Historical invariants

- parent runtime facadeのallowは内部closed grammarを越える権限拡張にしない
- GLM worker/reviewerのGit remote writeは禁止を維持する

## Dependencies

none

## Review findings

````text
Update execPolicyAllows to invoke codex execpolicy check once with all rule paths supplied through repeatable --rules arguments, rather than checking each file separately. Preserve JSON decision parsing and ensure the assertion exercises the single policy context.
````

## Current boundary

PR 345の外部review findingをcurrent HEADへ適応して検証する。
