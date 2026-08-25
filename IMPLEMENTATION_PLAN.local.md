# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/source-comment-absolute-invariant.md`

## NEXT（優先順）

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
- implementation baseline: Task 009 worker call outlier可視化・完了metadata同期済みのcurrent HEAD
- implementation boundary: 保存telemetryからtask/phase/session/model別分布・task増幅・p95 outlierを追加AI callなしでJSON表示し、directory I/O failureと観測済みturn母集団の親review差戻しを修正済み
- preserved boundary: source comment absolute invariant taskを最優先ACTIVEへ昇格。commit authorization source false negative、install smoke loop costを後続NEXTとして保持。Greptile日次scheduled review、external feasibility dispatch gate、safe-stop/isolation境界は不変、不要stashなし
- push: GLM push、force/non-fast-forward、他refへのpushは禁止。Greptile運用に必要な`refs/heads/main`と`refs/heads/codex/greptile-reviewed`の親Codexによる通常fast-forwardだけ許可

## 現在の停止理由

Task 009は完了。ユーザー明示priorityに従い、source comment absolute invariant taskをGLMなしで親Codexが直接実施する開始境界。

## 次の親Codex操作

ACTIVE taskのOriginal instructionと直接編集規則を再読し、過去comment対策の一次証拠・source inventory・既存lint ecosystemを確認してcommentlint設計へ進む。本taskではGLM worker/reviewerを利用しない。pushはGreptile運用のremote main/checkpoint通常fast-forward以外禁止。
