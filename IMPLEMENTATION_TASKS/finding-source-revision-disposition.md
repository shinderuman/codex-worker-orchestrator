# Task: Findingの旧世代・source revisionを跨ぐ正規処置

## Original instruction

2026-10-25 ユーザー指示:「FindingをIMPLEMENTATION_PLANに分割する」「最終的なFindingはCodexに実装させようと思う」。

## Resolved references

前Auditの同一Task改訂後の古いFindingが処置不能になる事故（#1359 G05、#1383 U03）では一回限りのState修復が必要だった。今回#1392 V03でもsource revisionの継続証拠と処置判定を修正。現行実装の実効性を優先確認する。

## Amendments

### 2026-10-25 Task要求同期後の未処置Finding拒否（親Codexの確認）

````text
External Review採否Taskで、未処置Findingを残したまま同じTaskの追加要求を正規sync-task-authorityで同期した後、既存Ownerへのindependent-nonblocking処置が「finding source revision lacks its finalized same-attempt transition」で拒否された。
findingSourceRevisionTransitionはIMPORT_PARENT_SOURCEだけを認め、SYNC_TASK_AUTHORITYを扱わない。一方、validateUnresolvedAttemptFindingsは未処置Findingがある候補のacceptを拒否する。同一Attemptの正規Task同期をimmutableなsource revision/transitionで検証し、旧Findingを削除・再観測で隠さず処置できるようにする。
任意のtransition kindを許すだけの修正はしない。Task同期自身のfinalized、同一Attempt、旧新Task/Project、generation、元のworkspace snapshotを独立に検証し、別Task・偽source・未確定遷移は拒否する。現行経路で既に成立する部分は再実装しない。
````


### 2026-11-01 External Review #1401の補完要求

親Codexは次の2件を本Taskへ束縛し、External Review照合Taskで重複実装しないと判断した。

- CR-1401-2: https://github.com/shinderuman/codex-worker-orchestrator/pull/1401#discussion_r4381578250 。原文: **Accept mutation steps at the finding's source generation.** A live finding can be recorded at generation G while a bound model call is active. The later `RecordAdmittedMutation` then writes a mutation from G to G+1. The continuity traversal reaches G, but `findingAuthorityProgressionAnchors` rejects the mutation because `step.fromTransition` is false. The finding can then be reported as unreachable. Add a mutation-specific anchor rule. Require the validated mutation step to match the finding's task and workspace, and compare its project with the previously matched authority. Preserve the existing mutation identity, lease, snapshot, and consecutive-generation checks.
- CR-1401-3: https://github.com/shinderuman/codex-worker-orchestrator/pull/1401#discussion_r4381578261 。原文: **Ignore temporary and non-JSON entries during progression scans.** `ResolveFinding` holds the mutation lock during resolution, so the claimed concurrent partial-file read is not established. However, an interrupted `writeJSONAtomic` can leave a `.controller-*.tmp` file, and both scans attempt to decode every directory entry. That stale file can make finding resolution fail. Filter each entry to regular `.json` files before calling `readJSON`. Keep validation for JSON records.

受理済みmutationでsource generationへ到達する正caseと、異なるTask/workspace/project・不連続generation・無効lease/snapshotの拒否caseを検証する。scanは通常の.json fileだけを対象とし、temporary/non-JSON entryは無視して、正規JSON recordのdecode/validation failureは隠さない。

## Purpose

Task/Attempt・Project・canonical Finding・source importのimmutable revision chain・generation/Transitionに基づいて同じFindingを正規に解決する。

## External feasibility

status: not-applicable

## Contract

Task/Attempt・Project・canonical Finding・source importのimmutable revision chain・generation/Transitionに基づいて同じFindingを正規に解決する。旧未処置を新観測で隠さず、断絶した改訂はfail-closed。

同一Attemptの `sync-task-authority` を跨ぐFinding継続も対象とし、IMPORT_PARENT_SOURCEとそれぞれの遷移contractを検証する。未処置Findingの解決からacceptへ進む経路を正規操作で成立させる。

## Must not

旧Findingを無言で削除しない。重複Findingの偽解決や手動State/一時operatorを標準化しない。第二のFinding正本は作らない。

## Acceptance criteria

同Taskの複数source改訂後に古い未処置を合法dispositionし、最終accept/Publicationへ進むE2E。異なるTask・不明snapshot/generationは拒否。現行sourceで既に実証できるなら追加product変更を要求しない。

Task同期前のFindingを同期後に同じ既存Ownerへ処置してaccept可能となるpositive caseと、別Task・snapshot/generation/transition不一致を拒否するnegative caseを独立に検証する。

## Historical invariants

- Current Git / `IMPLEMENTATION_RULES.md` / Plan / 当該Taskを正とする。管理Issueを実装要求の第二正本にしない。
- 正規GLM実装、Codexのsemantic判断、独立Review、Guard、immutable source evidence、1 Task 1 Commitを維持する。
- 実証済みの同一Ownerを二重実装しない。不要な後方互換性・第二state・専用operatorを増やさない。

## Dependencies

none
