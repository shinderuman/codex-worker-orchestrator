# Task: GLMのGit authority越権をproductionで防止する

## Original instruction

````text
F3: GLMのGit mutation禁止がprompt依存

- repository contractではGLM worker/reviewer commit/push禁止。
- 一方worker promptは「明示依頼なしにcommitしない」と読め、task requirementのcommit要求を自分への許可と誤認する余地がある。
- workerはBashを持つため、commit/ref操作自体を技術的には実行できる。

REQUIRED HARDENING

- GLM worker/reviewerによるcommit / branch / ref / reset / checkout破棄 / push等の禁止をprompt以外でも強制する。
- 少なくともmodel call前後でHEAD/ref/index等の禁止mutationをdeterministicに検出しfail closedできるか検討する。
- implementation上必要な通常source editは阻害しない。
- parent Codexのcommit/push authorityを巻き込まない。
- GLMとparentのGit authorityを混同しない。
````

## Amendments

- 2026-08-26 Product boundary: 任意repoでworker/reviewerが親Codexのcommit/ref/push authorityを勝手に行使できないgeneric production guardをTrack Aとして実装する。
- 2026-08-26 Clarification: Track A/Bを区別し、本repo固有hookだけでgeneric failure classを完了扱いにしない。

## Resolved references

- 禁止mutationはcommit、branch/ref作成・更新、push、reset、checkout等による破棄を含む。通常source/index editの必要境界は一次証拠で確定する。

## External feasibility

status: not-applicable

## Purpose

GLMがtask内commit文言等を自己許可と解釈して親Git authorityを行使するfailure classを任意repositoryで機械防止する。

## Contract

- worker/reviewer call前後のHEAD/ref/index/worktree authorityを区別し、禁止Git mutationをdeterministicに拒否・検出する。
- 親Codexのcommit/pushと通常source editを阻害せず、rollback/recoveryを明示する。

## Must not

- prompt禁止、repo固有hook、全Git操作禁止、親authority制限で代替しない。

## Acceptance criteria

- 任意repoのproduction pathでcommit/ref/push/破棄操作の代表caseをfail closed固定する。
- 通常実装editと親commit/pushは既存authorityで成立する。
- F3のTrack A/B分類と追加定常costを記録する。

## Historical invariants

- GLM commit/push禁止、親Codex限定fast-forward許可を維持する。

## Dependencies

- `IMPLEMENTATION_TASKS/eval-responsibility-reduction.md`

## Review findings

none

## Current boundary

NEXT。EVAL責務整理完了後にF3をTrack A/Bへ分類して着手する。
