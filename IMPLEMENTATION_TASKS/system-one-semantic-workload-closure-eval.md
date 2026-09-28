# Task: System-One semantic workloadの残候補評価

## Original instruction

```text
2026-09-28 Sol decision: duplicate/noise filteringのNo-GoをSystem-One全体のNo-Goへ一般化しない。review feedbackとrepo-search relevanceを含む候補Taskの後、現在Codex/Solが実際に消費しているsemantic workloadを確認し、まだ安全に下位へ移せる合理的なdecision class / execution pathを探索する。Go候補は別Taskへtracked化し、残りはSolに残す理由または不採用理由を明示する。
```

## Amendments

none

## Purpose

個別candidateの結果だけでSystem-One探索を終えず、残るCodex/Sol semantic workから次の有効候補を判定する。

## External feasibility

status: not-applicable

## Contract

- current task/bundle/telemetryを用い、Sol/Codexの高頻度semantic workと消費量をdecision class / execution path単位で確認する。
- deterministic、cheap semantic decision、Sol tailのどこへ置けるかを、Quality Delta、false negative、coverage、追加cost、fallbackで評価する。
- Go候補は独立Taskへtracked化し、No-Go候補は理由と再評価境界を示す。
- 既存System-One Taskとduplicateを確認し、まだ合理的な候補が残るなら探索を終えない。

## Must not

- 単一candidateのNo-Goや既存Task消化だけを探索終了条件にしない。
- Sol callの完全削除だけをReductionと定義しない。
- 品質を測れない候補をproductionへ自動昇格しない。
- recovery/CI等の別campaignへscopeを広げない。

## Acceptance criteria

- 現行semantic workload、検討したcandidate、Go Task、Sol残留/不採用理由が揃う。
- 追加の合理的候補が残らないか、残る候補がtracked化されている。
- Codex ReductionとQuality Deltaの比較可能性を明示し、通常のreview、validation、publicationを経てterminalとなる。

## Historical invariants

- System-One / Jev-style semantic offloadはdecision class単位でGo/No-Goを決める。
- Sol Highは曖昧・高risk・高レバレッジなsemantic tailに集中する。

## Dependencies

none
