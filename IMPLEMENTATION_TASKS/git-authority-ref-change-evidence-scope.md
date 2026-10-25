# Task: Git authority変更証拠のscope一致

## Original instruction

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
authority検出digestとRefChangesの対象を一致させる。
before/after symbolic HEAD targetのauthority ref unionから変更証拠を作り、unrelated branches/remotes/refs/codex churnを混入させない。
branch rename/symbolic HEAD変更では両側の対象を保持し、実authority変化のmissing/truncationを無視して通過させない。
````

## Amendments

none

## Resolved references

- PR #1345 / comment 4254066707: 検出digestはauthority refのみだがRefChangesは全refsから生成されるため、実authority変更と無関係refs多数変更で64件上限へ達し復旧証拠がtruncatedになる。before/after authority unionを正とする。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/runner/git_authority_guard.go:captureGitAuthoritySnapshot`
- `glm-worker/internal/runner/git_authority_guard.go:verify`
- `glm-worker/internal/runner/git_authority_guard.go:gitAuthorityRefChanges`

## Purpose

authority検出digestとRefChangesの対象を一致させる。

## External feasibility

status: not-applicable

## Contract

- before/after symbolic HEAD targetのauthority ref unionから変更証拠を作り、unrelated branches/remotes/refs/codex churnを混入させない。
- branch rename/symbolic HEAD変更では両側の対象を保持し、実authority変化のmissing/truncationを無視して通過させない。

## Must not

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

- authority変更と同時に64件超の無関係ref変更があっても、必要証拠が上限に押し出されない。
- authority refの移動/削除/renameとdetached HEADを現行契約に沿って検証する。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

## Historical invariants

- Current Gitと本Taskを要求正本とし、外部Review/Issueを第二仕様にしない。
- 誤成功・未帰属状態のadopt・既存成果の破壊で復旧させない。

## Dependencies

none
