# Task: 損失のない中断・復元と単一実行レーン

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
Depends on: #1216, #1217, #1188

## Purpose

Implement K3 from #1215: the physical/runtime side of canonical blocker episodes without nested mutating worktrees.

## Scope

Implement:

- lossless suspension capture using normalized content-addressed baseline/current index+worktree Git trees;
- explicit supported/unsupported workspace contract;
- parent/controller-managed path exclusion from Task-owned replay;
- retained operational snapshot refs/manifests;
- two-stage three-way baseline/current rebind onto a new blocker/external base;
- exactly one disposable detached derived lane per repository episode;
- workspace_id + git-dir/common-dir/HEAD/index/worktree postcondition verification;
- fresh AttemptRecord/ExecutionLease materialization through K1;
- stale-path/stale-workspace rejection;
- lane recycle/delete ordering after seal/index + lease revoke;
- typed cleanup retry and retained object/ref GC eligibility;
- fail-closed merge/ownership conflict with no stash/reset/rebase/conflict-repair Task fallback.

## Required snapshot semantics

Preserve staged/unstaged distinctions, tracked modifications, staged/unstaged deletion, non-ignored untracked regular files, symlinks/targets and Git executable-bit semantics. Fail before lease revocation for unresolved index stages, intent-to-add/other unsupported index semantics, unsupported special filesystem objects, dirty nested submodule state that cannot be represented losslessly, missing objects or ambiguous path/type/mode ownership.

## Runtime invariant

Semantic dependency depth is unbounded by an arbitrary numeric limit, but runtime authority is always:

`<= 1 repository blocker episode + <= 1 live mutating lease + <= 1 derived physical lane`.

A blocker-of-blocker suspends the current attempt and recycles the same lane; it never pushes another worktree/runtime frame.

## Acceptance

Cover #1215 scenarios for conflict-free rebind, content/type/mode conflict, unsupported suspension state, stale lease/path reuse, cleanup failure, crash around materialization/cleanup, 606->F11 preserve/publish/resume behavior, and same-path lane reuse.

Current `park`/`unpark` may provide low-level test ideas but is not a compatibility target.

Architecture checkpoints: #1215 comments `5911153757`, `5911271743`, `5911518029`, `5911566896`, `5911602795`, `5911667173`.

## Implementation checklist

Work in this dependency order. A checkbox is complete only when its stated boundary is implemented and its focused regression evidence passes; do not check items merely because supporting types/helpers exist.

- [ ] 1. Define and implement the lossless suspension snapshot contract.
  - Preserve baseline/current index+worktree trees, staged/unstaged state, deletions, untracked regular files, symlinks/targets and executable-bit semantics.
  - Reject unsupported index/filesystem/submodule states before authority is revoked.
- [ ] 2. Persist operational snapshot refs/manifests and exclude controller/parent-owned paths from Task replay.
  - Completion: captured state is content-addressed and independently verifiable before suspension proceeds.
- [ ] 3. Implement the two-stage three-way rebind for baseline and current state.
  - Completion: conflict-free rebind preserves semantics; content/type/mode/ownership ambiguity fails closed without stash/reset/rebase/conflict-repair fallback.
- [ ] 4. Implement exactly one disposable derived execution lane per repository episode.
  - Verify workspace_id, git-dir/common-dir, HEAD, index and worktree postconditions on materialization/reuse.
  - Do not create nested physical lanes for blocker-of-blocker.
- [ ] 5. Materialize fresh AttemptRecord/ExecutionLease authority through K1 and reject stale path/workspace/lease/generation before mutation.
  - Depends on: 1-4 and K1 interfaces.
- [ ] 6. Implement seal/revoke/recycle/delete ordering and typed cleanup retry.
  - Depends on: K4 sealing/index precondition.
  - Completion: destructive lane recycling cannot occur before evidence is durably sealed; retained refs/objects become GC-eligible only at the defined boundary.
- [ ] 7. Prove the integrated K3 regression matrix and perform final validation/review.
  - Cover conflict-free rebind, unsupported state, stale lease/path reuse, cleanup failure, crash boundaries, same-path reuse and 606->F11 preserve/publish/resume behavior.
  - Repository lint, focused/full relevant tests and whole-diff review must pass before K3 is complete.

