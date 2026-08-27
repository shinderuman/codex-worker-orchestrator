# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/instruction-surface-ownership.md`

## NEXT（優先順）

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

- accepted main baseline: `b5a4f2e9fd4a3199470e9df0759c8b969f6813c5`
- reconciliation branch: `gpt/implementation-metadata-reconcile`。latest accepted mainから作成したmetadata-only branchであり、accepted baselineと混同しない
- implementation boundary: PR #3 `6f08b301e9c4e1444af80c9b9ee646eb778a3baa`でrepository quality linterを導入し、PR #4 `b5a4f2e9fd4a3199470e9df0759c8b969f6813c5`でrepository-wide lint負債除去、reviewer前machine quality gate、accepted quality surface snapshot / worker自己改変fail-closed、scenario/prose/test負債整理までmainへSquash Merge済み
- preserved boundary: wake coalescing、machine output、safe-stop/resume、provider accounting、parent-managed metadata guard、GLM commit/push禁止、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持
- reconciliation scope: Plan / Tasks / Historyのcurrent implementation同期だけ。production code・runtime behaviorは変更しない
- merge boundary: metadata reconciliationはmainへ直接pushせずmain向けPRで停止し、Squash Mergeはユーザーが行う

## 現在の停止理由

PR #4までaccepted mainへSquash Merge済み。local runtime install / Codex+GLM acceptanceへ進む前に、PR #3/#4で完了・吸収・前提変更になったtaskをcurrent mainへ同期するmetadata reconciliationをユーザー指示で先行している。reconciliation PR作成後はユーザーのSquash Mergeまで停止し、ACTIVE taskのproduction実装は開始しない。

## 次の親Codex操作

metadata reconciliation PRがユーザーにSquash Mergeされた後、最新accepted mainをlocalへ同期して`./install.sh`を実行し、source HEAD・installed `glm-worker`・managed instructions・installed configの一致を確認する。そのinstalled状態でworker dispatch、machine quality gate、reviewer routing、quality surface protection、resume/stop、machine output、通常task lifecycleを確認し、Codex / GLMが新quality policy下で実運用できることをacceptしてからACTIVE taskへ戻る。
