# Task: System-One production adoption closure gate

## Original instruction

```text
2026-09-28 ユーザー指示: 現在の評価Task群から得たevidenceを使い、少なくとも1つのbounded System-One / Jev-style semantic pathをproduction executionへ導入するTaskをtracked化し、terminalまで実装する。shadow-onlyでは終了しない。個別candidateのGo / No-Goを判定し、不適格なcandidateを無理に採用しない。initial production adoption後は後続TaskをDogfoodとして継続評価できる状態にする。
```

## Amendments

2026-09-29 ユーザー指示（役割変更）: 本Taskは複数candidateのimplementationを所有しない。closure evalでGoとなったcandidateごとのbounded adoption Taskがすべてterminalになった後に、未実装Goが残らないこと、各pathの通常execution・fallback・rollback・Dogfood測定を確認する全体closure gateとする。System-One系列終了後の通常Taskをproduction Dogfood cohortとして扱う。

## Purpose

個別Taskでproduction導入したすべてのSystem-One Go candidateについて、系列完了条件と通常TaskでのDogfood継続条件を確認する。

## External feasibility

status: not-applicable

## Contract

- closure evalの分類と、Go候補ごとの独立Taskのterminal / Git / runtime evidenceを突合する。各Go候補はproduction adoption済み、または後続evidenceによって明示的にNo-Goへ変更済みでなければならない。
- 未実装のGo候補と合理的な未分類候補が残っていないことを確認する。
- deterministic / System-One / Sol Highのrouting境界、bounded input / structured output、通常executionでの実使用、provider / schema failure・partial output・uncertain result時のhigher-authority fallback、rollbackを各production pathで確認する。
- shadow-only / trial-onlyの経路をproduction adoption済みとして数えず、canonical source proof、reviewer independence、安全境界の維持を確認する。
- 通常TaskをDogfood cohortとして使い、hit率、fail-open率、false negative / escaped finding、Quality Delta、System-One追加cost、Codex / Sol実消費、review / fix / re-entry削減、latency、human interventionを継続測定できることを確認する。scope拡大・縮小はそのevidenceで判断する。

## Must not

- 本Taskへ別decision classのproduction実装を混ぜない。未実装Goを残して完了しない。
- shadow-only / trial-onlyの経路をproduction adoption済みとして数えない。
- 個別candidateのNo-GoをSystem-One全体のNo-Goにしない。
- 不適格なcandidateを採用数のためにproductionへ昇格しない。
- reviewer / source proof / safety境界を弱めない。
- unrelatedなNEXTやCI対応へ拡張しない。

## Acceptance criteria

- closure evalの全Go候補について、個別adoption Taskがterminal済みか、後続evidenceにより明示No-Goへ変更済みであり、未処理Goがない。
- production pathが通常executionで実際に使用され、deterministic / System-One / Sol High境界とfailure時のhigher-authority fallback、rollbackが確認できる。
- Quality DeltaとCodex / Sol実消費削減を含むDogfood測定を、系列完了後の通常Taskで継続できる。
- 通常のreview、validation、publicationを経て1 Task = 1 commitでterminalになる。

## Historical invariants

- System-Oneはproduction採用を前提とし、Go / No-Goはdecision class / execution path / production scopeごとに判定する。
- Sol Highには曖昧・高risk・高レバレッジなsemantic tailを残す。

## Dependencies

none

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/system-one-semantic-workload-closure-eval.md`
- `IMPLEMENTATION_TASKS/system-one-failure-path-advisory-adoption.md`
