# Task: bundleのtask diffへtask中に作成・commitしたfileを保持する

## Original instruction

````text
Diffファイルが抜けているというやつはBundleを作ったあとシェルスクリプトで抜き出すとか運用でカバーすればCodexとかにやらせられるのではないだろうか
````

## Amendments

````text
CommentlintとBundle DiffをCodex用に残すのはいい
````

## Resolved references

- production bundleでは`current-state/snapshot/task-diff.patch`がtask中に新規作成後commitされた3 fileを落とし、`review-current-task.patch`側には存在していた。
- current `taskdiff.Capture`はrecorded baseline indexからのtracked diffに、real repositoryで現在untrackedなfileだけを追加するため、baselineには無いがbundle時点でtracked/committed済みのfileがどちらにも入らない。
- 次のcommentlint dogfood監査まではrecorded baseline/current Gitからdeterministic supplementary diffを作る運用でカバー可能なので、pre-dogfood Web GPT実装のblocking条件にはしない。

## Purpose

canonical bundleのfresh task diffだけでtask-owned changesを完全に監査できるようにし、task中に作成されてcommit済みのfileを取りこぼさない。

## Contract

- recorded task baselineをauthorityとする。
- baseline indexに存在せずcurrent task resultに存在するpathは、bundle時点でtracked/committed済みでもtask diffへ含める。
- baseline以前から存在するuntracked fileとの区別を維持する。
- binary-safe diff behaviorを維持する。
- bundle生成はrepository/task lifecycleへread-onlyのままにする。

## Must not

- commit messageやtimestampからtask ownershipを推測しない。
- stale/unattributed `review-current-task.patch`をcanonical fallbackとして依存しない。
- この修正を理由に次のcommentlint dogfoodを先送りしない。

## Acceptance criteria

- `baseline clean -> new file作成 -> git add/commit -> bundle`でnew fileがcanonical `task-diff.patch`へ入るregression testを追加する。
- commit前後で同じtask-created fileがtask diffから消えない。
- baseline-preexisting untracked、modified、deleted、binary pathの既存semanticsを維持する。
- repository validationと独立reviewを通す。

## Historical invariants

- task diff baselineはtask start時のHEAD/index/worktree/untracked snapshotで固定する。
- bundleはcanonical post-task analysis artifact ownerである。

## Dependencies

none

## Review findings

none

## Current boundary

未着手。commentlint dogfood後のCodex+GLM implementation taskとして残す。
