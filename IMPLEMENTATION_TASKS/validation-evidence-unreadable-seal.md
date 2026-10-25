# Task: 読取不能validationのSeal coverage

## Original instruction

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
読めないgate/smoke記録を黙殺してcompleteとしない。
runtimeとrepositoryの両storeでqualitygate.Read/readSmokeRunRecordのread/decode/identity failureを区別し、既存sealのUnreadableへexact logical identityを残すか正規拒否する。
読取不能を抱えるsealをcompleteにせず、正当な他Task/window外の除外と欠落/破損の区別を保つ。
live exportとsealed exportのcoverage意味を同じ既存ownerへ合わせる。新しい互換状態を作らない。
````

## Amendments

none

## Resolved references

- PR #1343 / comment 4242081409: qualitygate.Readとsmokeの読取失敗をsealが黙殺し、runtimeCapturedだけでcompleteへ進む。誤ったcoverageを修復する。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/controller/runtime_evidence.go:readValidationRunRecord`
- `glm-worker/internal/controller/runtime_evidence.go:applyAttemptSealRuntimeCoverage`
- `glm-worker/internal/controller/evidence_attempt_seal.go:validateAttemptSealCoverage`

## Purpose

読めないgate/smoke記録を黙殺してcompleteとしない。

## External feasibility

status: not-applicable

## Contract

- runtimeとrepositoryの両storeでqualitygate.Read/readSmokeRunRecordのread/decode/identity failureを区別し、既存sealのUnreadableへexact logical identityを残すか正規拒否する。
- 読取不能を抱えるsealをcompleteにせず、正当な他Task/window外の除外と欠落/破損の区別を保つ。
- live exportとsealed exportのcoverage意味を同じ既存ownerへ合わせる。新しい互換状態を作らない。

## Must not

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

- corrupt/unreadable gate runとsmoke runを両storeで再現し、未記録completeにならない。
- 正当なforeign task/window外の除外、正常記録の保持、complete+Unreadable拒否が維持される。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

## Historical invariants

- Current Gitと本Taskを要求正本とし、外部Review/Issueを第二仕様にしない。
- 誤成功・未帰属状態のadopt・既存成果の破壊で復旧させない。

## Dependencies

none
