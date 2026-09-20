# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は`IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。現在のbranch・HEAD・dirty stateも複製せず、Git現物と`glm-worker --project-state`を正とする。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/install-smoke-claude-settings-env-isolation.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/machine-enforced-control-authority-legitimacy.md`
- `IMPLEMENTATION_TASKS/machine-negative-result-authority.md`
- `IMPLEMENTATION_TASKS/canonical-authority-bootstrap-enforcement.md`
- `IMPLEMENTATION_TASKS/staged-parent-action-no-reread-regression.md`
- `IMPLEMENTATION_TASKS/system-one-dogfood-evidence-shadow-eval.md`
- `IMPLEMENTATION_TASKS/task-progress-observability.md`
- `IMPLEMENTATION_TASKS/parent-fix-origin-cause-staged-transport.md`
- `IMPLEMENTATION_TASKS/machine-visible-improvement-signal-disposition.md`
- `IMPLEMENTATION_TASKS/terminal-result-budgeted-projection.md`
- `IMPLEMENTATION_TASKS/parent-usage-interleaved-turn-ambiguity.md`
- `IMPLEMENTATION_TASKS/parent-wait-custom-tool-observability.md`
- `IMPLEMENTATION_TASKS/task-stats-archive-skip-observability.md`
- `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md`
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
