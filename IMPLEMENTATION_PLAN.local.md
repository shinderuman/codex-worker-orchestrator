# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。現在のbranch・HEAD・dirty stateも複製せず、Git現物と`glm-worker --project-state`を正とする。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/go-suite-execution-time-reduction.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/execution-worktree-architecture-review.md`
- `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md`
- `IMPLEMENTATION_TASKS/active-only-minimal-instruction-workflow.md`
- `IMPLEMENTATION_TASKS/dogfood-bundle-three-task-audit.md`
- `IMPLEMENTATION_TASKS/self-referential-test-redesign.md`
- `IMPLEMENTATION_TASKS/self-referential-test-enforcement-owner.md`
- `IMPLEMENTATION_TASKS/self-referential-test-prevention-harness.md`
- `IMPLEMENTATION_TASKS/machine-git-evidence-raw-diff-contract.md`
- `IMPLEMENTATION_TASKS/parent-evidence-diff-identity.md`
- `IMPLEMENTATION_TASKS/task-admission-harness-scope.md`
- `IMPLEMENTATION_TASKS/repo-search-index-flag-cache-correctness.md`
- `IMPLEMENTATION_TASKS/parent-evidence-serialized-output-budget.md`
- `IMPLEMENTATION_TASKS/observation-preparation-cancellation.md`
- `IMPLEMENTATION_TASKS/task-requirement-reference-closure.md`
- `IMPLEMENTATION_TASKS/repo-search-result-context-budget.md`
- `IMPLEMENTATION_TASKS/repo-search-semantic-objective-query-separation.md`
- `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md`
- `IMPLEMENTATION_TASKS/controller-git-archive-cost-evaluation.md`
- `IMPLEMENTATION_TASKS/publication-sequence-roundtrip-evaluation.md`
- `IMPLEMENTATION_TASKS/parent-session-rotation-cost-evaluation.md`
- `IMPLEMENTATION_TASKS/codex-usage-comparability-evaluation.md`
- `IMPLEMENTATION_TASKS/repository-review-artifact-cleanup.md`
- `IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md`
- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT

- `IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`
- `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`
- `IMPLEMENTATION_TASKS/101-live-sol-ab.md`
- `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`
- `IMPLEMENTATION_TASKS/104-test-impact-selection.md`
- `IMPLEMENTATION_TASKS/106-review-call-reduction.md`
- `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`
- `IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`
