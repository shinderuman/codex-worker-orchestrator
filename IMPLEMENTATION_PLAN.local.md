# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/greptile-low-cost-scheduled-dispatch.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/markdown-context-footprint-reduction.md`
- `IMPLEMENTATION_TASKS/commit-authorization-source-recognition.md`
- `IMPLEMENTATION_TASKS/install-smoke-loop-cost-reduction.md`
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
- implementation baseline: 親Codex 5h Limit自動再開と完了metadataを同一commitへ同期したcurrent HEAD
- implementation boundary: read-only `glm-worker --codex-limit`、300分windowの位置非依存選別、Codex Desktop既存scheduler/task送信を使うwake rule、project所属Luna Low専用task、thread固有automation identityを実装。全Go gate、独立review、親差戻し、実Desktop identity/target/time verify、full install smokeを完了
- preserved boundary: Greptile日次reviewの機械dispatchをrepository固有ruleのままLuna Low専用taskへ縮小する要求をACTIVE化。Markdown runtime context削減を次点NEXT、commit authorization source false negative、install smoke loop costを後続NEXTとして保持。external feasibility dispatch gate、safe-stop/isolation境界は不変、不要stashなし
- push: 各親commit後の`refs/heads/main`とGreptile正常review後の`refs/heads/codex/greptile-reviewed`だけを親Codexが通常fast-forwardする。GLM push、force/non-fast-forward、他refへのpushは禁止

## 現在の停止理由

Greptile日次reviewのLuna Low dispatch化を開始できる境界。既存Greptile CLI reviewは実行せずcreditを消費しない。

## 次の親Codex操作

既存Greptile automation/task実体とrepository ruleを確認し、`codex-config` project所属のLuna Low専用taskを作成してscheduler prompt/targetを機械dispatch・親送信だけへ切り替える。finding採否・task化は親Codexへ残す。
