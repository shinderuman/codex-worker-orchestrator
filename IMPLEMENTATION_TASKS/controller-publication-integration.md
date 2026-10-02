# Task: controller所有の公開・統合と外部更新

## Original instruction

````text
PRを確認してくれ
K1~K7のタスクがある
K1,2,4は作業済みもしくは作業中だ
それら以外をお前にやらせたい
まずは状況を確認しろ
なお今回の作業はGLMは使わない
````

### 解決済み対象の要求原文

Parent design authority: #1215
Common rules: #25
Depends on: #1216, #1217, #1218, #1188

## Purpose

Implement K5 from #1215. The original #1190 remote-main advancement defect is now one acceptance case of a broader typed publication/integration subsystem; do not build a parallel recovery path.

## Scope

Own:

- accepted candidate lifecycle bound to exact attempt/Task/episode/controller generation;
- unpublished candidate rebind/regeneration after blocker integration or legitimate external advancement;
- remote observation as the immutable-history boundary;
- typed expected-old local ref transitions;
- blocker publication/integration before blocked parent resume;
- configured publication-remote observation and race classification;
- `ADOPT_EXTERNAL_ADVANCEMENT` for ordinary concurrent upstream advancement;
- immutable remotely observed prefix + additive descendant history;
- controller-proven descendant lineage for later root completion;
- candidate/review/validation/install evidence invalidation and required re-entry after rebind;
- no force-push/history guessing.

## Publication rules

- prepared candidate: supersedable by typed rebind;
- locally promoted but not remotely observed: still supersedable after proving remote non-observation; preserve old candidate as evidence;
- remotely observed candidate/prefix: immutable, never rebase/rewrite away;
- blocker work after observed prefix is additive descendant history;
- remote descendant that already contains published candidate is adopted as current base;
- remote movement that does not contain an immutable observed prefix fails closed;
- terminal complete ends blocker interruption eligibility.

## Blocker integration rule

A semantic blocker Task fulfills its dependency only after its accepted result is canonically published/integrated according to repository policy. Then the suspended parent is rebound onto the new integration tip. If parent restore later fails, valid blocker publication is retained.

## External advancement

Conflict-free ordinary upstream advancement is not a repair Task. Use typed controller rebind; conflict/ownership ambiguity or history rewrite is a preserved fail-closed boundary.

## Acceptance

Cover #1215 matrix scenarios for external advance during mutable/suspended work, prepared/local-only candidate rebind, remote-observed blocker, remote races containing/not containing candidate, immutable-prefix rewrite, blocker publication followed by parent restore failure, and no force push.

Current expected-old `git update-ref` and exact candidate/snapshot identities are reusable primitives, not compatibility constraints.

Architecture checkpoints: #1215 comments `5911271743`, `5911518029`, `5911566896`, `5911602795`, `5911667173`.

This Issue supersedes the old #1190 formulation rather than layering another rebase/recover command beside the clean controller.

## Implementation checklist

- [ ] 1. Define the accepted-candidate publication state model under controller/attempt/Task/episode authority.
  - Completion: prepared, locally promoted/unobserved and remotely observed states are explicit and snapshot-bound.
- [ ] 2. Implement typed expected-old local ref transitions and publication-remote observation.
  - Completion: races are classified from exact old/new/remote state; no history guessing or force push is possible.
- [ ] 3. Implement unpublished candidate rebind/regeneration after blocker integration or legitimate external advancement.
  - Completion: superseded candidates remain evidence while review/validation/install evidence is invalidated and re-entered as required.
- [ ] 4. Enforce remote observation as the immutable-history boundary.
  - Completion: remotely observed prefix cannot be rebased/rewritten away; later accepted work is additive descendant history.
- [ ] 5. Implement blocker publication/integration before dependency fulfillment and parent resume.
  - Completion: a blocker is not fulfilled by lane/review completion alone, and valid blocker publication survives later parent-restore failure.
