# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/install-smoke-loop-cost-reduction.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/commit-authorization-source-recognition.md`
- `IMPLEMENTATION_TASKS/comment-lint-empty-line-fix.md`
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
- implementation baseline: external review F1-F3を隔離統合・pushし、Markdown runtime context削減の実装・review・親採用・completion metadata同期を完了したcurrent HEAD
- implementation boundary: PoC write不能境界、heredoc認識、Codex app-server逐次handshakeを`f1a25cd`でremote mainへ反映。Markdown read graphを実測し、stop/isolate詳細をevent-gated instructionへ移動、README/prompt/prose pinを圧縮
- preserved boundary: lossless ACTIVE task requirement、Plan index、History cold path、必要quality gateは維持。20分超のfull smoke反復costをACTIVEへ昇格し、commit/push authority不一致とcomment lint空行fixを後続NEXTとして保持
- push: 各親commit後の`refs/heads/main`とGreptile正常review後の`refs/heads/codex/greptile-reviewed`だけを親Codexが通常fast-forwardする。GLM push、force/non-fast-forward、他refへのpushは禁止

## 現在の停止理由

install smokeの重複full Go suiteを品質coverage維持のまま削減できる境界。

## 次の親Codex操作

ACTIVE taskのlossless requirementを要求正本として、scenario別semantic coverageとreal `go test ./...`反復回数を測定し、production install behaviorを弱めずfull smokeを高速化する。
