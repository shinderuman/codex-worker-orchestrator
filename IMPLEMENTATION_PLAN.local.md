# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`を正とする。通常taskの完了証跡はGit、CI、bundle / telemetryから回収し、`IMPLEMENTATION_HISTORY.md`は将来taskが明示参照する非diffのcross-task decisionだけを保持する。このfileへtask詳細・Web GPTの評価/Issue管理状態・完了chronologyを複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/remote-push-approval-regression.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/autonomous-development-harness.md`
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
- local `main`の`6f85b3bb7938eabc38de01b7f066dc6f7ee120fc`に対する通常`git push origin main`はremote到達前の実行環境安全審査で拒否された。その後external fast-forwardによりcurrent `main` / `origin/main`は同commitを祖先に持つ`4aa3db0d688c726ac53b172835af8d93e831a86c`へ同期済み。過去事象は旧禁止regime 5件と恒久authority後regression 2件へ再分類済み。現在会話の明示承認によりexact host prefix `git -C /Users/shinderumanm/src/codex-config push origin main`を永続登録し、追加承認なしの同一command連続2回成功をproducer evidenceとして得たため、`remote-push-approval-regression.md`を親Goで完了へ進める。
- thin Go entrypointと`glm-worker` machine JSON outputを既存quality ownerでmechanical enforcementする変更をlocal mainへ統合済み。`autonomous-development-harness.md`はbug修正を優先するユーザー指示に従いNEXT先頭へ戻し、同taskのworker/model callは開始していない。
- retry edge・曖昧関係・requested wait分類を原本trace付きで辿る`analysis-index.json` v3とvalidationをcurrent mainへ統合済み。`021-conditional-improvements.md`のparent decision gateは新規採用taskなしで完了。
- authorization不整合調査はread-only observationと親No-Goまで完了済み。production修正を行わない判断と外部修正境界は削除済みtask fileのGit履歴および保存bundle evidenceから回収する。恒久的な自動再開許可は`IMPLEMENTATION_RULES.md`を正とする。
- F1-F10 hardening、parent action/handoff、typed parent-capability validation、repo-search、Codex analysis bundle、target-repository lean context、wait instruction削減、actionable containment denial、bounded parent finalization surface、commentlint sandbox-safe launcher、parent authority bootstrapはcurrent mainへ統合済み。詳細なcommit・validationはGit / CI、runtime・model evidenceはbundle / telemetryを正とする。
- preserved boundary: machine-readable lifecycle、snapshot/validation authority、parent-managed metadata guard、GLM commit/push禁止、parent Codex semantic authority、normal fast-forward Git safety、Direct Codex対orchestratedのCodex Reduction / Quality Delta最上位評価を維持する。