- [ ] 6. Implement `ADOPT_EXTERNAL_ADVANCEMENT` for ordinary compatible upstream movement.
  - Completion: descendant advancement is adopted through typed controller rebind; conflicting/ambiguous/rewrite movement fails closed without creating a repair Task.
- [ ] 7. Prove publication/external-advancement race scenarios and perform final validation/review.
  - Cover mutable/suspended external advancement, prepared/local-only candidate rebind, remote-observed blocker, containing/non-containing remote races, immutable-prefix rewrite, no-force-push, repository lint, relevant tests and whole-diff review.

### controller遷移・公開競合の確定設計原文

## Design checkpoint — full transition/CAS model and external-publication races

This checkpoint closes the two open Git integration / crash-recovery design items and **supersedes one earlier publication detail**.

### Correction: remote observation, not local promotion, is the immutable-history boundary

Earlier migrated design treated successful local promotion as immutable together with remote publication. Current publication mechanics and the #1190 remote-advancement failure show that this is unnecessarily restrictive.

Best clean-sheet rule:

- prepared candidate: replaceable under typed transition;
- locally promoted candidate that is **not yet observed on the configured publication remote**: still replaceable/rebindable under controller CAS; preserve the old candidate as superseded evidence;
- candidate/prefix positively observed on the publication remote: immutable history; never rewrite/rebase it away;
- terminal Task completion remains terminal for blocker interruption.

This lets a blocker discovered after local promotion but before push be inserted correctly before the root candidate by reverting/replacing only controller-owned unpublished local history. Once remotely observed, blocker work must be additive descendants.

---

## 1. One generic transition protocol

Every mutating controller transition uses one write-ahead protocol under the repository-wide controller lock.

### PREPARE

From controller head generation `G`, validate all semantic/Git/workspace/project preconditions and persist immutable `TransitionRecord T` containing exact expected-old and planned expected-new identities.

CAS the controller head to a pending-transaction revision. No external side effect is allowed before this durable reservation exists.

### APPLY

Perform typed external effects one by one. Each effect is recoverable only by exact classification:

```text
actual == expected-old  -> effect not yet applied; apply/retry
actual == expected-new  -> effect already applied; continue idempotently
otherwise               -> unexpected; fail closed
```

The transition may record effect progress, but correctness may not depend on the progress bit because a crash can occur after an effect and before its bookkeeping write.

### COMMIT

After all target postconditions hold, CAS the controller semantic authority to the planned target generation/revision/attempt/lease/project state. A successfully committed target is never rolled back merely because later cleanup fails.

### FINALIZE

Destructive cleanup of superseded lane/snapshot/ref/temp resources occurs only after semantic commit and required AttemptSeal/index durability. If cleanup fails, keep a typed pending-finalization transition and block conflicting reuse. Retry cleanup; do not roll the semantic transition backward.

Some pure metadata transitions have no external APPLY/FINALIZE and commit atomically. Some lane-materialization transitions can COMMIT the new lease as their final operation after all materialization postconditions hold.

There is never more than one repository-wide pending mutating transition.

---

## 2. Canonical transition table

### A. `DISPOSE_FINDING_NONBLOCKING`

Purpose: same-task / independent-nonblocking / duplicate finding disposition without execution switch.

Preconditions: exact source attempt/snapshot/controller generation; live lease if same-task; no conflicting pending transition.

Effects: persist finding disposition; bind/reuse semantic Task; if repository Task/Plan metadata changes, produce the exact new `ProjectSnapshot` through controller-owned metadata transition.

Postcondition: current execution lease remains unchanged for nonblocking; same-task correction remains bound to the same attempt correction surface.

Crash recovery: controller-only CAS/idempotent metadata update.

### B. `OPEN_BLOCKER_EPISODE` / `REPLAN_BLOCKER_EPISODE`

The same transaction shape is used whether the source is root A or an already-running blocker B.

Preconditions:

