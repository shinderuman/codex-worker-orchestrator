# Task: 復旧拒否のerror原因と論理条件の分離

## Original instruction

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
nil errorのwrapと常に真の判定で拒否原因を壊さない。
startup recoveryの空request/既存checkpoint、retired source/read authorityの論理不一致とGit/I/O失敗を分ける。%wへnilを渡さない。
abortRacedBaseAdvancementの必ずerrorとなる結果は同じerrorを直接返し、不要な常真分岐を残さない。
既存の拒否条件・error chain・controller/state mutationの範囲を保持する。汎用error frameworkへ拡張しない。
````

## Amendments

### 2026-10-25 External Reviewから同一責務へ統合: quality-surface-approval-error-provenance

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
品質surface approval対象のcollectorを再実行して最初の失敗を隠さない。
最初のcurrentQualitySurfaceReviewTargets/collectChangedPathsの結果を使い、読取失敗と正当なno-targetを区別する。
outsideGitViewの判断を二度目のGit結果で推定せず、元のerrorとexact source evidenceを保持する。
````

## Resolved references

- PR #1345 / comment 4254066700: finishPublicationAbortは全正常終端でPublicationAdvancementRequiredを返しnilにはならない。常に真の比較を除き、同じ拒否結果を直接返す。
- PR #1349 / comment 4269331588: last-requestが空かつerr=nilの論理拒否で%w(nil)になる。I/O errorと空requestを別分類する。
- PR #1349 / comment 4269331603: checkpoint存在時err=nilを%wでwrapし、破損読取もabsent要求違反と同一化する。存在/不存在/読取失敗を分ける。
- PR #1375 / comment 4347607583: fast-forward/branch/refの論理不一致でnil errorを%w wrapする分岐が現行にもある。拒否自体を維持し、Git failureと論理不一致を区別する。
- PR #1375 / comment 4347607599: descendant=false,err=nilで誤ったwrap表示になる現行分岐を確認。history拒否を弱めずerror provenanceを修正する。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/controller/publication_base_advancement.go:applyBaseAdvancement`
- `glm-worker/internal/controller/publication_abort.go:finishPublicationAbort`
- `glm-worker/internal/workflow/start_initialization_recovery.go:RecoverUnstartedMilestone`
- `glm-worker/internal/workflow/start_initialization_recovery.go:verifyUnstartedMilestoneRuntime`
- `glm-worker/internal/controller/retired_source.go:validateRetiredSourceAdvancement`
- `glm-worker/internal/controller/retired_source.go:validateRetiredSourceBoundary`
- `glm-worker/internal/controller/workflow_binding.go:retiredWorkflowAuthority`

- PR #1345 / comment 5498051852: 最初のcollector失敗後に再度collectChangedPathsを実行し、二度目成功ならoutsideGitView=trueへ誤分類する。最初の結果/原因を保持し、明示no-targetと読取失敗を区別する。

- `glm-worker/internal/workflow/quality_gate.go:qualitySurfaceApprovalReviewTargets`

## Purpose

nil errorのwrapと常に真の判定で拒否原因を壊さない。

## External feasibility

status: not-applicable

## Contract

- startup recoveryの空request/既存checkpoint、retired source/read authorityの論理不一致とGit/I/O失敗を分ける。%wへnilを渡さない。
- abortRacedBaseAdvancementの必ずerrorとなる結果は同じerrorを直接返し、不要な常真分岐を残さない。
- 既存の拒否条件・error chain・controller/state mutationの範囲を保持する。汎用error frameworkへ拡張しない。

External Reviewで確認した同一責務の具体条件（quality-surface-approval-error-provenance）:

- 最初のcurrentQualitySurfaceReviewTargets/collectChangedPathsの結果を使い、読取失敗と正当なno-targetを区別する。
- outsideGitViewの判断を二度目のGit結果で推定せず、元のerrorとexact source evidenceを保持する。

## Must not

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

- 論理不一致のmessageに%!w(<nil>)が出ず、実I/O/Git failureはerrors.Is/As可能な元原因を保持する。
- 既存拒否動作とpublication abortの必要な状態変更を維持し、関連lintを通す。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

- 初回failure/二回目なら成功となるcollectorでも再呼出せずerrorを保持する。
- no-target、正常targets、collectorなしを区別し、品質承認のfail-closedを維持する。

## Historical invariants

- Current Gitと本Taskを要求正本とし、外部Review/Issueを第二仕様にしない。
- 誤成功・未帰属状態のadopt・既存成果の破壊で復旧させない。

## Dependencies

none
