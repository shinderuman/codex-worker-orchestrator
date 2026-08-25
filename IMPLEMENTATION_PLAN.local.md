# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/009-worker-call-outliers.md`

## NEXT（優先順）

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
- implementation baseline: Task 015 fixed Eval corpus・完了metadata同期済みのcurrent HEAD
- implementation boundary: 4 case offline contract照合、5時間limit incident統合、producer field/schema/timingの実証authority固定、配線testとinstall smoke修復を完了済み
- preserved boundary: behavioral evidenceはsafe interruption taskで実装委譲前の実Claude PoC・親Go判断・後続production実装順序を確認済み。Greptile日次scheduled review、external feasibility dispatch gate、safe-stop/isolation境界は不変、不要stashなし
- push: GLM push、force/non-fast-forward、他refへのpushは禁止。Greptile運用に必要な`refs/heads/main`と`refs/heads/codex/greptile-reviewed`の親Codexによる通常fast-forwardだけ許可

## 現在の停止理由

Task 009 worker call外れ値観測がACTIVEへ昇格し、既存telemetryから反復可能なoutlier分類と最上位Evalへの改善候補を評価する開始境界にある。

## 次の親Codex操作

Task 009の要求正本と既存telemetry authorityを確認し、追加AI callを最小化した外れ値分析を同taskのGLM workerへ委譲する。pushはGreptile運用のremote main/checkpoint通常fast-forward以外禁止。
