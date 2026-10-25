# Task: Observation実行identityの再実行拒否寿命

## Original instruction

親CodexによるExternal Review採否の具体化指示（2026-10-25）:

````text
結果履歴の保持上限で同一decision roundの再実行拒否を失わない。
in-flight→indeterminate/pass/fail後も、同じTask/decision round/operation/params identityの再admissionを拒否する。結果表示の16件retentionと拒否identityの寿命を分離する。
既存state/lifecycle ownerへ収束し、第二DB・daemon・無期限に増える履歴を追加しない。round/task終端での安全な解放条件を明示する。
永続状態の意味や拒否条件の変更案は実装前にSol判断へ戻し、旧schema互換を設計理由にしない。
````

## Amendments

none

## Resolved references

- PR #1343 / comment 6110431434: RecoverStaleObservationExecutionsがindeterminateへ変更した後、trimObservationExecutionsはin-flight以外を16件へ制限する。Has/Beginのduplicate判定は残存recordだけなので、同一decision roundで17件以上のterminal追加後に同じoperation/paramsを再受理し得る。結果履歴とreplay拒否identityの寿命を同一視しない。

Reviewは外部の参考情報とし、実装要求は本Taskの親Codex判断を正とする。実行時にはcurrent sourceで成立性を再確認する。原Taskの要求はGit履歴 `ecb2e74611ecf7a09a1e6b673cc2982e8a4e9cc9:IMPLEMENTATION_TASKS/external-review-finding-adjudication.md` で回収できる。

- `glm-worker/internal/state/observation_execution.go:RecoverStaleObservationExecutions`
- `glm-worker/internal/state/observation_execution.go:trimObservationExecutions`
- `glm-worker/internal/state/observation_execution.go:HasObservationExecution`
- `glm-worker/internal/state/observation_execution.go:BeginObservationExecution`

## Purpose

結果履歴の保持上限で同一decision roundの再実行拒否を失わない。

## External feasibility

status: not-applicable

## Contract

- in-flight→indeterminate/pass/fail後も、同じTask/decision round/operation/params identityの再admissionを拒否する。結果表示の16件retentionと拒否identityの寿命を分離する。
- 既存state/lifecycle ownerへ収束し、第二DB・daemon・無期限に増える履歴を追加しない。round/task終端での安全な解放条件を明示する。
- 永続状態の意味や拒否条件の変更案は実装前にSol判断へ戻し、旧schema互換を設計理由にしない。

## Must not

- 対象外の外部指摘を一括実装しない。current schema拒否、Git/Task/source authority、独立Reviewを弱めない。
- 第二state DB、専用daemon、旧schema/旧protocol互換fallbackを追加しない。
- 根本原因・永続状態/公開API/architectureの未確定判断は実装前にSolへ戻す。

## Acceptance criteria

- 同じroundで17件以上のterminal記録を追加しても、古いindeterminateを含む同一identityが再実行されない。
- 別params/別roundの正当なadmissionと、遅延completion・中断復旧の拒否境界が維持される。
- 保持量の解放条件とreplay防止の証明が両立する。
- 関連する正規test/lintと独立Reviewを実施し、current sourceに束縛した証拠を残す。

## Historical invariants

- Current Gitと本Taskを要求正本とし、外部Review/Issueを第二仕様にしない。
- 誤成功・未帰属状態のadopt・既存成果の破壊で復旧させない。

## Dependencies

none
