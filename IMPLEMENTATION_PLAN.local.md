# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/quality-surface-authorization-convergence.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/finalize-check-module-cwd.md`
- `IMPLEMENTATION_TASKS/normal-push-preflight-reduction.md`
- `IMPLEMENTATION_TASKS/parent-capability-validation-churn.md`
- `IMPLEMENTATION_TASKS/worker-repo-search-active-task-seed.md`
- `IMPLEMENTATION_TASKS/reviewer-repo-search-parent-metadata.md`
- `IMPLEMENTATION_TASKS/bundle-task-diff-committed-files.md`
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
- current accepted implementation head before this queue maintenance: `612a8c799c2c8a7ffb7fac9b5c78fcfb97ef9ee9`
- F1-F10 hardening、parent action/handoff、typed parent-capability validation、repo-search、Codex analysis bundle、target-repository lean context、wait instruction削減、actionable containment denial、bounded parent finalization surface、commentlint sandbox-safe launcherはすべてcurrent mainへ統合済み。詳細なcommit・validation・escaped原因は`IMPLEMENTATION_HISTORY.md`を正とする。
- preserved boundary: machine-readable lifecycle、snapshot/validation authority、parent-managed metadata guard、GLM commit/push禁止、parent Codex semantic authority、normal fast-forward Git safety、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持する。

## 現在の停止理由

commentlint dogfoodと外部ログ監査を完了し、実装が必要なproduction findingだけをCodex実装taskへ分離してqueueした。Web GPTの測定・Acceptance trackerはこのPlanへ入れない。旧Codex長期threadには削除済みwait instruction等のstale contextが残るため、ユーザー指示により次の開発task開始時だけ新規Codex sessionへ移行し、そのsessionを以後の長期開発sessionとして継続する。旧sessionへ戻らず、定期session rotationも導入しない。

## 次の親Codex操作

新規Codex sessionでRules / Plan / ACTIVE `IMPLEMENTATION_TASKS/quality-surface-authorization-convergence.md`を読み、通常Codex + GLM workflowでこの1 taskを開始する。完了後も同じCodex sessionを継続し、Planの次taskへ進む。各taskのbundle/parent/Guardian evidenceは外部監査でCodex Reduction / Quality Deltaを評価できる形で保持する。
