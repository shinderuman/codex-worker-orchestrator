# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/external-review-pr-344.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/codex-review-gap-telemetry.md`
- `IMPLEMENTATION_TASKS/structured-validation-gate-telemetry.md`
- `IMPLEMENTATION_TASKS/packet-validation-correction-recovery.md`
- `IMPLEMENTATION_TASKS/telemetry-history-compact-summary.md`
- `IMPLEMENTATION_TASKS/105-session-rotation.md`
- `IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md`
- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT

- `IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`
- `IMPLEMENTATION_TASKS/desktop-terminal-payload-double-render-boundary.md`
- `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`
- `IMPLEMENTATION_TASKS/101-live-sol-ab.md`
- `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`
- `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`
- `IMPLEMENTATION_TASKS/104-test-impact-selection.md`
- `IMPLEMENTATION_TASKS/106-review-call-reduction.md`

## 現在のGit境界

- branch: `main`
- 2026-09-03のCodex/GLM telemetry調査に基づく観測・復旧改善を022の前段へ追加した。実装開始前のtask corpus監査で、2026-08-31の`171c0ff`が4件の実機Acceptance trackerをPlanから外した一方でtask fileを残し、現行validatorもPlanからtaskへの片方向確認しか行わないため未schedule状態が継続していたことを確認した。親Codexがcommit・削除済みHistory・bundle/telemetry・後続実装を再照合し、4件はいずれも後続実機evidenceまで含めてCOMPLETE、production再実装不要と判断して完了同期した。逆方向closure guardを実装し、working-tree harnesslint・final HEAD・`--project-state`でTask corpusとPlan scheduleのexactly-once閉包を検証する。親Codex token attributionを実装し、task execution / parent finalization別の実token・turn/tool/compaction/output bytesを追加AI callなしで取得可能にした。telemetry history queryを実装し、current/history・task・期間filterとversion/schema cohort別coverage/outlierをbounded JSONで取得可能にした。105完了後は親Codexが開始時と同等のtelemetry再評価を行い、Findingsがあれば022以前へ追加して作業サイクルを継続する。最上位目的はCodex / Sol側の実消費削減であり、GLM token削減だけを理由に親Codex tokenやSol判断回数を増やさない。test省略、review省略、compaction閾値変更、model routingは追加観測だけで自動採用せずBLOCKEDを維持する。
- retry edge・曖昧関係・requested wait分類を原本trace付きで辿る`analysis-index.json` v3とvalidationをcurrent mainへ統合済み。`021-conditional-improvements.md`のparent decision gateは新規採用taskなしで完了。
- authorization不整合調査はread-only observationと親No-Goまで完了済み。production修正を行わない判断と外部修正境界は削除済みtask fileのGit履歴および保存bundle evidenceから回収する。恒久的な自動再開許可は`IMPLEMENTATION_RULES.md`を正とする。
- F1-F10 hardening、parent action/handoff、typed parent-capability validation、repo-search、Codex analysis bundle、target-repository lean context、wait instruction削減、actionable containment denial、bounded parent finalization surface、commentlint sandbox-safe launcher、parent authority bootstrapはcurrent mainへ統合済み。詳細なcommit・validationはGit / CI、runtime・model evidenceはbundle / telemetryを正とする。
- preserved boundary: machine-readable lifecycle、snapshot/validation authority、parent-managed metadata guard、GLM commit/push禁止、parent Codex semantic authority、normal fast-forward Git safety、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持する。
