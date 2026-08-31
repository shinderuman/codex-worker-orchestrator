# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/021-conditional-improvements.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/authorization-context-inconsistency.md`
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
- 分析用索引task `70028e7d-ef53-4f70-8c7e-765d7fb78ee0`は実装・独立再review・Sol採用・現snapshotのfull test/race・parent acceptを通過してcommit済み。完了taskをHistoryへ移し、条件付き改善評価をACTIVEへ昇格した。昇格taskは未着手で、2026-09-01の依頼に従い開始せず停止する。恒久的な自動再開許可はIMPLEMENTATION_RULES.mdを正とする。
- F1-F10 hardening、parent action/handoff、typed parent-capability validation、repo-search、Codex analysis bundle、target-repository lean context、wait instruction削減、actionable containment denial、bounded parent finalization surface、commentlint sandbox-safe launcherはcurrent mainへ統合済み。詳細なcommit・validation・escaped原因は`IMPLEMENTATION_HISTORY.md`を正とする。
- preserved boundary: machine-readable lifecycle、snapshot/validation authority、parent-managed metadata guard、GLM commit/push禁止、parent Codex semantic authority、normal fast-forward Git safety、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持する。
