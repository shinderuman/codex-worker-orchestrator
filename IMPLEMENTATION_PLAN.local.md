# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/safe-interruption-task-suspension.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/interrupted-task-checkout-isolation.md`
- `IMPLEMENTATION_TASKS/external-feasibility-dispatch-gate.md`
- `IMPLEMENTATION_TASKS/015-fixed-eval-corpus.md`
- `IMPLEMENTATION_TASKS/009-worker-call-outliers.md`
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
- implementation baseline: 実Claude interrupt feasibility完了commit（current HEAD）
- implementation boundary: process safe-stopと別task checkout/state隔離を分離。実Claude CLI 2.1.226のprocess group cleanup・partial session同一ID resumeを確認し、safe-stop production実装へSol Go判断済み
- preserved boundary: external feasibility gateの中断diffはmessage identity付きstash 2件へ可逆保全し、orphan process group終了済み
- push: 禁止

## 現在の停止理由

blockerなし。safe-stopの外部critical assumptionは解消済みで、taskにproduction最小設計・禁止事項・test境界を固定済み。

## 次の親Codex操作

ACTIVE safe interruption taskを要求正本として、単一目的local `--stop` handshake、Claude process-group cleanup、interrupted checkpoint/status、同一session resume、machine contract・test・instruction更新をGLMへ委譲する。別task checkout隔離は混ぜない。pushしない。
