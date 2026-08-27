# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/installer-missing-dependency-brew-hint.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/instruction-surface-ownership.md`
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

- branch: `main`
- accepted main baseline: `2c8bf5fe22b529dd446c65bd43b6a2289819730d`
- implementation boundary: PR #3 `6f08b301e9c4e1444af80c9b9ee646eb778a3baa`でrepository quality linterを導入し、PR #4 `b5a4f2e9fd4a3199470e9df0759c8b969f6813c5`でrepository-wide quality enforcement、PR #5 `21cb94b5f4b9800f8092f2e2f5afc276ad37ae62`でPlan / Tasks / History reconciliation、PR #6 `2c8bf5fe22b529dd446c65bd43b6a2289819730d`で欠落した`branch: main` metadataを復元してmainへSquash Merge済み
- preserved boundary: wake coalescing、machine output、safe-stop/resume、provider accounting、parent-managed metadata guard、GLM commit/push禁止、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持
- current implementation: `gpt/installer-brew-hint-v2`でHomebrew formulaを持つ必須commandの欠落時だけ`brew install <formula>`案内を追加し、既存install smokeへpositive/negative regressionを追加する
- merge boundary: current implementationはmainへ直接pushせずmain向けPRで停止し、Squash Mergeはユーザーが行う

## 現在の停止理由

PR #6はmainへSquash Merge済み。ACTIVE taskのproduction変更と回帰testを専用branchで作成中であり、mainへmergeされるまでlocal accepted-main runtime install / task completionは行わない。

## 次の親Codex操作

current implementation差分をreviewしてmain向けPRとして提示する。ユーザーのSquash Merge後に最新mainをlocalへ同期して`./install.sh`の本配置とinstalled/source一致を確認し、task acceptance成立後にHistory移行・task file削除・Planの次ACTIVE昇格を行う。
