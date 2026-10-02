# Task: canonical blocker経路への切替と統合検証

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
Depends on: #1216, #1217, #1188, #1218, #1190, #1186

## Purpose

Implement K7 from #1215: clean cutover to the completed architecture. This is not generic cleanup. Completion requires obsolete authority routes to be mechanically unreachable for normal mutating lifecycle.

## Scope

- route normal parent/model lifecycle through repository controller APIs;
- remove/replace path-local StateStore authority where it can mint independent mutation authority;
- remove/replace old blocker-specific park/new-task authority paths that conflict with one-episode/one-lease/one-lane semantics;
- remove/replace superseded defect-registration admission where #1217 owns the finding ledger;
- ensure no alternate branch/worktree/Plan rewrite path bypasses ExecutionLease admission;
- remove backward-compatibility shims that preserve the escaped topology;
- wire #1188 Attempt/Task/Episode Bundle projections as the audit authority;
- end-to-end fault-injection/acceptance harness for #1215's complete matrix;
- replay Old-root context-proliferation topology and prove no second mutating context can be minted;
- replay 606->F11 preserve/block/publish/rebind/resume flow without manual Plan/stash/reset choreography;
- crash injection at PREPARE/APPLY/COMMIT/FINALIZE boundaries;
- worktree deletion + Git GC + same-path lane recreation + historical Bundle reconstruction;
- final repository lint, whole-diff review and live Dogfood evidence gate.

## Acceptance

All #1215 acceptance scenarios must pass in the integrated runtime, especially:

- normal blocker and blocker-of-blocker serial replan;
- duplicate/cycle/nonblocker disposition;
- stale lease and path reuse rejection;
- restore conflict preserved fail-closed;
- external-main/publication races;
- remote-observed history immutability;
- cleanup failure retry;
- Bundle integrity after old worktree deletion;
- no-context-proliferation under injected failure.

A normal mutating lifecycle must have exactly one way to acquire authority: the canonical repository controller ExecutionLease/transition route.

## Must not

- do not leave the old topology enabled behind compatibility flags;
- do not retain a second blocker scheduler or evidence archive system;
- do not solve failures by creating a recovery-of-recovery Task/worktree;
- do not mark cutover complete from unit tests alone: require integrated/live Dogfood evidence appropriate to #1215.

Architecture checkpoints: #1215 comments `5911518029`, `5911566896`, `5911602795`, `5911667173` plus the earlier controller/suspension/publication checkpoints referenced by #1215.

## Implementation checklist

- [ ] 1. Route the normal mutating parent/model lifecycle through the canonical repository controller APIs.
  - Completion: normal mutation authority is acquired only through the K1 ExecutionLease/transition route.
- [ ] 2. Remove or make unreachable path-local StateStore/Plan/worktree authority paths that can mint independent mutation authority.
  - Completion: no alternate branch/worktree/Plan rewrite path can bypass ExecutionLease admission.
- [ ] 3. Remove or replace superseded blocker park/new-task and defect-registration paths.
  - Completion: K2 episode/finding semantics and K3 one-lane runtime are the only normal blocker lifecycle; no second scheduler remains.
- [ ] 4. Remove backward-compatibility shims preserving the escaped topology and wire K4 Bundle projections as canonical audit authority.
  - Completion: retired topology is not reachable behind feature/compatibility flags and no parallel evidence archive remains.
- [ ] 5. Build the integrated fault-injection/acceptance harness across K1-K6.
  - Cover PREPARE/APPLY/COMMIT/FINALIZE crashes, stale lease/path reuse, cleanup retry, publication races, immutable remote prefix and Bundle integrity.
- [ ] 6. Replay the Old-root context-proliferation topology.
  - Completion: injected failures cannot mint a second mutating context, repair-of-repair Task or nested physical lane.
- [ ] 7. Replay the 606->F11 preserve/block/publish/rebind/resume topology.
  - Completion: the flow succeeds or fails closed through canonical typed transitions without manual Plan/stash/reset choreography.
- [ ] 8. Prove worktree deletion + Git GC + same-path lane recreation + historical Bundle reconstruction end to end.
  - Completion: historical evidence remains exact and independent of current lane identity/path.
- [ ] 9. Perform final repository validation, whole-diff review and live Dogfood evidence gate.
  - K7 is not complete from unit tests alone; the full #1215 acceptance matrix, repository lint/CI and appropriate live Dogfood evidence must pass.


## Amendments

### 2026-10-02 着手と検証・PR・Mergeの要求

````text
K4終わったっぽいから順次作業始めろ
まずはローカルで確認して最終的には既存通りPRでCIを通してからMergeしろ
Squash Mergeする際はちゃんとコミットコメントを作ること
CIを発動させるにはブランチの命名規則があるからそれに従うこと
````

## Resolved references

- 本Task本文へ保存した要求原文をCodexの実装契約とする。GitHub Issue #1219 は対象解決時の出典であり、可変Issue状態を実装authorityにしない。
- 設計出典: https://github.com/shinderuman/codex-worker-orchestrator/issues/1215
- K1・K2・K4は既存controller packageの実装を利用する。旧runtime形状は互換要件にしない。

## Purpose

canonical blocker経路への切替と統合検証をcanonical blocker lifecycleの独立責務として成立させる。

## Contract

- 通常のmutating lifecycleをK1〜K6へ接続し、旧権限経路と互換shimを撤去する。fault injection、Old-root・606/F11 replay、GC後Bundle再構築、適切なlive Dogfoodで検証する。
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

- `IMPLEMENTATION_TASKS/lossless-suspension-single-lane.md`
- `IMPLEMENTATION_TASKS/controller-publication-integration.md`
- `IMPLEMENTATION_TASKS/terminal-task-metadata-transition.md`
