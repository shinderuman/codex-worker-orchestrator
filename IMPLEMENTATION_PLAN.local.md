# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。現在のbranch・HEAD・dirty stateも複製せず、Git現物と`glm-worker --project-state`を正とする。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/review-failure-path-deadline-gate.md`

## NEXT（優先順）
- `IMPLEMENTATION_TASKS/system-one-sol-finding-feedback-gate.md`
- `IMPLEMENTATION_TASKS/conditional-adversarial-failure-path-reviewer-trial.md`
- `IMPLEMENTATION_TASKS/repo-search-system-one-relevance-filter-eval.md`
- `IMPLEMENTATION_TASKS/system-one-semantic-workload-closure-eval.md`
- `IMPLEMENTATION_TASKS/task-diff-preexisting-untracked-baseline.md`
- `IMPLEMENTATION_TASKS/quality-surface-implicit-tool-config.md`
- `IMPLEMENTATION_TASKS/repo-search-result-context-budget.md`
- `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md`
- `IMPLEMENTATION_TASKS/repo-search-semantic-objective-query-separation.md`
- `IMPLEMENTATION_TASKS/parent-evidence-locator-identity.md`
- `IMPLEMENTATION_TASKS/parent-review-evidence-coverage-accumulation.md`
- `IMPLEMENTATION_TASKS/codex-install-interruption-recovery.md`
- `IMPLEMENTATION_TASKS/cli-install-interruption-recovery.md`
- `IMPLEMENTATION_TASKS/publication-sequence-roundtrip-evaluation.md`
- `IMPLEMENTATION_TASKS/parent-session-rotation-cost-evaluation.md`
- `IMPLEMENTATION_TASKS/codex-usage-comparability-evaluation.md`
- `IMPLEMENTATION_TASKS/repository-review-artifact-cleanup.md`
- `IMPLEMENTATION_TASKS/resume-prompt-stop-reason-decoupling.md`
- `IMPLEMENTATION_TASKS/zai-five-hour-resume-grace-retry.md`
- `IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md`
- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT

- `IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`
- `IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`
- `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`
- `IMPLEMENTATION_TASKS/101-live-sol-ab.md`
- `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`
- `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`
- `IMPLEMENTATION_TASKS/104-test-impact-selection.md`
- `IMPLEMENTATION_TASKS/106-review-call-reduction.md`
