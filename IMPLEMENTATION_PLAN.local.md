# codex-worker-orchestrator 実装index

恒久workflowは `IMPLEMENTATION_RULES.md`、個別要求は `IMPLEMENTATION_TASKS/*.md`、完了証跡とescaped原因は `IMPLEMENTATION_HISTORY.md`を正とする。このfileへtask詳細を複製しない。

## 最上位目的

Sol High相当の品質をできるだけ維持しながらCodex / Sol側の実消費量を大幅に削減する。最上位EvalはDirect Codex対Codex + glm-workerのCodex ReductionとQuality Delta。

## ACTIVE

- `IMPLEMENTATION_TASKS/commit-authorization-source-recognition.md`

## NEXT（優先順）

- `IMPLEMENTATION_TASKS/comment-lint-empty-line-fix.md`
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
- implementation baseline: Markdown runtime context削減を`ab28e21`でpush・本配置し、install smoke loop cost削減の実装・review・Sol採用・completion metadata同期を完了したcurrent HEAD
- implementation boundary: production installer契約と43 installer scenarioを維持したままreal `go test ./...`を44回から3回へ削減し、full smokeを実環境305.71秒で再確認。machine execution反復costの観測・親task化判断を既存product化contractへ統合
- preserved boundary: lossless ACTIVE task requirement、Plan index、History cold path、必要quality gateは維持。commit/push authorization source不一致をACTIVEへ昇格し、commentlint空行fixとEVAL責務整理を後続NEXTとして保持
- push: 各親commit後の`refs/heads/main`とGreptile正常review後の`refs/heads/codex/greptile-reviewed`だけを親Codexが通常fast-forwardする。GLM push、force/non-fast-forward、他refへのpushは禁止

## 現在の停止理由

commit/push authorization sourceの受理集合をcanonical instruction間で同期できる境界。

## 次の親Codex操作

ACTIVE taskのlossless requirementを要求正本として、task内明示許可と本repositoryのremote main fast-forward継続許可をrepository/installed instruction・実行承認境界で一致させる。
