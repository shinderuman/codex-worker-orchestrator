# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/autonomous-development-harness.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT

- `IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`
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
- `autonomous-development-harness.md`のread-only architecture評価と独立reviewを完了し、新規独立Harness・Planner・state model・schedulerはNo-Go、運用は現状維持と親Codexが判断済み。production変更と022前の追加taskはなく、task完了metadata同期後は022を開始せず停止する。
- retry edge・曖昧関係・requested wait分類を原本trace付きで辿る`analysis-index.json` v3とvalidationをcurrent mainへ統合済み。`021-conditional-improvements.md`のparent decision gateは新規採用taskなしで完了。
- authorization不整合調査はread-only observationと親No-Goまで完了済み。production修正を行わない判断と外部修正境界は削除済みtask fileのGit履歴および保存bundle evidenceから回収する。恒久的な自動再開許可は`IMPLEMENTATION_RULES.md`を正とする。
- F1-F10 hardening、parent action/handoff、typed parent-capability validation、repo-search、Codex analysis bundle、target-repository lean context、wait instruction削減、actionable containment denial、bounded parent finalization surface、commentlint sandbox-safe launcher、parent authority bootstrapはcurrent mainへ統合済み。詳細なcommit・validationはGit / CI、runtime・model evidenceはbundle / telemetryを正とする。
- preserved boundary: machine-readable lifecycle、snapshot/validation authority、parent-managed metadata guard、GLM commit/push禁止、parent Codex semantic authority、normal fast-forward Git safety、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持する。
