# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。現在のbranch・HEAD・dirty stateも複製せず、Git現物と`glm-worker --project-state`を正とする。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- [`IMPLEMENTATION_TASKS/canonical-publication-source-continuity.md`](IMPLEMENTATION_TASKS/canonical-publication-source-continuity.md)

## NEXT（優先順）


- [`IMPLEMENTATION_TASKS/task-revision-worker-admission.md`](IMPLEMENTATION_TASKS/task-revision-worker-admission.md)
- [`IMPLEMENTATION_TASKS/codex-efficiency-followup-checkpoint.md`](IMPLEMENTATION_TASKS/codex-efficiency-followup-checkpoint.md)
- [`IMPLEMENTATION_TASKS/post-publication-terminal-handoff-scope-failure.md`](IMPLEMENTATION_TASKS/post-publication-terminal-handoff-scope-failure.md)
- [`IMPLEMENTATION_TASKS/parent-inflight-wait-admission.md`](IMPLEMENTATION_TASKS/parent-inflight-wait-admission.md)
- [`IMPLEMENTATION_TASKS/validation-failure-retry-admission.md`](IMPLEMENTATION_TASKS/validation-failure-retry-admission.md)
- [`IMPLEMENTATION_TASKS/codex-usage-comparability-evaluation.md`](IMPLEMENTATION_TASKS/codex-usage-comparability-evaluation.md)
- [`IMPLEMENTATION_TASKS/app-controller-transition-owner.md`](IMPLEMENTATION_TASKS/app-controller-transition-owner.md)
- [`IMPLEMENTATION_TASKS/app-analysis-responsibility-owner.md`](IMPLEMENTATION_TASKS/app-analysis-responsibility-owner.md)
- [`IMPLEMENTATION_TASKS/reviewer-packet-correction-reentry.md`](IMPLEMENTATION_TASKS/reviewer-packet-correction-reentry.md)
- [`IMPLEMENTATION_TASKS/machine-git-evidence-raw-diff-contract.md`](IMPLEMENTATION_TASKS/machine-git-evidence-raw-diff-contract.md)
- [`IMPLEMENTATION_TASKS/parent-evidence-diff-identity.md`](IMPLEMENTATION_TASKS/parent-evidence-diff-identity.md)
- [`IMPLEMENTATION_TASKS/parent-evidence-serialized-output-budget.md`](IMPLEMENTATION_TASKS/parent-evidence-serialized-output-budget.md)
- [`IMPLEMENTATION_TASKS/finding-source-revision-disposition.md`](IMPLEMENTATION_TASKS/finding-source-revision-disposition.md)
- [`IMPLEMENTATION_TASKS/communication-transport-unification.md`](IMPLEMENTATION_TASKS/communication-transport-unification.md)
- [`IMPLEMENTATION_TASKS/primary-promotion-tree-transitions.md`](IMPLEMENTATION_TASKS/primary-promotion-tree-transitions.md)
- [`IMPLEMENTATION_TASKS/validation-evidence-unreadable-seal.md`](IMPLEMENTATION_TASKS/validation-evidence-unreadable-seal.md)
- [`IMPLEMENTATION_TASKS/system-one-architecture-effectiveness-modernization.md`](IMPLEMENTATION_TASKS/system-one-architecture-effectiveness-modernization.md)
- [`IMPLEMENTATION_TASKS/publication-sequence-roundtrip-evaluation.md`](IMPLEMENTATION_TASKS/publication-sequence-roundtrip-evaluation.md)
- [`IMPLEMENTATION_TASKS/guardian-facade-review-effectiveness.md`](IMPLEMENTATION_TASKS/guardian-facade-review-effectiveness.md)
- [`IMPLEMENTATION_TASKS/self-referential-test-redesign.md`](IMPLEMENTATION_TASKS/self-referential-test-redesign.md)
- [`IMPLEMENTATION_TASKS/self-referential-test-enforcement-owner.md`](IMPLEMENTATION_TASKS/self-referential-test-enforcement-owner.md)
- [`IMPLEMENTATION_TASKS/self-referential-test-prevention-harness.md`](IMPLEMENTATION_TASKS/self-referential-test-prevention-harness.md)
- [`IMPLEMENTATION_TASKS/normal-workflow-organic-e2e-verification.md`](IMPLEMENTATION_TASKS/normal-workflow-organic-e2e-verification.md)
- [`IMPLEMENTATION_TASKS/git-authority-ref-change-evidence-scope.md`](IMPLEMENTATION_TASKS/git-authority-ref-change-evidence-scope.md)
- [`IMPLEMENTATION_TASKS/task-admission-harness-scope.md`](IMPLEMENTATION_TASKS/task-admission-harness-scope.md)
- [`IMPLEMENTATION_TASKS/task-requirement-reference-closure.md`](IMPLEMENTATION_TASKS/task-requirement-reference-closure.md)
- [`IMPLEMENTATION_TASKS/repo-search-index-flag-cache-correctness.md`](IMPLEMENTATION_TASKS/repo-search-index-flag-cache-correctness.md)
- [`IMPLEMENTATION_TASKS/observation-replay-fence-retention.md`](IMPLEMENTATION_TASKS/observation-replay-fence-retention.md)
- [`IMPLEMENTATION_TASKS/observation-preparation-cancellation.md`](IMPLEMENTATION_TASKS/observation-preparation-cancellation.md)
- [`IMPLEMENTATION_TASKS/install-smoke-lock-process-lifecycle.md`](IMPLEMENTATION_TASKS/install-smoke-lock-process-lifecycle.md)
- [`IMPLEMENTATION_TASKS/recovery-rejection-error-provenance.md`](IMPLEMENTATION_TASKS/recovery-rejection-error-provenance.md)
- [`IMPLEMENTATION_TASKS/repo-search-result-context-budget.md`](IMPLEMENTATION_TASKS/repo-search-result-context-budget.md)
- [`IMPLEMENTATION_TASKS/repo-search-semantic-objective-query-separation.md`](IMPLEMENTATION_TASKS/repo-search-semantic-objective-query-separation.md)
- [`IMPLEMENTATION_TASKS/parent-session-rotation-cost-evaluation.md`](IMPLEMENTATION_TASKS/parent-session-rotation-cost-evaluation.md)
- [`IMPLEMENTATION_TASKS/repository-review-artifact-cleanup.md`](IMPLEMENTATION_TASKS/repository-review-artifact-cleanup.md)
- [`IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md`](IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md)
- [`IMPLEMENTATION_TASKS/022-final-verification.md`](IMPLEMENTATION_TASKS/022-final-verification.md)

## BLOCKED / USER_PERMISSION_WAIT

- [`IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`](IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md)
- [`IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`](IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md)
- [`IMPLEMENTATION_TASKS/101-live-sol-ab.md`](IMPLEMENTATION_TASKS/101-live-sol-ab.md)
- [`IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`](IMPLEMENTATION_TASKS/103-compaction-threshold-change.md)
- [`IMPLEMENTATION_TASKS/104-test-impact-selection.md`](IMPLEMENTATION_TASKS/104-test-impact-selection.md)
- [`IMPLEMENTATION_TASKS/106-review-call-reduction.md`](IMPLEMENTATION_TASKS/106-review-call-reduction.md)
- [`IMPLEMENTATION_TASKS/102-model-routing-redesign.md`](IMPLEMENTATION_TASKS/102-model-routing-redesign.md)
- [`IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`](IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md)
