# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/gpt-external-review-current-head-validation.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/instruction-surface-ownership.md`
- `IMPLEMENTATION_TASKS/unknown-production-surface-risk.md`
- `IMPLEMENTATION_TASKS/glm-git-authority-enforcement.md`
- `IMPLEMENTATION_TASKS/quality-evidence-mutation-risk.md`
- `IMPLEMENTATION_TASKS/deterministic-rule-activation.md`
- `IMPLEMENTATION_TASKS/sol-decision-boundary-enforcement.md`
- `IMPLEMENTATION_TASKS/prose-semantic-guard-migration.md`
- `IMPLEMENTATION_TASKS/instruction-conflict-resolution.md`
- `IMPLEMENTATION_TASKS/010-task-splitting-milestones.md`
- `IMPLEMENTATION_TASKS/011-operation-category-telemetry.md`
- `IMPLEMENTATION_TASKS/012-compaction-threshold-evaluation.md`
- `IMPLEMENTATION_TASKS/013-worker-model-routing-evaluation.md`
- `IMPLEMENTATION_TASKS/014-test-impact-evaluation.md`
- `IMPLEMENTATION_TASKS/016-worker-repo-search-integration.md`
- `IMPLEMENTATION_TASKS/017-reviewer-diff-first-search.md`
- `IMPLEMENTATION_TASKS/018-exhaustive-search-gate.md`
- `IMPLEMENTATION_TASKS/019-repo-search-product-wiring.md`
- `IMPLEMENTATION_TASKS/020-repo-search-telemetry-eval.md`
- `IMPLEMENTATION_TASKS/021-conditional-improvements.md`
- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT

- `IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`
- `IMPLEMENTATION_TASKS/autonomous-development-harness.md`
- `IMPLEMENTATION_TASKS/desktop-terminal-payload-double-render-boundary.md`
- `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`
- `IMPLEMENTATION_TASKS/101-live-sol-ab.md`
- `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`
- `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`
- `IMPLEMENTATION_TASKS/104-test-impact-selection.md`
- `IMPLEMENTATION_TASKS/105-session-rotation.md`
- `IMPLEMENTATION_TASKS/106-review-call-reduction.md`

## 現在のGit境界

- branch: `main`
- implementation baseline: GPT external review PR #2のfindingをcurrent HEADへ照合し、EVAL自然言語authority重複を解消した状態
- implementation boundary: PR #2は指定ref fetch・proposal object確認・lossless GLM照合・適応修正・全quality gate・独立review・親Codex semantic review・parent accept・History移行を完了。PR #1をACTIVEへ昇格したが、fetch・proposal確認・GLM dispatchを含め未着手
- preserved boundary: wake coalescingの10分closed interval・fail-open reservation・task ID fail-closed、machine output boundary、full smoke証拠再利用、GLM commit/push禁止を維持
- push: 各親commit後の`refs/heads/main`とGreptile正常review後の`refs/heads/codex/greptile-reviewed`だけを親Codexが通常fast-forwardする。GLM push、force/non-fast-forward、他refへのpushは禁止

## 現在の停止理由

ユーザーがPR #2のmain push完了後に一度停止するよう明示したため。PR #1はACTIVEへ昇格済みだが未着手のまま保持する。

## 次の親Codex操作

ユーザーの再開指示後、PR #1 taskのOriginal instructionと保存済みexternal review payloadを再読し、proposal transportの現状確認から開始する。
