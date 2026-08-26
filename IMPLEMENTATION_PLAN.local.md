# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/comment-lint-empty-line-fix.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/eval-responsibility-reduction.md`
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
- implementation baseline: 5h wake再予約lifecycle修復をcurrent HEADへ収録した状態
- implementation boundary: 発火済みautomationを削除せず同一IDへ次回one-shotを更新し、suggested-create false-completeを拒否するcanonical instruction・policy/scenario testを収録。全test/race/vet/build/gofmt、commentlint、full install smoke、独立review、親Codex最終採用を完了
- preserved boundary: 次回wake `2026-08-26T10:18:27Z`を同ID・同target・ACTIVE one-shotでmachine verification済み。force/non-fast-forward・tag・他ref・他repository・GLM commit/push禁止を維持し、commentlint空行fixをACTIVE、EVAL責務整理をNEXTとして保持
- push: 各親commit後の`refs/heads/main`とGreptile正常review後の`refs/heads/codex/greptile-reviewed`だけを親Codexが通常fast-forwardする。GLM push、force/non-fast-forward、他refへのpushは禁止

## 現在の停止理由

なし。comment lint fixが不要空行を新規生成する原因を修正する境界。

## 次の親Codex操作

ACTIVE taskのlossless requirementを正として、comment lint fixの不要空行生成原因をGLMへ調査・実装修正させる。