### 保存・復元の確定設計原文

## Design checkpoint — lossless suspension, single lane, retained-object lifetime

This closes the three open Suspend / resume / worktree-lifecycle design items at architecture level.

Grounding: migrated raw 606 -> F11 evidence, current `baseline.go` / `taskdiff` / `retention.go` / `snapshot.go` / `park.go`, and an isolated Git feasibility experiment already run during this audit.

### 1. Suspension is Git-semantic Task-owned state, not worktree lifetime

For one suspended attempt retain immutable identities for:

```text
execution_base_commit H0
baseline_index_tree    BI0
baseline_worktree_tree BW0
current_index_tree     CI0
current_worktree_tree  CW0
parent/controller-authority digest
attempt/task/episode/controller provenance
```

The four trees are normalized full repository projections. Parent/controller-managed paths are normalized to canonical controller/base content rather than captured as Task-owned deltas:

- `IMPLEMENTATION_RULES.md`
- `IMPLEMENTATION_PLAN.local.md`
- `IMPLEMENTATION_HISTORY.md`
- `IMPLEMENTATION_TASKS/`

This is deliberate: raw 606 -> F11 showed production state could be restored while local Plan metadata caused the actual restore conflict.

The pair-of-pairs preserves both pre-existing repository dirt and Task-owned delta:

```text
H0 -> BI0/BW0        = pre-Task baseline state
BI0/BW0 -> CI0/CW0  = Task-owned current state
```

Index and worktree remain distinct so staged vs unstaged state survives.

Supported state includes ordinary tracked changes, staged/unstaged changes, staged/unstaged deletion, non-ignored untracked regular files, symlinks/target changes, and Git executable-bit semantics.

Suspension fails closed before lease revocation for non-lossless shapes, including unresolved index stages, intent-to-add/other non-stage-0 index semantics that would be lost, non-ignored special filesystem objects, dirty nested submodule worktree state beyond an admitted gitlink OID, or any path that cannot be reproduced with exact regular/symlink + executable semantics.

Ignored untracked files are explicitly ephemeral/non-authoritative. Empty directories and non-Git filesystem metadata such as timestamps/general xattrs are not part of repository-semantic Task state.

### 2. Resume uses a two-stage three-way rebind

Given blocker-integrated base `H1`:

```text
BI1 = merge3(H0.tree, BI0, H1.tree)
BW1 = merge3(H0.tree, BW0, H1.tree)

CI1 = merge3(BI0, CI0, BI1)
CW1 = merge3(BW0, CW0, BW1)
```

First rebind the pre-existing baseline onto the blocker base, then replay the Task-owned delta onto that rebound baseline.

This prevents both attribution errors:

- blocker delta becoming Task-owned work;
- pre-existing dirty baseline being lost by simply resetting the Task baseline to `H1`.

All merges are preflighted in synthetic objects/temp indexes before mutating a real workspace/ref. Conflict, add/add/type/mode ambiguity, missing object, unsupported path shape, or authority mismatch fails closed. No conflict-repair Task/worktree is created.

After successful rebind the controller proves materialized index/worktree exactly match `CI1/CW1`, parent-managed paths match current controller authority, then mints a fresh runtime attempt/baseline revision and invalidates stale snapshot-bound review/quality/publication evidence.

The isolated Git experiment covered baseline staged+unstaged dirt, Task staged+unstaged changes, staged/unstaged deletion, executable mode, symlink target, untracked content and a disjoint blocker commit; distinctions survived the two-stage rebind. Same-content blocker/Task overlap produced a normal merge conflict, which is the desired fail-closed boundary.

### 3. Snapshot objects must remain reachable independently of worktree cleanup

Each operational suspension has one immutable `snapshot_id` tied to a retained Git object root/manifest. Conceptually it retains the four trees plus provenance manifest under a controller-owned reachable ref namespace equivalent to:

```text
refs/glm-worker/suspensions/<snapshot-id>
```

Exact ref naming is implementation detail. Required invariant: operational objects remain Git-reachable without the physical lane/worktree.

The snapshot transition is not committed until retained objects/ref and controller manifest match. Missing/pruned retained objects are repository corruption and fail closed; the controller never guesses/reconstructs bytes from prose.