- exact independent-blocking FindingRecord;
- machine-proven quiescent source attempt;
- no in-flight mutating call/publication/install transaction;
- source lease/current workspace/base match exact authority;
- target semantic Task is bound/reused and dependency addition is cycle-safe;
- suspension snapshot can be captured losslessly.

APPLY/precommit artifacts:

1. create retained suspension trees + manifest;
2. create/update immutable AttemptSeal state needed before destructive reuse;
3. produce new semantic project snapshot/dependency graph;
4. create episode or next episode revision;
5. revoke the source execution lease.

COMMIT: source attempt becomes `suspended-for-blocker`; controller points to the new episode revision with **no live mutating lease**.

FINALIZE: only disposable source-lane cleanup if applicable. Root primary workspace may remain physically present but is non-authoritative while suspended.

Crash rule: if lease revocation/suspension cannot be proven, no blocker attempt is minted. Never create a recovery context.

### C. `MATERIALIZE_EPISODE_ATTEMPT`

Purpose: start the exact scheduler-selected fresh/resumed Task.

Preconditions: active episode revision, no live lease, scheduler-selected Task inside episode closure, exact integration tip/project snapshot.

For resumed Task, preflight two-stage suspension rebind to the current integration tip first.

APPLY: create/recycle the one detached lane, materialize exact index/worktree state, verify common-dir/git-dir/HEAD/workspace identity, create fresh AttemptRecord.

COMMIT: mint the sole execution lease bound to workspace/attempt/episode revision/controller generation.

A stale worktree at the same path cannot satisfy the postcondition because workspace/git-dir/lease identities differ.

### D. `ACCEPT_EXECUTION_ATTEMPT`

Purpose: move one blocker/root attempt from mutable execution to accepted publication candidate.

Preconditions: exact live attempt/lease, review/validation policy satisfied for its current snapshot, no unresolved blocking finding.

APPLY: create immutable Task-owned candidate commit/object from exact Task-owned delta against the current controller integration base; bind candidate/evidence to exact snapshot.

COMMIT: revoke mutating execution lease and move the attempt to accepted/publication state. Physical lane may remain for publication diagnostics but has no model mutation authority.

If candidate is not remotely observed it remains supersedable through typed rebind.

### E. `REBIND_UNPUBLISHED_CANDIDATE`

Purpose: handle legitimate new base before remote publication — blocker integration, external remote-main advancement, or another typed controller base advance.

Preconditions: candidate is **not remotely observed**; exact old base/candidate/project snapshot; new base is an admitted descendant/advancement; no live mutating call.

APPLY: three-way rebind Task-owned source/candidate to the new base; invalidate stale candidate-bound review/validation/install evidence; generate new candidate identity; if local branch had been promoted to old candidate, CAS it away only after proving remote has not observed the old candidate.

COMMIT: supersede old candidate/evidence by immutable lineage record and bind new candidate/base.

No force-push and no rewrite of remotely observed history.

### F. `PUBLISH_ACCEPTED_TASK`

This is used by an episode blocker Task as well as the root Task. A blocker is considered fulfilled for scheduler purposes only after its accepted commit is canonically published/integrated according to repository policy; this matches raw 606 -> F11 where F11 becomes the new base before 606 resumes.

Preconditions: exact accepted candidate, publication remote readable, expected remote OID known, candidate ancestry is valid, no conflicting controller transition.

APPLY:

1. local branch-ref update if needed by repository publication policy, always expected-old CAS;
2. push candidate to configured remote using normal fast-forward semantics;
3. re-read remote OID.

Crash/recovery classifications:

- remote == expected-old: push not applied -> retry;
- remote == candidate: push applied -> continue;
- remote is descendant of candidate: candidate was published and an external advancement followed -> publication succeeded; feed descendant into `ADOPT_EXTERNAL_ADVANCEMENT` before next resume;
- remote moved to a different descendant of expected-old that does not contain candidate: push raced/lost -> do not force; classify as external advancement and rebind if candidate remained unpublished;
- remote not descendant of an already-remotely-observed immutable prefix: fail closed/manual history-authority boundary.

