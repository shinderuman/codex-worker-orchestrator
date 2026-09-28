# Task: bounded System-One semantic pathのproduction adoption

## Original instruction

```text
2026-09-28 ユーザー指示: 現在の評価Task群から得たevidenceを使い、少なくとも1つのbounded System-One / Jev-style semantic pathをproduction executionへ導入するTaskをtracked化し、terminalまで実装する。shadow-onlyでは終了しない。個別candidateのGo / No-Goを判定し、不適格なcandidateを無理に採用しない。initial production adoption後は後続TaskをDogfoodとして継続評価できる状態にする。
```

## Amendments

none

## Purpose

評価でGoとしたbounded decision classを通常のproduction executionへ導入し、Codex / Sol実消費の削減とQuality Deltaを測定可能にする。

## External feasibility

status: not-applicable

## Contract

- `system-one-semantic-workload-closure-eval.md`と先行評価のevidenceから、最初のadoption対象となるdecision class / execution pathを1つ明示する。候補が未確定なら推測で実装せず、先行評価の結果を要求する。
- cheap semantic modelのbounded input、structured result、routingをproduction pathへ接続する。deterministicで処理できる判断はdeterministic gateに残す。
- safe / clear caseだけを低cost経路へ送り、ambiguous / high-risk / high-leverage tailをSol Highへ戻す。provider / schema failure、partial output、uncertain resultはfail-openまたはhigher-authority pathへ戻す。
- canonical source proof、reviewer independence、既存安全境界を維持する。
- hit率、fail-open率、false negative / escaped finding、Quality Delta、System-One追加cost、Codex / Sol実消費、review / fix / re-entry削減、latency、human interventionをDogfoodで観測できるようにする。
- rollback可能なbounded rolloutとし、初回adoption後のDogfood結果によるscope拡大・縮小・調整を追跡可能にする。

## Must not

- shadow-onlyの実装で完了しない。
- 個別candidateのNo-GoをSystem-One全体のNo-Goにしない。
- 不適格なcandidateを採用数のためにproductionへ昇格しない。
- reviewer / source proof / safety境界を弱めない。
- unrelatedなNEXTやCI対応へ拡張しない。

## Acceptance criteria

- 1つ以上のbounded semantic execution pathでSystem-Oneがproduction executionに使われる。
- failure時のhigher-authority fallback、Quality DeltaとCodex / Sol削減の測定、rollbackとDogfood継続の手段が確認できる。
- 通常のreview、validation、publicationを経て1 Task = 1 commitでterminalになる。

## Historical invariants

- System-Oneはproduction採用を前提とし、Go / No-Goはdecision class / execution path / production scopeごとに判定する。
- Sol Highには曖昧・高risk・高レバレッジなsemantic tailを残す。

## Dependencies

- `IMPLEMENTATION_TASKS/system-one-semantic-workload-closure-eval.md`
