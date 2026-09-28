# Task: System-One semantic workloadの残候補評価

## Original instruction

```text
2026-09-28 Sol decision: duplicate/noise filteringのNo-GoをSystem-One全体のNo-Goへ一般化しない。review feedbackとrepo-search relevanceを含む候補Taskの後、現在Codex/Solが実際に消費しているsemantic workloadを確認し、まだ安全に下位へ移せる合理的なdecision class / execution pathを探索する。Go候補は別Taskへtracked化し、残りはSolに残す理由または不採用理由を明示する。
```

## Amendments

2026-09-28 ユーザー指示（System-One系列の完了条件）:

> System-One自体を採用するか否かは評価対象ではありません。System-One / Jev-style semantic offloadをproduction architectureへ導入することは、当初からSystem-One系列の目的です。
>
> Go / No-Goは個別のdecision class / execution path / production scopeについて判定してください。個別candidateのNo-GoをSystem-One全体のNo-Goへ一般化してはいけません。
>
> 既存candidateがすべて不適格でも、current Codex / Sol semantic workloadから次のbounded candidateを探索してください。少なくとも1つのbounded System-One / Jev-style semantic pathをproduction executionへ導入するTaskをtracked化し、terminalまで実装してください。
>
> production pathでは、deterministic処理可能なものをdeterministic gateへ残し、ambiguous / high-risk / high-leverage tailをSol Highへ残してください。provider / schema failure、partial output、uncertain resultはfail-openまたはhigher-authority pathへ戻してください。canonical source proof、reviewer independence、安全境界を弱めず、Quality DeltaとCodex / Sol実消費削減を測定可能にし、rollback可能にしてください。
>
> initial adoption後のDogfoodでhit率、fail-open率、false negative / escaped finding、Quality Delta、追加cost、Codex / Sol実消費、review / fix / re-entry削減、latency、human interventionを観測し、scopeを拡大・縮小・調整してください。

## Purpose

個別candidateの結果だけでSystem-One探索を終えず、残るCodex/Sol semantic workから次の有効候補を判定する。

## External feasibility

status: not-applicable

## Contract

- current task/bundle/telemetryを用い、Sol/Codexの高頻度semantic workと消費量をdecision class / execution path単位で確認する。
- deterministic、cheap semantic decision、Sol tailのどこへ置けるかを、Quality Delta、false negative、coverage、追加cost、fallbackで評価する。
- Go候補は独立Taskへtracked化し、No-Go候補は理由と再評価境界を示す。
- 既存System-One Taskとduplicateを確認し、まだ合理的な候補が残るなら探索を終えない。
- System-One全体の採否は判定せず、少なくとも1つのboundedなproduction adoption候補を選び、独立Taskへ引き渡す。既存候補がすべてNo-Goなら現行semantic workloadから追加候補を探索する。

## Must not

- 単一candidateのNo-Goや既存Task消化だけを探索終了条件にしない。
- Sol callの完全削除だけをReductionと定義しない。
- 品質を測れない候補をproductionへ自動昇格しない。
- recovery/CI等の別campaignへscopeを広げない。

## Acceptance criteria

- 現行semantic workload、検討したcandidate、Go Task、Sol残留/不採用理由が揃う。
- 追加の合理的候補が残らないか、残る候補がtracked化されている。
- 最初のproduction adoption Taskへ候補、authority、fail-open、Sol tail、Quality Delta・実消費測定、rollback条件を引き渡す。
- Codex ReductionとQuality Deltaの比較可能性を明示し、通常のreview、validation、publicationを経てterminalとなる。

## Historical invariants

- System-One / Jev-style semantic offloadはdecision class単位でGo/No-Goを決める。
- Sol Highは曖昧・高risk・高レバレッジなsemantic tailに集中する。

## Dependencies

none
