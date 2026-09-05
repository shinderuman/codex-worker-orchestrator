# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。現在のbranch・HEAD・dirty stateも複製せず、Git現物と`glm-worker --project-state`を正とする。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/post-worker-quality-gate-recovery.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/quality-surface-approval-review-continuation.md`
- `IMPLEMENTATION_TASKS/quality-toolchain-preflight-before-model.md`
- `IMPLEMENTATION_TASKS/markdown-derived-state-authority-audit.md`
- `IMPLEMENTATION_TASKS/external-review-a70d35c-43e1da9-follow-up.md`
- `IMPLEMENTATION_TASKS/parent-codex-rollout-chain-attribution.md`
- `IMPLEMENTATION_TASKS/telemetry-history-compact-summary.md`
- `IMPLEMENTATION_TASKS/codex-efficiency-reevaluation-checkpoint.md`
- `IMPLEMENTATION_TASKS/auto-resume-heartbeat-transaction.md`
- `IMPLEMENTATION_TASKS/prose-only-control-enforcement-audit.md`
- `IMPLEMENTATION_TASKS/task-stats-revision-consumer-audit.md`
- `IMPLEMENTATION_TASKS/continuous-improvement-task-capture.md`
- `IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`
- `IMPLEMENTATION_TASKS/runtime-install-completion-binding.md`
- `IMPLEMENTATION_TASKS/parent-plan-continuation-enforcement.md`
- `IMPLEMENTATION_TASKS/codex-instruction-conflict-reduction.md`
- `IMPLEMENTATION_TASKS/mechanized-control-prose-thinning.md`
- `IMPLEMENTATION_TASKS/packet-validation-correction-recovery.md`
- `IMPLEMENTATION_TASKS/user-level-installation-scope-redesign.md`
- `IMPLEMENTATION_TASKS/105-session-rotation.md`
- `IMPLEMENTATION_TASKS/structured-validation-gate-telemetry.md`
- `IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md`
- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT

- `IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`
- `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`
- `IMPLEMENTATION_TASKS/101-live-sol-ab.md`
- `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`
- `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`
- `IMPLEMENTATION_TASKS/104-test-impact-selection.md`
- `IMPLEMENTATION_TASKS/106-review-call-reduction.md`