COMMIT: Task publication is durable; semantic blocker becomes fulfilled/terminal as appropriate; episode integration tip advances to the observed remote/canonical tip.

A later root restore failure **does not roll back this published blocker**.

### G. `ADOPT_EXTERNAL_ADVANCEMENT`

Purpose: ordinary concurrent configured-upstream advancement, not a repair Task.

Preconditions: remote state readable and exact; advancement respects the current immutable published prefix.

Cases:

1. **No remotely observed local candidate/prefix to preserve**: conflict-free rebind current suspended/accepted Task state onto new remote tip; supersede any unpublished candidate and reacquire affected evidence.
2. **Current immutable prefix is remotely observed and remote tip descends it**: adopt the remote descendant as new controller integration base; never rewrite the published prefix; rebind suspended future work onto the new tip.
3. **Remote does not descend the immutable observed prefix / remote history was rewritten**: fail closed. No force push, no guessed rebase.
4. **Content/ownership rebind conflict**: fail closed with old/new evidence retained.

This is the generic clean-sheet mechanism underlying the #1190 class; it is not blocker-specific, but blocker transitions consume the same primitive.

### H. `RESUME_SUSPENDED_ATTEMPT`

Scheduler determines that a suspended Task's blocker dependencies are fulfilled.

Preconditions: exact suspended snapshot, all required semantic dependencies fulfilled, no live lease, exact current integration tip/project snapshot.

APPLY: preflight two-stage rebind; materialize fresh lane/current workspace; create successor AttemptRecord linked to the suspension seal.

COMMIT: mint fresh lease. Old attempt remains immutable `suspended-for-blocker` history.

If restoring the initial root Task and no episode blocker remains unresolved, COMMIT may simultaneously mark the episode `unwinding`; after the root successor lease is proven live, a small controller-only transition closes the episode while retaining its evidence index. Root execution then continues as ordinary execution rather than keeping a needless open episode.

### I. `FAIL_CLOSED`

Any unexpected Git/workspace/controller identity, unresolved transition ambiguity, nonrepresentable snapshot, no-runnable dependency state, cycle, or rebind conflict can transition to a structured failed-closed controller state.

Effects: revoke any unsafe live lease, preserve all reachable snapshots/seals/transition records and exact actual state, deny new mutating contexts. Recovery requires a typed transition whose preconditions explicitly understand that failure state; never manufacture repair-of-repair work.

### J. `FINALIZE_CLEANUP` / `GC_SNAPSHOT`

Pure cleanup after semantic safety. Exact workspace/ref/snapshot eligibility and generation are required. Cleanup failure remains retryable finalization and cannot invalidate already-committed blocker/publication progress.

---

## 3. Publication/external-race policy by stage

### Mutable execution / suspended pre-candidate

External upstream advancement is another admitted base advancement. Rebind baseline/current trees, invalidate affected snapshot evidence, resume on the new tip. Conflict -> fail closed.

### Candidate prepared, not locally promoted

Candidate is supersedable. Rebind Task delta to new base, regenerate candidate/evidence.

### Locally promoted but not remotely observed

**Corrected rule:** still supersedable. Because no external observer has the candidate as publication authority, controller may CAS local ref back/forward to the rebound candidate after proving remote non-observation. Preserve old candidate as historical evidence.

This avoids pushing `A` then fixing blocker `B` as `A -> B` when the safer unpublished history can become `B -> rebound A`.

### Remotely observed candidate/prefix

Immutable. Never rebase/rewrite it away.

A later blocker or external advancement is additive:

```text
published A1 -> blocker B -> optional resumed A2
```

or, for unrelated external advancement already containing A1:

```text
published A1 -> external E -> resumed/revalidated continuation
```

Completion of A may accept a later canonical descendant only when controller lineage proves all intervening commits/transitions and required evidence has been reacquired; arbitrary descendant HEAD is not sufficient.

### Terminal complete

