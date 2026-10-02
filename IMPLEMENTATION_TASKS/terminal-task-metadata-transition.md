# Task: 完了Taskメタデータのtyped遷移

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
Depends on: #1216, #1217, #1190

## Purpose

Implement K6 from #1215. Keep repository-specific terminal Plan/Task/dependency retirement separate from the generic controller/publication kernel, but make it one typed snapshot-bound transition rather than free-form parent editing.

## Scope

Repository harness owns mechanically determined terminal metadata changes for an exact controller-proven terminal semantic Task:

- remove/retire exact terminal Task file where repository policy requires it;
- remove/retire exact schedule entry;
- advance normal Plan ACTIVE/NEXT only when mechanically determined by normal completion semantics;
- retire mechanically safe inbound `Dependencies` references into `Fulfilled dependencies`;
- support a terminal blocker Task that was never root Plan ACTIVE while leaving the root/focus Task unchanged;
- expose newly runnable blocker-closure state back to #1217 without selecting unrelated ordinary NEXT work;
- validate task corpus/Plan before commit;
- bind source/result ProjectSnapshot/controller generation/transition identity;
- retry by exact expected-old/new state rather than infer phase from missing files/history.

## Ordering

For a blocker Task T:

1. #1190 proves T's accepted production result is canonically published/integrated;
2. this transition retires T's repository metadata / mechanically fulfills dependencies;
3. evidence/finalization linkage becomes durable;
4. #1217 scheduler may select the next closure member or root resume.

Do not mark a blocker fulfilled merely because its lane/review completed before integration.

## Semantic boundary

If retirement requires a semantic scheduling/priority/follow-up decision, stop and leave it to parent authority. This Issue must not become a blocker scheduler or generic Markdown editor.

## Acceptance

- normal terminal Task retirement prevents `completed_task_file_still_tracked` repair churn;
- inbound dependency retirement is deterministic or stops precisely on ambiguity;
- exact terminal blocker Task can retire while root remains focus/Plan ACTIVE;
- arbitrary NEXT/BLOCKED Task without controller terminal/integration proof is rejected;
- after prerequisite retirement, #1217 can observe newly runnable dependent only inside its admitted closure;
- crash/retry distinguishes integrated / metadata-retired / evidence-finalized phases explicitly;
- stale source ProjectSnapshot fails closed;
- task-corpus checks, focused tests, repository lint and whole-diff review pass.

Architecture checkpoints: #1215 comments `5911271743`, `5911566896`, `5911602795`, `5911667173`.

The prior #1186 boundary is retained only because it independently matches this final repository-harness responsibility; references to the deleted historical blocker Issue split are superseded.

## Implementation checklist

- [ ] 1. Define the typed terminal metadata transition input/output contract.
  - Bind exact semantic Task, source/result ProjectSnapshot, controller generation and transition identity.
  - Reject arbitrary NEXT/BLOCKED Tasks without terminal/integration proof.
- [ ] 2. Implement deterministic terminal Task/schedule retirement.
  - Completion: exact terminal Task metadata is retired without free-form parent editing or inference from missing files/history.
- [ ] 3. Implement deterministic inbound dependency retirement into `Fulfilled dependencies`.
  - Completion: mechanically safe references are updated; ambiguity stops precisely instead of guessing.
- [ ] 4. Implement normal Plan ACTIVE/NEXT advancement only where repository policy mechanically determines it.
  - Completion: semantic scheduling/priority/follow-up decisions remain outside K6.
- [ ] 5. Support terminal blocker retirement while preserving the root/focus Task.
  - Depends on: K5 integration proof and K2 episode semantics.
  - Completion: blocker completion exposes newly runnable closure state without selecting unrelated ordinary NEXT work.
- [ ] 6. Make retry/recovery distinguish integrated, metadata-retired and evidence-finalized phases by exact expected-old/new state.
  - Completion: stale source ProjectSnapshot fails closed and retries do not infer progress from absent metadata.
- [ ] 7. Validate task corpus/Plan and prove terminal transition regressions.
  - Cover normal retirement, blocker retirement, dependency fulfillment, ambiguity, stale snapshot, crash/retry, repository lint, relevant tests and whole-diff review.


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

- 本Task本文へ保存した要求原文をCodexの実装契約とする。GitHub Issue #1186 は対象解決時の出典であり、可変Issue状態を実装authorityにしない。
- 設計出典: https://github.com/shinderuman/codex-worker-orchestrator/issues/1215
- K1・K2・K4は既存controller packageの実装を利用する。旧runtime形状は互換要件にしない。

## Purpose

完了Taskメタデータのtyped遷移をcanonical blocker lifecycleの独立責務として成立させる。

## Contract

- 統合済みterminal TaskだけのTask/schedule/dependency retirementをexact source/result ProjectSnapshotへbindする。blocker retirementではrootを維持し、意味判断を要するschedule変更では停止する。
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


## External feasibility

status: not-applicable

## Fulfilled dependencies

- `IMPLEMENTATION_TASKS/controller-publication-integration.md`
