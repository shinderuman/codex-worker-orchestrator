# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。現在のbranch・HEAD・dirty stateも複製せず、Git現物と`glm-worker --project-state`を正とする。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- [`IMPLEMENTATION_TASKS/rollout-usage-gap-accounting.md`](IMPLEMENTATION_TASKS/rollout-usage-gap-accounting.md)

## NEXT（優先順）

- [`IMPLEMENTATION_TASKS/ab-worktree-physical-identity.md`](IMPLEMENTATION_TASKS/ab-worktree-physical-identity.md)
- [`IMPLEMENTATION_TASKS/plan-entry-grammar-equivalence.md`](IMPLEMENTATION_TASKS/plan-entry-grammar-equivalence.md)
- [`IMPLEMENTATION_TASKS/clean-cutover-live-command-proof.md`](IMPLEMENTATION_TASKS/clean-cutover-live-command-proof.md)
- [`IMPLEMENTATION_TASKS/export-fixture-and-scenario-ownership.md`](IMPLEMENTATION_TASKS/export-fixture-and-scenario-ownership.md)
- [`IMPLEMENTATION_TASKS/session-rotation-handoff-test-locality.md`](IMPLEMENTATION_TASKS/session-rotation-handoff-test-locality.md)
- [`IMPLEMENTATION_TASKS/parent-only-sync-test-locality.md`](IMPLEMENTATION_TASKS/parent-only-sync-test-locality.md)
- [`IMPLEMENTATION_TASKS/remove-backward-compatibility-surfaces.md`](IMPLEMENTATION_TASKS/remove-backward-compatibility-surfaces.md)

## BLOCKED / USER_PERMISSION_WAIT

- [`IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`](IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md)
- [`IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`](IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md)
- [`IMPLEMENTATION_TASKS/101-live-sol-ab.md`](IMPLEMENTATION_TASKS/101-live-sol-ab.md)
- [`IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`](IMPLEMENTATION_TASKS/103-compaction-threshold-change.md)
- [`IMPLEMENTATION_TASKS/104-test-impact-selection.md`](IMPLEMENTATION_TASKS/104-test-impact-selection.md)
- [`IMPLEMENTATION_TASKS/106-review-call-reduction.md`](IMPLEMENTATION_TASKS/106-review-call-reduction.md)
- [`IMPLEMENTATION_TASKS/102-model-routing-redesign.md`](IMPLEMENTATION_TASKS/102-model-routing-redesign.md)