No blocker interruption. Later independent defect is new work.

---

## 4. Why blocker Tasks publish before parent resume

The preferred episode semantics are now explicit: a semantic blocker Task completes its normal accepted/publication path and advances the canonical repository base **before** its blocked parent resumes.

Therefore a chain executes serially as:

```text
A suspended
B suspended
C accepted -> published
B rebound onto C -> accepted -> published
A rebound onto B -> resume
```

not as a private stack of uncommitted repair commits waiting for A to finish.

This matches the useful part of raw 606 -> F11: F11 became the new base, then 606 work was restored/rebased onto it. It also ensures a valid blocker fix is not lost merely because the parent later fails to resume.

---

## Result

Architecture-level questions now closed:

- prepare/apply/commit/finalize transaction protocol;
- transition set and exact crash-recovery classification;
- blocker publication/integration point;
- ordinary external-main advancement interaction;
- publication race handling at mutable/prepared/local-only/remote-observed/terminal stages;
- correction that **remote observation is the immutable-history boundary**, superseding the earlier local-promotion rule.

Next work: portable AttemptSeal / semantic-Task / episode evidence-index schemas and Bundle-after-worktree-deletion acceptance, then consolidated current-code/raw-lineage end-to-end validation and final acceptance matrix.


## Amendments

### 2026-10-02 着手と検証・PR・Mergeの要求

````text
K4終わったっぽいから順次作業始めろ
まずはローカルで確認して最終的には既存通りPRでCIを通してからMergeしろ
Squash Mergeする際はちゃんとコミットコメントを作ること
CIを発動させるにはブランチの命名規則があるからそれに従うこと
````

### 2026-10-02 本配置の対象外化

既存installerによるmerge済みK3と後続K5〜K7の本配置許可確認に対するユーザー回答原文:

````text
本配置せず実装・PR・CI・Mergeのみ進める
````

## Resolved references

- 本Task本文へ保存した要求原文をCodexの実装契約とする。GitHub Issue #1190 は対象解決時の出典であり、可変Issue状態を実装authorityにしない。
- 設計出典: https://github.com/shinderuman/codex-worker-orchestrator/issues/1215
- K1・K2・K4は既存controller packageの実装を利用する。旧runtime形状は互換要件にしない。

## Purpose

controller所有の公開・統合と外部更新をcanonical blocker lifecycleの独立責務として成立させる。

## Contract

- 候補・remote観測・公開・統合・外部更新をcontrollerへbindする。観測済みprefixは変更せず、未公開候補のrebind後はsnapshot証拠を失効させる。blocker統合は親復元に先行する。
- GLM modelを呼び出さずCodex自身が実装・検証する。
- 最新Amendmentにより本配置は対象外とし、実装・PR・CI・Mergeだけを行う。
- web-gpt/**命名規則の専用branchでローカル検証後にPR CIを通し、意味のあるtitle/bodyを明示してSquash Mergeする。

## Must not

- 旧schemaのmigration・alias・互換fallbackを追加しない。
- recovery-of-recovery Task、入れ子のmutating worktree、force pushを追加しない。
- 他Taskの独立責務や既存未完了Taskを勝手に完了扱いしない。

## Acceptance criteria

- 保存済み要求原文のAcceptanceと実装チェック項目の境界を実装・回帰検証する。
- 中断・retry・missing/corrupt/stale stateでは正確なauthorityと証拠を保持し、false successへ縮退しない。
- ローカルの関連テスト・repository lint・vet/buildとPR CIが成功する。
- 最終diff review後、変更内容と検証結果を説明するSquashコミット本文でMergeする。

## Historical invariants

- 同一repositoryのlive mutating leaseとderived laneは各最大1個。
- root/focus Task、execution Task、attempt、evidence identityを分離する。
- remote観測済み履歴は不変。Task完了とevidence coverageは別の状態。

## Dependencies

none

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/lossless-suspension-single-lane.md`

## External feasibility

status: not-applicable
