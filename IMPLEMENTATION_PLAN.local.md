# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。現在のbranch・HEAD・dirty stateも複製せず、Git現物と`glm-worker --project-state`を正とする。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/normal-workflow-snapshot-stop-regression.md`

## NEXT（優先順）


- `IMPLEMENTATION_TASKS/cli-positive-task-start-admission.md`
- `IMPLEMENTATION_TASKS/dogfood-bundle-three-task-audit.md`
- `IMPLEMENTATION_TASKS/repo-search-result-context-budget.md`
- `IMPLEMENTATION_TASKS/repo-search-semantic-objective-query-separation.md`
- `IMPLEMENTATION_TASKS/codex-efficiency-intermediate-checkpoint.md`
- `IMPLEMENTATION_TASKS/publication-sequence-roundtrip-evaluation.md`
- `IMPLEMENTATION_TASKS/parent-session-rotation-cost-evaluation.md`
- `IMPLEMENTATION_TASKS/codex-usage-comparability-evaluation.md`
- `IMPLEMENTATION_TASKS/repository-review-artifact-cleanup.md`
- `IMPLEMENTATION_TASKS/post-105-codex-efficiency-reevaluation.md`
- `IMPLEMENTATION_TASKS/022-final-verification.md`

## BLOCKED / USER_PERMISSION_WAIT
- `IMPLEMENTATION_TASKS/user-requirement-ingress-binding.md`
- `IMPLEMENTATION_TASKS/claude-cli-runtime-preflight-reevaluation.md`
- `IMPLEMENTATION_TASKS/101-live-sol-ab.md`
- `IMPLEMENTATION_TASKS/103-compaction-threshold-change.md`
- `IMPLEMENTATION_TASKS/104-test-impact-selection.md`
- `IMPLEMENTATION_TASKS/106-review-call-reduction.md`
- `IMPLEMENTATION_TASKS/102-model-routing-redesign.md`
- `IMPLEMENTATION_TASKS/configurable-peak-pause-windows.md`

## 優先順位の判断
現在のACTIVEは通常経路のsnapshot不整合停止の修正とする。Bundle復旧後の新セッションで実施する。CLI admissionをその次に維持し、その通常開発Task完了後に三つのBundleを再監査する。誤起動した旧CLI Taskと旧repo-search評価のcheckpoint・証跡は保全する。


- バグ修正を先に行い、その後をCodex Reductionの作業にする。通常経路のsnapshot不整合停止の修正をACTIVE、その次にCLIの誤Task-start回帰を置く。両Task完了後の三Bundle再監査をその次に登録し、repo-searchのcontext budget / dedup評価とsemantic objective / lexical query分離評価を続ける。
- 中間評価、publication / rotation費用評価、usage比較可能性の確認はrepo-search評価より後に置く。実際の採否に必要な測定は各TaskのContract内で行い、未成立の依存を理由に別の評価Taskを先行必須へしない。
- cleanupは対応表に列挙されたTaskの完了後、post-105再評価は022直前、022は他の実行可能Taskがなくなった後に行う。並べ替えでこれらの条件を解除しない。
- BLOCKEDは条件付きの実行候補であり、並べ替えだけで解除しない。要求保存だけのoptional pauseとprovider cost最適化は、Codex ReductionとQuality Deltaの評価より後順位にする。
