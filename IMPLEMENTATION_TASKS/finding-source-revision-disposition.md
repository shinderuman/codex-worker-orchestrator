# Task: Findingの旧世代・source revisionを跨ぐ正規処置

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「最終的なFindingはCodexに実装させようと思う」。

## Resolved references

前Auditの同一Task改訂後の古いFindingが処置不能になる事故（#1359 G05、#1383 U03）では一回限りのState修復が必要だった。今回#1392 V03でもsource revisionの継続証拠と処置判定を修正。現行実装の実効性を優先確認する。

## Amendments

none

## Purpose

Task/Attempt・Project・canonical Finding・source importのimmutable revision chain・generation/Transitionに基づいて同じFindingを正規に解決する。

## External feasibility

status: not-applicable

## Contract

Task/Attempt・Project・canonical Finding・source importのimmutable revision chain・generation/Transitionに基づいて同じFindingを正規に解決する。旧未処置を新観測で隠さず、断絶した改訂はfail-closed。

## Must not

旧Findingを無言で削除しない。重複Findingの偽解決や手動State/一時operatorを標準化しない。第二のFinding正本は作らない。

## Acceptance criteria

同Taskの複数source改訂後に古い未処置を合法dispositionし、最終accept/Publicationへ進むE2E。異なるTask・不明snapshot/generationは拒否。現行sourceで既に実証できるなら追加product変更を要求しない。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