### 4. One disposable detached physical lane

The episode has at most one derived mutating lane. It should normally be a detached worktree at the controller episode integration tip, not one branch per semantic blocker Task.

Each materialization gets a fresh generated `workspace_id` bound to:

```text
canonical path
git worktree git-dir identity
verified common-dir repository identity
expected HEAD/integration tip
expected index/worktree snapshot
attempt_id
lease_id
controller generation / episode revision
```

Path is location only, never authority.

Create/materialize requires old lease revoked, no conflicting transition/in-flight mutation, exact integration/project snapshot identities, and a prepared transition recording expected old/absence and intended new workspace identity. The lease is minted only after common-dir/git-dir/HEAD/index/worktree postconditions pass.

For resume, materialize already-preflighted `CI1/CW1` before the fresh lease becomes live.

### 5. Destructive lane cleanup ordering

A model cannot delete its worktree merely because it believes the Task is done.

Before remove/recycle:

1. no in-flight call;
2. attempt is admitted terminal or suspended-for-blocker;
3. required operational snapshot is retained;
4. immutable AttemptSeal/index link is durable;
5. execution lease is durably revoked;
6. current lane common-dir/git-dir/HEAD/index/worktree identities match the cleanup transition exactly;
7. cleanup transition is write-ahead prepared.

Only then may the controller remove that exact worktree. If the path contains an unknown/nonmatching worktree or normal directory, do **not** `rm -rf`; fail closed. Cleanup failure stays as a pending cleanup transition and blocks reuse until exact recovery completes.

A recreated lane at the same pathname receives a new `workspace_id`, git-dir identity, attempt/lease and controller-generation binding. Therefore stale callers cannot regain authority from pathname reuse.

### 6. Operational snapshot GC rule

Retain suspension objects while any of these remain true:

- a nonterminal semantic Task may resume from them;
- root/episode unwind may consume them;
- an incomplete transition references them as expected old/new state;
- a successor attempt materialization/commit is not yet durable;
- portable immutable evidence/index finalization has not made future Bundle reconstruction independent of them.

For a suspended child, operational snapshot refs become GC-eligible only after successor rebind/materialization is durably committed, no transition can return to the old operational snapshot, and AttemptSeal/index lineage preserves audit reconstruction.

For the root suspension, retain operational objects through successful root handoff and episode close.

GC is itself typed controller cleanup: remove only exact eligible refs under expected controller generation and never perform broad repository pruning as lifecycle recovery.

### Result

Architecture-level questions now closed:

- exhaustive supported/unsupported snapshot contract;
- two-stage lossless baseline/current rebind;
- physical lane create/recycle/delete authority and stale-path rejection;
- operational snapshot/ref retention and GC eligibility.

Next work: full controller transition table/CAS recovery matrix, legitimate external-main/publication race interaction, then portable AttemptSeal/Task/Episode Bundle schemas and final acceptance matrix.


## Amendments

### 2026-10-02 着手と検証・PR・Mergeの要求

````text
K4終わったっぽいから順次作業始めろ
まずはローカルで確認して最終的には既存通りPRでCIを通してからMergeしろ
Squash Mergeする際はちゃんとコミットコメントを作ること
CIを発動させるにはブランチの命名規則があるからそれに従うこと
````

## Resolved references

- 本Task本文へ保存した要求原文をCodexの実装契約とする。GitHub Issue #1218 は対象解決時の出典であり、可変Issue状態を実装authorityにしない。
- 設計出典: https://github.com/shinderuman/codex-worker-orchestrator/issues/1215
- K1・K2・K4は既存controller packageの実装を利用する。旧runtime形状は互換要件にしない。

## Purpose

損失のない中断・復元と単一実行レーンをcanonical blocker lifecycleの独立責務として成立させる。

## Contract

- K1・K2・K4を利用し、開始前と現在のindex/worktreeを分離保存する。二段階three-way rebind、単一detached lane、fresh attempt/lease、証拠保全後のcleanup/GCをtyped controller transitionで実装する。
- GLM modelを呼び出さずCodex自身が実装・検証する。
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

## External feasibility

status: not-applicable
