# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。現在のbranch・HEAD・dirty stateも複製せず、Git現物と`glm-worker --project-state`を正とする。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- [`IMPLEMENTATION_TASKS/parent-only-root-resume-and-amendment-admission.md`](IMPLEMENTATION_TASKS/parent-only-root-resume-and-amendment-admission.md)

## NEXT（優先順）

- [`IMPLEMENTATION_TASKS/request-publication-terminal-success.md`](IMPLEMENTATION_TASKS/request-publication-terminal-success.md)
- [`IMPLEMENTATION_TASKS/request-advancement-authority.md`](IMPLEMENTATION_TASKS/request-advancement-authority.md)
- [`IMPLEMENTATION_TASKS/nested-repository-executable-source-identity.md`](IMPLEMENTATION_TASKS/nested-repository-executable-source-identity.md)
- [`IMPLEMENTATION_TASKS/staging-request-unique-publication.md`](IMPLEMENTATION_TASKS/staging-request-unique-publication.md)
- [`IMPLEMENTATION_TASKS/staging-response-encoded-byte-budget.md`](IMPLEMENTATION_TASKS/staging-response-encoded-byte-budget.md)
- [`IMPLEMENTATION_TASKS/scheduled-pause-error-preservation.md`](IMPLEMENTATION_TASKS/scheduled-pause-error-preservation.md)
- [`IMPLEMENTATION_TASKS/publication-abort-recovery-action.md`](IMPLEMENTATION_TASKS/publication-abort-recovery-action.md)
- [`IMPLEMENTATION_TASKS/parent-evidence-diff-execution-bounds.md`](IMPLEMENTATION_TASKS/parent-evidence-diff-execution-bounds.md)
- [`IMPLEMENTATION_TASKS/quality-wiring-package-bindings.md`](IMPLEMENTATION_TASKS/quality-wiring-package-bindings.md)
- [`IMPLEMENTATION_TASKS/controlled-commentlint-source-proof.md`](IMPLEMENTATION_TASKS/controlled-commentlint-source-proof.md)
- [`IMPLEMENTATION_TASKS/darwin-ci-acceptance-closure.md`](IMPLEMENTATION_TASKS/darwin-ci-acceptance-closure.md)
- [`IMPLEMENTATION_TASKS/review-source-equivalence-same-head.md`](IMPLEMENTATION_TASKS/review-source-equivalence-same-head.md)
- [`IMPLEMENTATION_TASKS/validation-input-git-environment-isolation.md`](IMPLEMENTATION_TASKS/validation-input-git-environment-isolation.md)
- [`IMPLEMENTATION_TASKS/repo-search-unmerged-index-diagnostic.md`](IMPLEMENTATION_TASKS/repo-search-unmerged-index-diagnostic.md)
- [`IMPLEMENTATION_TASKS/plan-spacing-supported-task-forms.md`](IMPLEMENTATION_TASKS/plan-spacing-supported-task-forms.md)
- [`IMPLEMENTATION_TASKS/diff-unsupported-platform-build.md`](IMPLEMENTATION_TASKS/diff-unsupported-platform-build.md)
- [`IMPLEMENTATION_TASKS/peak-pause-civil-time-boundaries.md`](IMPLEMENTATION_TASKS/peak-pause-civil-time-boundaries.md)
- [`IMPLEMENTATION_TASKS/docker-cli-deny-rule-coverage.md`](IMPLEMENTATION_TASKS/docker-cli-deny-rule-coverage.md)
- [`IMPLEMENTATION_TASKS/codex-efficiency-review-followup.md`](IMPLEMENTATION_TASKS/codex-efficiency-review-followup.md)
- [`IMPLEMENTATION_TASKS/normal-workflow-organic-e2e-verification.md`](IMPLEMENTATION_TASKS/normal-workflow-organic-e2e-verification.md)
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
