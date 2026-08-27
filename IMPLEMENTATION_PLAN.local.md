# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/prose-semantic-guard-migration.md`

## NEXT（優先順）
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

- branch: `hardening/agent-guardrails`
- accepted main baseline: `815efc8f6601d344a93e8fa213d0227306db1f0d`
- implementation boundary: PR #3 `6f08b301e9c4e1444af80c9b9ee646eb778a3baa`でrepository quality linter、PR #4 `b5a4f2e9fd4a3199470e9df0759c8b969f6813c5`でrepository-wide quality enforcement、PR #5 `21cb94b5f4b9800f8092f2e2f5afc276ad37ae62`でmetadata reconciliation、PR #6 `2c8bf5fe22b529dd446c65bd43b6a2289819730d`でPlan branch metadata修復、PR #7 `a111e0adc2d4f299e677f85148f34906d74e9c2c`でinstaller Homebrew hint、PR #9 `84e3baf18fc8d1c722682288c91ceb1623fd4e20`で恒久Repository Lint、PR #10 `815efc8f6601d344a93e8fa213d0227306db1f0d`でGreptile / CodeRabbit自動review停止をmainへ反映済み。hardening integrationではF1 `instruction-surface-ownership`をPR #8のSquash Merge commit `621a6c6cec384b8ef0488796b6488085a77a6b5d`、F2 `unknown-production-surface-risk`をPR #12のSquash Merge commit `f33203418c33d85baee34fa69225ef954e7996aa`、F3 `glm-git-authority-enforcement`をPR #13のSquash Merge commit `627e0dcfa15148a68684c3ff9008a4658c5a2615`、F4 `quality-evidence-mutation-risk`をPR #14のSquash Merge commit `87af3f4700f5dd8220c582efa62f18f561c33c20`、F5 `deterministic-rule-activation`をPR #15のSquash Merge commit `9c33ae4784b6cfe33cf63de8d415fac38cad9fae`、F6 `sol-decision-boundary-enforcement`をPR #16のSquash Merge commit `16d975e02dc37b2279cce441d09bebc85adef3f3`として完了済み
- preserved boundary: wake coalescing、machine output、safe-stop/resume、provider accounting、parent-managed metadata guard、GLM commit/push禁止、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持
- current implementation: F6 `sol-decision-boundary-enforcement`はPR #16のSquash Merge commit `16d975e02dc37b2279cce441d09bebc85adef3f3`としてCOMPLETE。F7 `prose-semantic-guard-migration`がACTIVE
- merge boundary: hardening taskは専用task branchへ細かくcheckpoint commit/pushし、taskごとに`hardening/agent-guardrails`向けPRをSquash Mergeして1 task = 1 integration commitとする。hardening campaign完了時はintegrationからmainへSquashしないmergeを行う

## 現在の停止理由

F7開始を妨げるdependency / permission waitはない。

## 次の親Codex操作

latest integrationをF7 branchへ履歴を書き換えず同期し、Rules / Plan / ACTIVE F7 taskを再読する。残存prose pin corpusをTrack A generic behavior/typed invariant、Track B repository-specific prose guard、正当なwording-contractへ棚卸しし、主要semanticをdeterministic guardへ移行するF7 production実装を進める。
